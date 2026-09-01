# GoRush

> 类似“大众点评”的本地生活服务平台后端。**Day 2 版本**在 Gin + MySQL + GORM 之上加入 Redis 缓存、GEO 附近商家和 ZSet 热度榜。

## 项目状态

| Day | 主线 | 状态 |
|---|---|---|
| 1 | Gin + GORM + MySQL，基础商家、券、评价、收藏和秒杀 | ✅ |
| 2 | Redis Cache / GEO / ZSet，缓存降级、健康检查和重建索引 | ✅ |
| 3+ | Lua / RocketMQ / 限流 / 幂等 / 分布式锁 | 待实现 |
| 4+ | Prometheus / pprof / 压测 | 待实现 |

## 目录结构

```text
gorush/
├── cmd/
│   ├── server/        # HTTP 服务入口
│   ├── migrate/       # MySQL 迁移 CLI
│   ├── seed/          # 演示数据 CLI
│   └── reindex/       # 从 MySQL 重建 Redis GEO 索引
├── internal/
│   ├── config/        # MySQL / Redis / HTTP 环境变量
│   ├── database/      # GORM 连接与连接池
│   ├── redisx/        # Cache / GEO / ZSet 封装与集成测试
│   ├── model/         # 数据库模型
│   ├── migrate/       # 迁移 runner
│   ├── middleware/    # auth / request_id / access_log / recovery
│   ├── repository/    # MySQL 数据访问层
│   ├── service/       # 业务逻辑与降级策略
│   ├── handler/       # HTTP 边界层
│   ├── httpx/         # 统一响应与日志
│   └── router/        # 路由与依赖装配
├── migrations/            # SQL DDL
├── docker-compose.yml     # MySQL 8 + Redis 7.4
├── Makefile
└── .env.example
```

## 本地启动

前置条件：Go 1.26.1+、Docker 和 Docker Compose。可按需将 `.env.example` 复制为 `.env`。默认 MySQL 使用 `127.0.0.1:13306`，Redis 使用 `127.0.0.1:16379`，HTTP 服务监听 `127.0.0.1:18080`。

启动顺序很重要：

```bash
# 1. 启动 MySQL 和 Redis（make up 本身不会等待健康检查完成）
make up

# 2. 重复查看状态，直到 mysql 和 redis 都显示 healthy，再继续
docker compose ps

# 3. 创建 MySQL 表
make migrate

# 4. 写入固定演示数据
make seed

# 5. 从 MySQL 中的在线商家重建 Redis GEO 索引
make reindex

# 6. 启动 HTTP 服务
make run
```

`make seed` 会重置演示数据；之后必须再执行 `make reindex`。Redis 数据丢失、Redis 持久化卷重建或 Redis 从库重做后，也要执行 `make reindex`。该命令只删除并重建 `gorush:geo:shops:*` GEO 索引，不会执行 `FLUSHDB` / `FLUSHALL`。

完整重置（会删除 Compose 数据卷）：

```bash
make reset
```

## API 一览

公开接口：

| Method | Path | 说明 | Redis 不可用时 |
|---|---|---|---|
| GET | `/health` | MySQL 和 Redis 健康检查 | HTTP 503，分别报告 `checks.db` / `checks.redis` |
| GET | `/api/v1/shop-types` | 商家分类 | 不受影响 |
| GET | `/api/v1/shops` | 商家列表，支持 `type_id` / `page` / `size` | 不受影响 |
| GET | `/api/v1/shops/:id` | 商家详情，缓存并增加当日热度 | 回退 MySQL，HTTP 200 |
| GET | `/api/v1/shops/nearby` | GEO 附近商家，按距离升序 | HTTP 503，业务码 `50301` |
| GET | `/api/v1/shops/hot` | 当日商家热度榜 | HTTP 503，业务码 `50301` |
| GET | `/api/v1/shops/:id/vouchers` | 商家的普通、秒杀和推广活动 | 回退 MySQL，HTTP 200 |
| GET | `/api/v1/shops/:id/reviews` | 商家评价列表 | 不受影响 |
| POST | `/api/v1/seckill-vouchers` | 创建秒杀券，成功后失效该商家券缓存 | MySQL 写入可用，缓存失效失败会记录日志 |

需要 `X-User-ID` 头：

| Method | Path | 说明 |
|---|---|---|
| POST | `/api/v1/shops/:id/reviews` | 写评价 |
| POST | `/api/v1/shops/:id/favorite` | 收藏（幂等） |
| DELETE | `/api/v1/shops/:id/favorite` | 取消收藏（幂等） |
| GET | `/api/v1/users/me/favorites` | 我的收藏 |
| POST | `/api/v1/seckill/:voucher_id` | 抢秒杀券 |
| GET | `/api/v1/orders/:id` | 查看自己的订单 |

