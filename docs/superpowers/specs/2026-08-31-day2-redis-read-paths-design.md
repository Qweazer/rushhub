# GoRush Day 2 Redis 高频读链路设计

## 目标

在保留 Day 1 Gin、GORM、MySQL 分层和既有 API 行为的前提下，让商家详情、附近商家、热门排行榜和优惠活动四类高频读场景由 Redis 承担，完成 Cache Aside、GEO 和 ZSet 的可运行基线。

Day 2 的完成标准：

- 商家详情和优惠活动采用 Cache Aside，缓存命中时不查询 MySQL。
- 附近商家直接由 Redis GEO 完成地理位置检索和距离排序。
- 热门日榜直接由 Redis ZSet 完成计数和排名。
- Redis 出现故障时，商家详情和优惠活动可以回源 MySQL；附近商家和热门榜返回明确的 503。
- 提供可重复执行的 GEO 重建命令、Docker Redis 环境、自动化测试和运行文档。

## 范围

### 本次实现

- Redis 客户端、配置、健康检查和 `gorush:` key 命名空间。
- 商家详情 Cache Aside、空值缓存、TTL 抖动和进程内 singleflight。
- 优惠活动 Cache Aside，以及创建秒杀券后的缓存失效。
- 按全部商家和商家分类维护 Redis GEO 索引。
- 附近商家查询 API。
- 以商家详情访问量为口径的 Redis ZSet 日榜。
- 热门商家查询 API。
- 批量商家详情加载，避免附近和热门接口产生 N+1 MySQL 查询。
- GEO 重建 CLI、Docker Compose、Makefile、环境配置和 README 更新。
- 单元测试、Redis 集成测试和端到端验证。

### 本次不实现

- 商家写接口及其缓存失效流程。
- Redis 分布式锁、逻辑过期、后台刷新或多级缓存。
- 周榜、月榜、排行榜持久化或跨日合并。
- RocketMQ、CDC 或完整 CQRS 读模型同步。
- Redis Cluster、Sentinel、主从切换和生产容量规划。
- 搜索、推荐、支付、真实登录和 JWT。

## 架构

继续使用现有调用链：

```text
HTTP -> handler -> service -> repository -> MySQL
                    |
                    +-> redisx -> Redis
```

新增 `internal/redisx`，封装 Redis 协议、key、序列化、GEO 和 ZSet 操作。Service 仍是业务编排边界：它决定何时读缓存、何时回源、哪些 Redis 错误可以降级，以及如何组合 Redis 返回的 ID 与 MySQL/缓存中的商家对象。

不采用通用 Repository 装饰器，因为 GEO 和排行榜不是 MySQL Repository 的透明替代品；也不引入 CQRS 或消息队列，因为当前没有商家写链路，Day 2 只需建立清晰、可测试的 Redis 读路径。

## Redis key 设计

| 场景 | Key | 类型 | Value / Member | 生命周期 |
|---|---|---|---|---|
| 商家详情 | `gorush:shop:detail:{id}` | String | JSON 商家对象；不存在时为受控空值标记 | 正常值 30 分钟加抖动；空值 2 分钟 |
| 优惠活动 | `gorush:shop:vouchers:{shopID}` | String | JSON `VoucherGrouped` | 10 分钟加抖动 |
| 全部附近商家 | `gorush:geo:shops:all` | GEO/ZSet | member 为 shop ID，score 由 Redis GEO 编码 | 由 reindex 重建，无 TTL |
| 分类附近商家 | `gorush:geo:shops:type:{typeID}` | GEO/ZSet | member 为 shop ID | 由 reindex 重建，无 TTL |
| 热门日榜 | `gorush:shop:hot:{YYYYMMDD}` | ZSet | member 为 shop ID，score 为当天详情访问量 | 每次计数时刷新 72 小时 TTL |

日期使用应用服务器本地时区，与当前 GoRush `loc=Local` 的时间处理保持一致。

## 商家详情 Cache Aside

`GET /api/v1/shops/:id` 的处理顺序：

1. 从 `gorush:shop:detail:{id}` 读取缓存。
2. 命中正常 JSON 时直接返回商家。
3. 命中空值标记时返回 404，不访问 MySQL。
4. 缓存未命中时，通过进程内 singleflight 合并相同 shop ID 的并发回源。
5. 从 MySQL 查询商家；存在则写入带 TTL 抖动的缓存，不存在则写入 2 分钟空值。
6. Redis GET/SET 失败时记录结构化日志；GET 失败直接回源 MySQL，SET 失败不影响本次响应。
7. 成功取得商家后，仅对这条直接详情请求执行当天 ZSet 的 `ZINCRBY 1`。计数失败只记录日志，不让详情请求失败。

