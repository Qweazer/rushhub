# GoRush

> 类似"大众点评"的本地生活服务平台后端。**Day 1 版本** —— 单体 Gin + MySQL + GORM，故意保留最朴素的实现，为后续 Day 引入 Redis / Lua / RocketMQ 等技术做铺垫。

## 项目定位

业务外壳：

- 商家分类 / 商家列表 / 商家详情
- 优惠券 / 推广活动 / 秒杀券
- 评价 / 收藏
- 朴素 MySQL 秒杀 + 订单
- 健康检查

技术主线（按 Day 推进，**今天只实现 Day 1 部分**）：

| Day | 主线 |
|---|---|
| 1 | Gin + GORM + MySQL，朴素实现 ✅ |
| 2+ | Redis Cache / GEO / ZSet / Lua |
| 3+ | RocketMQ / 限流 / 幂等 / 分布式锁 |
| 4+ | Prometheus / pprof / 压测 |

## 目录结构

```
gorush/
├── cmd/
│   ├── server/        # HTTP 服务入口
│   ├── migrate/       # 数据库迁移 CLI
│   └── seed/          # 演示数据灌入 CLI
├── internal/
│   ├── config/        # 环境变量集中读
│   ├── database/      # GORM 连接 + 连接池
│   ├── model/         # 8 张表的 Go struct
│   ├── migrate/       # 迁移 runner
│   ├── middleware/    # auth / request_id / access_log / recovery
│   ├── repository/    # 数据访问层
│   ├── service/       # 业务逻辑层
│   ├── handler/       # HTTP 边界层
│   ├── httpx/         # 统一响应 + slog 封装
│   └── router/        # 路由装配
├── migrations/        # SQL DDL
├── docker-compose.yml
├── Makefile
└── .env.example
```

## 本地启动

### 前置条件

- Go 1.21+
- Docker + Docker Compose

### 一键启动

```bash
# 1) 启动 MySQL（首次会拉镜像并初始化）
make up

# 2) 等 MySQL healthy 后执行 migration
make migrate

# 3) 灌入演示数据（可选）
make seed

# 4) 启动 GoRush
make run
```

服务监听 `http://127.0.0.1:18080`。

### 一键复位

```bash
make reset    # 删库重建 + migrate + seed
```

## API 一览

公开接口：

| Method | Path | 说明 |
|--------|------|------|
| GET    | /health | 健康检查（含 DB ping）|
| GET    | /api/v1/shop-types | 商家分类 |
| GET    | /api/v1/shops | 商家列表（`type_id` / `page` / `size` 可选）|
| GET    | /api/v1/shops/:id | 商家详情 |
| GET    | /api/v1/shops/:id/vouchers | 商家的所有活动（普通 / 秒杀 / 推广）|
| GET    | /api/v1/shops/:id/reviews | 商家评价列表 |
| POST   | /api/v1/seckill-vouchers | 创建秒杀券 |

需要 `X-User-ID` 头：

| Method | Path | 说明 |
|--------|------|------|
| POST   | /api/v1/shops/:id/reviews | 写评价 |
| POST   | /api/v1/shops/:id/favorite | 收藏（幂等）|
| DELETE | /api/v1/shops/:id/favorite | 取消收藏（幂等）|
| GET    | /api/v1/users/me/favorites | 我的收藏 |
| POST   | /api/v1/seckill/:voucher_id | 抢秒杀券 |
| GET    | /api/v1/orders/:id | 看自己的订单 |

所有响应统一格式：

```json
{ "code": 0, "message": "ok", "data": { ... } }
```

## curl 示例

```bash
# 健康检查
curl http://127.0.0.1:18080/health

# 商家分类
curl http://127.0.0.1:18080/api/v1/shop-types

# 商家列表（按分类过滤 + 分页）
curl 'http://127.0.0.1:18080/api/v1/shops?type_id=1&page=1&size=10'

# 商家详情
curl http://127.0.0.1:18080/api/v1/shops/1

# 商家所有活动
curl http://127.0.0.1:18080/api/v1/shops/4/vouchers

# 创建秒杀券（运营/测试用）
curl -X POST http://127.0.0.1:18080/api/v1/seckill-vouchers \
  -H 'Content-Type: application/json' \
  -d '{"shop_id":1,"title":"99 元秒杀","price":9900,"stock":100,"begin_time":"2026-09-01T10:00:00+08:00","end_time":"2026-09-02T10:00:00+08:00"}'

# 写评价（需要登录态）
curl -X POST http://127.0.0.1:18080/api/v1/shops/1/reviews \
  -H 'Content-Type: application/json' \
  -H 'X-User-ID: 1' \
  -d '{"score":5,"content":"海底捞服务真棒"}'

# 收藏
curl -X POST http://127.0.0.1:18080/api/v1/shops/1/favorite -H 'X-User-ID: 1'

# 我的收藏
curl http://127.0.0.1:18080/api/v1/users/me/favorites -H 'X-User-ID: 1'

# 抢秒杀券
curl -X POST http://127.0.0.1:18080/api/v1/seckill/5 -H 'X-User-ID: 1'

# 看自己订单
curl http://127.0.0.1:18080/api/v1/orders/1 -H 'X-User-ID: 1'
```

## 开发常用命令

```bash
make fmt      # gofmt -w .
make vet      # go vet ./...
make build    # 编译到 ./bin/server
make tidy     # go mod tidy
make logs     # docker compose logs -f mysql
make down     # 停 MySQL
```

## Day 1 已知局限（按设计保留，待后续 Day 优化）

- **普通 MySQL 秒杀**：每次抢券都要打 MySQL。高并发下 DB 压力大。
- **没有 Redis 缓存**：商家详情、券列表每次查库。
- **没有限流**：恶意请求可以打爆服务。
- **没有真正的登录态**：`X-User-ID` 头是模拟的，后续会接 JWT。
- **没有 ELK / 监控**：日志目前只在 stderr。
- **没有压测**：wrk / vegeta 等会在后续 Day 引入。

详见 `migrations/0001_init.sql` 与 `internal/repository/seckill.go` 的注释。