统一业务响应格式：

```json
{"code":0,"message":"ok","data":{}}
```

## curl 示例

```bash
# 健康检查
curl -fsS http://127.0.0.1:18080/health

# 商家分类和列表
curl -fsS http://127.0.0.1:18080/api/v1/shop-types
curl -fsS 'http://127.0.0.1:18080/api/v1/shops?type_id=1&page=1&size=10'

# 详情：每次成功请求会为当日热度 +1
curl -fsS http://127.0.0.1:18080/api/v1/shops/1

# 附近商家：lng/lat 必填，radius 单位为米且最大 50000
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/nearby?lng=116.48&lat=39.99&radius=50000&page=1&size=10'

# 按当日访问数降序的热度榜，limit 最大 100
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/hot?limit=10'

# 商家所有活动
curl -fsS http://127.0.0.1:18080/api/v1/shops/4/vouchers

# 创建秒杀券（运营/测试用）
curl -fsS -X POST http://127.0.0.1:18080/api/v1/seckill-vouchers \
  -H 'Content-Type: application/json' \
  -d '{"shop_id":4,"title":"99 元限时秒杀","price":9900,"stock":100,"begin_time":"2026-09-01T10:00:00+08:00","end_time":"2026-09-02T10:00:00+08:00"}'

# 写评价（需要登录态）
curl -fsS -X POST http://127.0.0.1:18080/api/v1/shops/1/reviews \
  -H 'Content-Type: application/json' -H 'X-User-ID: 1' \
  -d '{"score":5,"content":"海底捞服务真棒"}'

# 收藏、我的收藏、抢券和查订单
curl -fsS -X POST http://127.0.0.1:18080/api/v1/shops/1/favorite -H 'X-User-ID: 1'
curl -fsS http://127.0.0.1:18080/api/v1/users/me/favorites -H 'X-User-ID: 1'
curl -fsS -X POST http://127.0.0.1:18080/api/v1/seckill/5 -H 'X-User-ID: 1'
curl -fsS http://127.0.0.1:18080/api/v1/orders/1 -H 'X-User-ID: 1'
```

## Redis 数据结构

| Key | 类型 | 内容 / 生命周期 |
|---|---|---|
| `gorush:shop:detail:{shop_id}` | String JSON | 商家详情，30 分钟 + 0–5 分钟随机 TTL；不存在商家使用 2 分钟空值标记 |
| `gorush:shop:vouchers:{shop_id}` | String JSON | 分组券列表，10 分钟 + 0–5 分钟随机 TTL；创建秒杀券后删除 |
| `gorush:geo:shops:all` | GEO / ZSet | 所有在线商家的坐标，由 `make reindex` 重建 |
| `gorush:geo:shops:type:{type_id}` | GEO / ZSet | 按分类的在线商家坐标，由 `make reindex` 重建 |
| `gorush:shop:hot:{YYYYMMDD}` | ZSet | member 为商家 ID，score 为当日详情访问数，TTL 72 小时 |

实用检查命令：

```bash
docker exec gorush-redis redis-cli GET gorush:shop:detail:1
docker exec gorush-redis redis-cli GET gorush:shop:vouchers:4
docker exec gorush-redis redis-cli GEOPOS gorush:geo:shops:all 1
docker exec gorush-redis redis-cli ZSCORE "gorush:shop:hot:$(date +%Y%m%d)" 1
```

## 验证命令

```bash
# 仅运行需要本地 Redis 的集成测试（固定使用 127.0.0.1:16379）
make test-redis

# 全量测试 + vet + 分别构建 server/migrate/seed/reindex
# 构建产物位于 mktemp -d 创建的目录，退出时只删除该临时目录
make verify
```

## 降级矩阵与已知限制

Redis 对 **nearby / hot 是必需依赖**：没有 Redis 无法计算 GEO 结果或读取 ZSet 榜单，因此返回 HTTP 503 / 业务码 `50301`。Redis 对 **shop detail / vouchers 是可选加速层**：读取、回填或失效失败时仍从 MySQL 返回结果。`/health` 在任一依赖异常时返回 503，便于 readiness 摘流。

- 热度榜只保留近 72 小时的每日计数，无跨日聚合。
- GEO 索引不会自动跟踪 MySQL 商家坐标/状态变更，变更后需运行 `make reindex`。
- 秒杀仍是朴素 MySQL 实现；Lua、消息队列、限流、幂等与分布式锁尚未引入。
- `X-User-ID` 只是本地演示登录态，尚未接入 JWT。
- 日志输出到 stderr，尚无 Prometheus / ELK / 压测基线。