TTL 抖动采用基础 TTL 上增加有限随机时长，避免大量 key 同时过期。singleflight 只解决单实例内的击穿，不构成分布式锁。

## 优惠活动 Cache Aside

`GET /api/v1/shops/:id/vouchers` 的处理顺序：

1. 先验证商家存在性；验证使用商家详情缓存能力，但不增加热门计数。
2. 读取 `gorush:shop:vouchers:{shopID}`。
3. 命中时反序列化并直接返回。
4. 未命中时从 MySQL 查询 vouchers 与 seckill_vouchers，按普通、秒杀、推广分组后写入缓存。
5. Redis 故障时回源 MySQL。

`POST /api/v1/seckill-vouchers` 仍先在 MySQL 事务中写入 vouchers 和 seckill_vouchers。事务提交成功后删除 `gorush:shop:vouchers:{shopID}`。删除失败记录错误，但不把已成功的数据库事务改报为失败；旧值最多保留到 TTL 到期。

## 附近商家 GEO

新增接口：

```http
GET /api/v1/shops/nearby?lng=116.48&lat=39.99&radius=5000&type_id=1&page=1&size=10
```

参数规则：

- `lng`、`lat` 必填，分别限制在 `[-180, 180]` 和 `[-90, 90]`。
- `radius` 单位为米，默认 5000，范围 `(0, 50000]`。
- `type_id` 可选；缺省查 `gorush:geo:shops:all`，存在时查对应分类 key。
- `page` 默认 1，`size` 默认 10，最大 50。

查询流程：

1. 使用 `GEOSEARCH` 按半径查询，要求返回距离并按近到远排序。
2. Redis 没有原生 offset 分页；请求前 `page * size` 个结果，再在应用内切出当前页。为控制单次结果量，页码最大 100。
3. 对当前页的商家 ID 批量读取详情缓存。
4. 缓存缺失项使用单次 MySQL `WHERE id IN (...)` 查询并回填缓存。
5. 按 GEO 原顺序组装结果，每项包含 `shop` 和 `distance_m`。
6. GEO key 不存在时返回空列表；Redis 命令失败时返回 503，不进行 MySQL 经纬度扫描降级。

响应结构：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "items": [
      {"shop": {"id": 1, "name": "..."}, "distance_m": 328.4}
    ],
    "page": 1,
    "size": 10
  }
}
```

## 热门排行榜 ZSet

只有成功完成的直接商家详情请求增加热度。附近商家、热门榜和优惠活动内部加载商家对象时不增加热度，避免形成自反馈。

新增接口：

```http
GET /api/v1/shops/hot?limit=10
```

规则：

- `limit` 默认 10，最大 100。
- 使用当天 `gorush:shop:hot:{YYYYMMDD}` 执行 `ZREVRANGE WITHSCORES`。
- 对返回的商家 ID 批量加载详情，保持 ZSet 排序。
- 响应每项包含 `shop` 和整数 `views`。
- Redis key 不存在时返回空列表；Redis 命令失败时返回 503。
- 每次增加热度时同时刷新日榜 key 的 72 小时 TTL。跨日后旧 key 不再写入，将在 72 小时内自动删除；Day 2 API 只暴露当天榜单。

## GEO 重建

新增 `cmd/reindex` 和 `make reindex`：

1. 从 MySQL 一次性读取全部上线商家的 ID、分类 ID、经纬度。
2. 只删除 `gorush:geo:shops:all` 和 `gorush:geo:shops:type:*`，不调用 `FLUSHDB`，不删除详情缓存、优惠活动缓存或热门榜。
3. 使用 pipeline 写入全部商家 key 和分类 key。
4. 任一步失败时命令返回非零退出码并打印错误。

`make reset` 在 MySQL migrate、seed 完成后调用 `make reindex`。服务器启动时不扫描 MySQL 重建 GEO，避免多实例并发启动时重复建索引。

Day 2 没有商家写接口，因此 GEO 的变更来源只有 seed/reindex。未来新增商家写接口时，必须在写成功后同步更新或删除对应 GEO member。

## 配置与运行环境

配置新增：

- `REDIS_ADDR`，默认 `127.0.0.1:16379`。
- `REDIS_PASSWORD`，默认空。
- `REDIS_DB`，默认 `0`。
- `REDIS_TIMEOUT_MS`，默认 `200`。

Docker Compose 增加 Redis 7 服务，对宿主机暴露 `16379`，带健康检查和持久化 volume。应用创建 Redis client 时不因初次 Ping 失败而退出，因此 MySQL 读场景仍能降级；`/health` 同时检查 MySQL 和 Redis，任一失败均返回 503，并分别报告状态。

## 错误处理

- 新增 Redis 不可用业务错误码 `50301`，HTTP 状态为 503。
- 参数格式或范围错误返回现有 400 类错误。
- 商家不存在继续返回 404。
- 缓存反序列化失败视为缓存损坏：记录日志、删除损坏 key，并回源 MySQL。
- Cache Aside 的 Redis 故障不得被解释成缓存命中或数据不存在。
- GEO/热门接口没有 MySQL 降级路径；Redis 错误必须显式返回 503。
- 所有 Redis 错误日志携带 request ID、操作名和 key，不记录密码或完整连接串。

## 文件与职责

预计新增或修改：

- `internal/config/config.go`：Redis 配置。
- `internal/redisx/client.go`：client 创建、Ping、超时和关闭。
- `internal/redisx/keys.go`：集中维护所有 key 格式。
- `internal/redisx/cache.go`：通用字节缓存和受控空值标记；JSON 编解码保留在对应 Service，避免 `redisx` 反向依赖业务 DTO。
- `internal/redisx/geo.go`：GEO 查询和重建。
- `internal/redisx/hot.go`：热度累计和日榜读取。
- `internal/repository/shop.go`：批量商家查询和索引源数据查询。
- `internal/service/shop.go`：详情 Cache Aside、附近和热门编排。
- `internal/service/voucher.go`：优惠活动 Cache Aside 与失效。
- `internal/handler/shop.go`：附近和热门 HTTP 参数解析。
- `internal/handler/health.go`：Redis 健康检查。
- `internal/router/router.go`：依赖注入和新路由。
- `cmd/reindex/main.go`：GEO 重建 CLI。
- `docker-compose.yml`、`.env.example`、`Makefile`、`README.md`：本地运行与文档。

若具体实现中一个 Redis 文件过大，可按责任继续拆分；不得把业务校验下沉到 Redis 封装层。

## 测试策略

所有生产行为遵循 TDD：先写一个因缺少行为而失败的测试，确认失败原因正确，再写最小实现并运行到通过。

测试至少覆盖：

- 商家详情缓存命中不访问 MySQL。
- 商家详情缓存 miss 回源并写缓存。
- 不存在商家的短期空值缓存。
- Redis 读取或写入失败时详情仍能从 MySQL 返回。
- 只有直接详情请求增加热门计数。
- 优惠活动缓存命中、miss、Redis 故障回源。
- 创建秒杀券成功后删除缓存；删除失败不改变数据库成功结果。
- GEO 距离排序、分类 key、参数边界和应用层分页。
- 批量详情 miss 只执行一次批量 MySQL 查询并保持 Redis 排序。
- 热度累加、日榜降序、limit 边界和 72 小时 TTL。
- GEO/热门 Redis 故障返回 HTTP 503 与业务码 50301。
- `/health` 同时报告 MySQL 和 Redis。
- reindex 只操作 GoRush GEO keys，不清空 Redis DB。

Service 通过最小 Redis/MySQL 接口依赖具体实现，纯业务编排测试使用内存 fake，避免测试 GORM 或 go-redis 内部实现；Redis 命令语义使用真实 Redis 集成测试验证。最终验证包括：

- `go test ./...`
- `go vet ./...`
- 三个 CLI 和 server 的完整构建
- 启动 Docker MySQL/Redis 后执行 migrate、seed、reindex
- 通过 HTTP 验证缓存 miss/hit、附近排序、热门计数、活动缓存失效和 Redis 故障降级

## 验收场景

1. 全新环境执行 `make up && make migrate && make seed && make reindex && make run` 后，四类 Redis 读场景可调用。
2. 连续两次查询同一商家，第二次不访问 MySQL，并且当天热门分数累计为 2。
3. 附近接口按距离返回商家，分类筛选不会混入其他分类。
4. 热门接口按当天详情访问量倒序返回商家。
5. 连续两次查询优惠活动，第二次命中缓存；创建秒杀券后下一次查询能看到新活动。
6. Redis 停止后，商家详情和优惠活动仍可从 MySQL 返回；附近和热门返回 50301。
7. reindex 不会删除无关 Redis key，也不会清空详情缓存或热门榜。
