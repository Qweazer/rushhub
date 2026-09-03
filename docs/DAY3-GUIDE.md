# GoRush Day 3 实现原理 · Lua + RocketMQ 完全指南

> 面向 **Day 1 / Day 2 都跟下来的新手**。本文假设你已经懂：
> - 完整调用链（Handler → Service → Repository → DB）
> - Context / Request ID / 事务 / Cache Aside 等基础概念
> - Redis String 缓存 + 命中/失效
>
> 如果没，先看 `docs/DAY1-DAY2-GUIDE.md`。
>
> Day 3 的核心是：**把 Day 1 的"朴素 MySQL 秒杀"改造成"Redis Lua 原子扣减 + RocketMQ 异步落 MySQL + Worker 重试"**。

---

## 0 · 本文阅读指南

- **🔍 类比**：生活化比喻
- **⚠️ 小心**：常见踩坑
- **❓ 为什么**：设计取舍

---

## 1 · Day 3 解决了什么问题（Day 1 的"朴素秒杀"差在哪）

### 1.1 Day 1 的秒杀长这样

```
HTTP POST /api/v1/seckill/5
    ↓
Service.Seckill
    ↓
SeckillRepository.SeckillTx:
    BEGIN
    UPDATE seckill_vouchers SET stock = stock - 1 WHERE voucher_id = ? AND stock > 0
    INSERT INTO orders ...
    COMMIT
    ↓
HTTP 200 OK
```

### 1.2 这有什么问题（你跑 1k QPS 就知道了）

| 问题 | 后果 |
|------|------|
| **每个请求都打 MySQL** | 10000 QPS 抢 100 张券，MySQL 跑 10000 次 UPDATE+INSERT → 连接池爆 |
| **MySQL 是同步链路上的关键路径** | MySQL 抖动时秒杀功能 100% 不可用 |
| **没有限流** | 恶意刷接口直接打挂服务 |
| **没有重试** | 网卡抖动一次就丢单 |
| **没有冗余消费去重** | RocketMQ 至少一次投递 → MySQL 可能写 2 次订单 |

Day 3 要全部解决这些。

### 1.3 Day 3 的架构（一眼看完）

```
HTTP POST /api/v1/seckill/5
    ↓
[ Middleware: 限流 ]                ← 令牌桶，超额直接 429，连 Redis 都不去
    ↓
OrderService.Seckill:
    ↓
[ Redis Lua: qualification.lua ]   ← 原子扣减 + 一人一券 + reservation + pending + inflight
    ↓ （<1ms 返）
[ RocketMQ Publisher: Send ]       ← 异步把不可变 event 投递到 MQ
    ↓
HTTP 202 ACCEPTED                  ← 不等 worker，立刻返

═══════════════════════════════════════════════

[ cmd/order-worker ]（独立进程）):
    ↓
[ MQ Consumer ] 收到消息
    ↓
[ SeckillRepository.CreateOrderFromEvent ]
    BEGIN
    UPDATE seckill_vouchers SET stock = stock - 1   ← 真正落库才扣
    INSERT INTO orders (id, event_id, request_id, ...) ← UNIQUE(event_id) 兜底
    COMMIT
    ↓
[ Redis: MarkCreated ]             ← 标记 inflight → CREED
    ↓
[ MQ: Ack ]

[ 同时：Pending Runner ]
    ↓
[ 定期扫 Redis pending ZSet ]       ← 处理 Send 失败但 Lua 已成功的事件
    ↓
[ 重发到 MQ ]
```

🔍 **核心思想**：**MySQL 不再是同步关键路径**。Lua 在 Redis 里就把"资格"占住了（< 1ms），MySQL 在 worker 异步落库。MySQL 抖动了，秒杀依然能抢。

---

## 2 · 关键概念解释（小白字典）

### 2.1 令牌桶限流（Token Bucket）

**令牌桶** = 一个桶里装令牌，每秒往里倒 N 个；每来一个请求消耗一个令牌；桶空了就拒绝。

🔍 **类比**：游乐园"每小时接待 100 人"——门口放一个计数器，每来一个人减 1，归零就关门休息一小时。

**为什么选令牌桶而不是计数器**：计数器在 1 秒内接受 1000 个请求也是 1000/秒（瞬时尖刺）。令牌桶允许 burst——前 0.5 秒打满 200 个也行，但下一秒必须慢下来。

GoRush 默认：**200 QPS，burst 400**（每 API 实例）。

### 2.2 Lua 脚本原子性

**Redis 单线程**，所以一个 Lua 脚本里的所有命令会**被当一条命令**——中间不会被其他客户端插队。

🔍 **类比**：你在银行柜台办业务，从坐下到离开，所有步骤（取号 → 验证 → 盖章 → 给卡）一次性完成，不会有人插到你前面。

**为什么秒杀必须用 Lua**：因为"扣库存 + 检查一人一券 + 写 reservation + 写 pending + 写 inflight"必须**整体原子**——拆成多个 Redis 命令就有竞态。

### 2.3 异步消息（Async Messaging）

**同步调用**：A 调 B，B 处理完 A 才往下走。
**异步消息**：A 把消息扔到队列，A 立刻往下走；B 之后从队列拿消息处理。

🔍 **类比**：同步 = 你去餐厅点菜，厨师做完才走；异步 = 你扫码下单后立刻离开，厨师做好后服务员给你送上门。

**为什么秒杀要异步**：MySQL 写订单慢（10ms+），不能让它卡住"抢到券"的响应。Redis Lua 立刻返"抢到了"，MySQL 慢慢写。

### 2.4 至少一次 / 最多一次 / 恰好一次

| 语义 | 含义 | 谁能做到 |
|------|------|---------|
| 最多一次（At-Most-Once）| 消息可能丢 | Redis PUB/SUB |
| 至少一次（At-Least-Once）| 消息可能重复，但不会丢 | **RocketMQ 默认** |
| 恰好一次（Exactly-Once）| 消息不丢不重 | RocketMQ 事务消息 + 业务幂等 |

GoRush 用 **"至少一次 + 业务幂等"** 实现"业务恰好一次"：
- RocketMQ 至少一次投递（可能重发）
- MySQL `UNIQUE(event_id)` 兜底：第二次 INSERT 直接报错，返回 "OrderDuplicate"
- 业务上看 = 一笔订单只写一次

### 2.5 业务幂等（Idempotency）

**幂等**：同一个操作执行多次和执行一次结果一样。

例子：
- HTTP POST 重复发（用户多点几次按钮）→ 业务上看应该只下一个订单
- MQ 重复投递（网络抖动重发）→ 业务上看应该只写一次数据库

**怎么实现**：用一个**唯一 ID**（这里是 `event_id`，由 Redis Lua 在第一次 Reserve 时生成）。第二次看到相同 event_id → 跳过。

### 2.6 分布式锁（Distributed Lock）

**多个进程同时跑一个同一件事**——需要一把"锁"，保证只有一个人在做。

GoRush 用 Redis SETNX 实现：
- `SET key value NX PX 30000` —"如果 key 不存在就 set，过期 30 秒"
- 谁先拿到 key 谁就是 owner
- 别人拿到 `ErrRebuildBusy`

### 2.7 雪花算法（Snowflake）

**分布式唯一 ID**——多台机器同时生成 ID 不能撞。

经典方案：`(timestamp << 22) | (machine_id << 12) | (sequence)`——41 位时间戳 + 10 位机器 ID + 12 位自增序号。

GoRush 用 `github.com/bwmarrin/snowflake v0.3.0`，每个 server 实例需要不同 `ORDER_ID_NODE`（0~1023）。

---

## 3 · 完整调用链（Day 3 版）

### 3.1 抢券（POST /api/v1/seckill/:voucher_id）

```
HTTP POST /api/v1/seckill/5
    ↓
[ middleware.RequestID() ]          生成 req_id 写 ctx
[ middleware.Auth() ]              读 X-User-ID 写 ctx (uid=1)
[ middleware.SeckillRateLimit() ]   ← Day 3 新增：令牌桶，超额 429
    ↓
[ handler.OrderHandler.Seckill ]    解析路径参数
    ↓ c.Request.Context()
[ service.OrderService.Seckill ]
    1. 生成 orderID (snowflake) + eventID (crypto/rand)
    2. 调 redisx.Qualify(...)       ← Day 3 核心
       ↓ Redis: EVAL qualification.lua
       原子完成：
       - 检查 gate 是否暂停
       - 检查 voucher 是否存在 + status=1 + 时间范围内
       - 检查 user 是否已抢过 (buyers HASH)
       - 检查库存 (stock > 0)
       - 扣 stock (HINCRBY -1)
       - 写 buyers HASH (userID → orderID)
       - 写 requests HASH (requestID → orderID)
       - 写 reservation HASH (orderID → event)
       - 写 event HASH (eventID → payload)
       - 写 pending ZSet (eventID, score=next_retry_at_ms)
       - 写 inflight ZSet (orderID, score=now_ms)
       ↓ 返 outcome (Reserved / Replay / OutOfStock / AlreadyBought / ...)
    3. publisher.Send(storedEvent)   ← 第一次发 MQ
    4. RecordInitialSend(outcome)    ← 标记 SENT 或 PENDING
    ↓
HTTP 202 ACCEPTED
    { order_id, event_id, state }
```

### 3.2 worker 消费消息（独立进程 cmd/order-worker）

```
[ order-worker ] 启动：
    1. 连 MySQL / Redis / RocketMQ
    2. 启动 PendingRunner（每秒扫 pending ZSet）
    3. 启动 ConsumerRunner（MQ 拉消息）
    ↓
[ MQ Consumer 收到消息 ]
    ↓
[ worker.ConsumerRunner.processDelivery ]
    ↓
[ repository.CreateOrderFromEvent ]
    BEGIN
    SELECT WHERE event_id = ? OR id = ? OR (user_id, voucher_id)
    IF 找到：
       对比所有不可变字段
       完全一致 → OrderDuplicate（幂等返回）
       不一致  → ErrOrderPayloadConflict（拒收，让 MQ 重投到死信队列）
    ELSE：
       UPDATE stock = stock - 1 WHERE voucher_id=? AND stock > 0
       IF RowsAffected == 0 → ErrInventoryDrift (拒收)
       INSERT INTO orders(id, event_id, request_id, ...)
    COMMIT
    ↓
[ Redis MarkCreated(order_id) ]     ← 标记 inflight → CREED
    ↓
[ MQ Ack ]
```

### 3.3 Pending 重试（处理 Send 失败的"孤儿"）

```
[ 每 200ms 一次 PendingRunner.runPendingCycle ]
    ↓
[ ClaimPending(owner, batch=32, lease=10s) ]
    - 从 pending ZSet 拉 next_retry_at <= now 的事件
    - SETNX lease_key 抢锁（CAS）
    抢到的 owner 才能继续处理
    ↓
[ publisher.Send(storedEvent) ]
    ↓
[ ACK → MarkSent (移除 pending) ]
[ UNKNOWN / 失败 → Reschedule (重写 score, 增加 attempts) ]
    ↓
[ Backoff = min(max, min * 2^attempt) + jitter ]
    1s → 2s → 4s → 8s → ... → 30s 上限
```

🔍 **为什么需要 Pending Runner**：Lua 已经扣了库存，但首次 Send 失败。这种"卡住的事件"必须有人扫——不能让它永久 PENDING。

---

## 4 · 13 个 Lua 脚本在干嘛

| 脚本 | 何时被调 | 作用 |
|------|----------|------|
| `qualification.lua` | HTTP POST /seckill 时 | **核心**：原子扣减 + 占位 |
| `record_initial.lua` | 首次 Send 后 | 标记 ACK → SENT / 失败 → PENDING+attempted |
| `claim_pending.lua` | Worker 每 200ms | 拉一批 pending 事件 + 抢锁 |
| `mark_sent.lua` | MQ ACK 后 | 移除 pending + 设 delivery=SENT |
| `reschedule.lua` | Send 失败后 | 更新 pending score + attempts++ |
| `mark_created.lua` | MySQL 订单写入后 | 移除 inflight + 设 business=CREATED |
| `compensate.lua` | 终失败补偿 | 还库存 + 移 buyers + 标记 FAILED/CANCELLED |
| `rebuild_voucher.lua` | seckillctl rebuild | 单个 voucher 重建（清状态 + 重写）|
| `resume_seckill.lua` | seckillctl resume | 清暂停标记 |
| `acquire_rebuild.lua` | rebuild 前 | 抢全局重建锁（防两个运维同时重建） |
| `release_rebuild.lua` | rebuild 结束 | 释放全局重建锁 |
| `delete_rebuild_keys.lua` | rebuild 内部 | 精确删除 voucher/buyers/requests key |
| `finalize_rebuild.lua` | rebuild 完成 | 写 generation 标记 + 放锁 |

**所有脚本的共性**：
1. 用 KEYS 数组传 Redis keys（不是参数）→ Redis Cluster 兼容
2. 用 ARGV 传业务字段
3. 入参都验过 +（不许有 nil）
4. 整数返回码 → Go 翻译成 typed outcome（不解析字符串）

---

## 5 · 完整 Redis 数据结构速查

| Key 模式 | 类型 | 内容 |
|----------|------|------|
| `gorush:{seckill}:gate` | String | "paused" / 不存在 = 正常 |
| `gorush:{seckill}:voucher:{id}` | Hash | stock, shop_id, begin_ts, end_ts, status |
| `gorush:{seckill}:voucher:{id}:buyers` | Hash | userID → "orderID\|eventID\|requestID\|userID" |
| `gorush:{seckill}:voucher:{id}:requests` | Hash | requestID → "orderID\|eventID\|requestID\|userID" |
| `gorush:{seckill}:reservation:{orderID}` | Hash | orderID → event (version, business, delivery) |
| `gorush:{seckill}:event:{eventID}` | Hash | event payload + payload_sha1（防篡改）|
| `gorush:{seckill}:delivery:pending` | ZSet | eventID → next_retry_at_ms |
| `gorush:{seckill}:orders:inflight` | ZSet | orderID → created_ms |
| `gorush:{seckill}:rebuild:lease` | String | 当前 rebuild owner |
| `gorush:{seckill}:rebuild:generation` | String | 上次成功的 generation UUID |

**所有 key 都带 `{seckill}` hash tag** — Redis Cluster 用同一节点存，Lua 能跨 key 写。

🔍 **为什么用 Hash 而不是 String**？Hash 一次性 `HMSET/HMGET` 读写多个字段，比多次 `GET/SET` 快 10 倍。

---

## 6 · 关键设计取舍

### 6.1 为什么"两道扣库存"（Lua 扣一次 + MySQL 扣一次）？

直觉上应该只扣一次。但 Day 3 **故意扣两次**：

- **Lua 扣**：防 Redis 单点丢失导致的"少卖"。库存可以临时不一致，但 Lua 是性能路径不能漏。
- **MySQL 扣**：防 Redis 故障 / 重启后状态丢失，MySQL 是真相之源。

**最终一致性靠 worker**：worker 异步把 Redis 的 reservation 同步到 MySQL。如果 Lua 成功但 worker 没机会落 MySQL → 后台"重建"流程 (`seckillctl rebuild`) 从 orders 表反向恢复 Redis。

### 6.2 为什么 Lua 内返回 outcome（数字）而不是 error message？

```lua
return {2, payload, business, delivery, ...}  -- 业务结果
return redis.error_reply('corrupt state')      -- 真正的程序错
```

🔍 **业务 vs 系统错的区分**：
- 业务错（"已抢过"）→ outcome=2，正常返回，HTTP 也返 200 + code=60004
- 系统错（"Lua 状态被外部破坏"）→ redis.error_reply，HTTP 返 500

如果都用 error message 区分，客户端就要 `strings.Contains(err.Error(), "out of stock")` — **脆**。

### 6.3 为什么 reservation 写到 Hash 不是 String？

如果用 String，整个 reservation 是一个 JSON blob。要查"这个 orderID 是 SENT 还是 PENDING" 就得 JSON 解析整个 blob。

Hash 可以 `HGET reservation:{id} delivery` — O(1) 字段查询。

### 6.4 为什么 MySQL 表里 event_id 单独 UNIQUE？

`orders.event_id UNIQUE` 是兜底幂等的最后一道防线：
- Lua 重复扣减：Lua 内 buyers HASH 阻止
- MQ 重投：worker 端 event_id UNIQUE 阻止
- 两个 user/voucher 撞单：`(user_id, voucher_id)` UNIQUE 阻止

**三道防线，任何一道漏了都不会超卖 / 漏单**。

### 6.5 为什么 OrderService.Seckill 不在 Lua 失败时回滚？

Lua 失败（用户已抢过 / 库存不足）→ **本来就不应该改状态**。Lua 是原子的，要么全成功要么全不动，不需要回滚。

只有"Lua 成功但后续 Send 失败"才需要 `compensate.lua` 把库存还回去。

### 6.6 为什么 worker 用独立进程（`cmd/order-worker`）而不是 goroutine？

- **故障隔离**：worker OOM 不会让 HTTP 502
- **独立伸缩**：可以跑 3 个 worker 实例抢同一个 pending ZSet（自动水平扩展）
- **独立部署**：可以灰度升级 worker 不影响 HTTP
- **独立监控**：worker 健康端点 `:<WORKER_HEALTH_PORT>/health`

---

## 7 · `cmd/seckillctl` — 运维管控工具

Day 3 唯一允许 "Rebuild Redis state" 的工具，必须手动执行。

```bash
# 查看当前状态
make seckill-status

# 暂停整个秒杀（Lua 看到 gate=paused 后返 QualificationPaused）
make seckill-pause

# 从 MySQL 重建 Redis（耗时，取决于 voucher 数）
make seckill-rebuild

# 恢复（要求 pending 和 inflight 都 = 0 才能成功）
make seckill-resume
```

🔍 **为什么不能在线 rebuild？**

`rebuild` 会清空每个 voucher 的 buyers/requests/reservation，但**不能动 inflight 和 pending**——这些是"正在飞"的订单。

如果 rebuild 时还有 inflight 订单 → 重建后 worker 又来一个 mark-created → Redis 上找不到对应 reservation → 状态错乱。

所以 `rebuild` 必须先 `pause`，再等 inflight/pending 全部归零，再 rebuild，最后 resume。

---

## 8 · 常见疑问 FAQ

### Q: Lua 里调用 Redis `TIME` 命令安全吗？

A: 不安全！TIME 在 Redis Cluster 下会报错（Redis 5.0+）。Day 3 Lua 没用 TIME，用 `next_retry_at_ms` 由调用方传入。

### Q: 怎么验证 event 真的不重复？

A: 看 `internal/repository/seckill_order_integration_test.go` 的 6 个测试——同 event_id 两次 → OrderDuplicate；同 order_id 不同 event → ErrOrderPayloadConflict。

### Q: 秒杀返了 ACCEPTED 但 order 表查不到？

A: 正常。ACCEPTED 只表示 Lua + 首次 Send 都成功了。**CREATED 才表示 MySQL 已经写入**。等几秒查 `GET /orders/:id`，state 字段会从 PENDING 变成 CREATED。

### Q: MySQL 突然挂了怎么办？

A:
- HTTP 层：Lua 还在跑，抢到的用户拿到 ACCEPTED
- worker：发现 MySQL 报错 → 不 ACK → MQ 重投 → 持续重试直到 MySQL 恢复
- 用户：看到订单一直 PENDING，体验下降但不丢单

### Q: 怎么调 "1 万 QPS 抢 100 张券" 的测试？

A: 用 `wrk`/`vegeta` 打 `/api/v1/seckill/:id`，10000 并发。期望：
- 100 个 202（ACCEPTED）
- 9900 个 60003（out of stock）
- Redis stock=0
- MySQL orders = 100（worker 处理完后）

### Q: 不用 RocketMQ 可以吗？

A: 可以。`internal/mqx` 是接口，可以实现 NATS / Kafka / Redis Streams 版本。worker 不感知。

### Q: RocketMQ 5.x broker 起不来怎么办？

A: 按这个顺序查：
1. `docker logs gorush-rocketmq-broker | head -20` —— 看完整启动序列。
   - 看到 `boot success. serializeType=JSON and name server is ...` → **broker 实际启动了**，问题是别的（看 healthcheck）
   - 看到 `rocketmq-proxy startup successfully` → **proxy 也起来了**
   - 只看到 `NullPointerException` at `ScheduleMessageService.configFilePath:280` —— **named volume 兼容性问题**（见下）
2. **named volume 兼容性问题**（Docker Desktop macOS + x86_64 emulation）：broker 在 named volume 上跑 `ScheduleMessageService.persist()` 时 NPE，exit 255，restart 循环。修法：把 `docker-compose.yml` 里 broker 的两个 named volume 改成 bind mount：
   ```yaml
   volumes:
     - ./docker/rocketmq/broker.conf:/home/rocketmq/.../broker.conf:ro
     - ./docker/rocketmq/broker_logs:/home/rocketmq/logs      # bind mount
     - ./docker/rocketmq/broker_store:/home/rocketmq/store    # bind mount
   ```
3. **topic 没创建**：publisher 报 `No topic route info in name server` 或 `TOPIC_NOT_FOUND, code17`：
   ```bash
   docker exec gorush-rocketmq-broker sh mqadmin updateTopic \
     -n gorush-rocketmq-namesrv:9876 \
     -t gorush_voucher_order_create \
     -c DefaultCluster -w 8 -r 8
   ```
4. **Go SDK 连不上 proxy**：检查 `docker ps` 看 broker 是否暴露 8081（gRPC 端口）。检查 `ROCKETMQ_ENDPOINT` 是否配 `127.0.0.1:8081`。
5. **healthcheck 一直 unhealthy**：`docker-compose.yml` 现在要求同时 `10911` 和 `8081` 都通：
   ```yaml
   healthcheck:
     test: ["CMD-SHELL", "bash -ec 'exec 3<>/dev/tcp/127.0.0.1/10911 && exec 4<>/dev/tcp/127.0.0.1/8081'"]
   ```
   漏掉任何一个都会被 docker 判 unhealthy（即使进程还活着）。

### Q: `brokerIP1=127.0.0.1` 在生产能这么配吗？

A: **不能**。这在本地能跑只是 Docker Desktop 把容器 10911 映射到了 host 127.0.0.1:10911，Go SDK 恰好能找到。生产环境 broker 应配真实可路由 IP（如 K8s Pod IP 或 Service DNS）。

### Q: Go SDK 启动时往 `/logs` 写失败影响功能吗？

A: 不影响功能，只在 stderr 里有几行 "can't make directories for new logfile: mkdir /logs: read-only file system"。Go SDK 内部默认日志目录，Config 没暴露 LogPath。解决需要升级 SDK 或 monkey-patch 全局 logger。

---

## 9 · 调试清单（出问题时按这个查）

| 现象 | 查什么 |
|------|--------|
| 用户报"已抢过"但没订单 | Redis 看 buyers HASH 有没有该 userID；查 events 表 |
| 库存没释放 | `redis-cli HGET voucher:{id} stock` |
| 订单一直 PENDING | 看 `/health` Redis 是否 down；worker 日志最近一条 |
| rebuild 卡住 | `redis-cli GET rebuild:lease` 是不是过期了 |
| MQ 投递慢 | 看 worker 的 `last_send` 时间戳；查 broker 是不是 OOM |
| Lua 失败但状态错乱 | `redis-cli HGETALL voucher:{id}` 看每个字段 |
| **broker 容器重启循环，日志只有 NPE** | **Docker Desktop (macOS) + x86 镜像 + named volume 兼容问题**。把 `docker-compose.yml` 里 broker 的 named volume 换成 bind mount |
| **publisher 报 `TOPIC_NOT_FOUND`** | **topic 没创建**。`docker exec gorush-rocketmq-broker sh mqadmin updateTopic -n namesrv:9876 -t <topic> -c DefaultCluster -w 8 -r 8` |
| **publisher 连接超时** | broker 没启 `--enable-proxy`（v5.x Go SDK 必须 gRPC）；检查 8081 端口 `docker ps` 看是否映射 |
| **worker 报 `db_inventory_drift`** | MySQL `seckill_vouchers.stock` 已耗尽但 Redis 还认为有库存 → 用 `make seckill-rebuild` 从 MySQL 反向同步 Redis |

---

## 10 · Day 3 一图看接口

```
公开 (无需登录)                  登录态 (X-User-ID)
┌─────────────────────┐    ┌─────────────────────────────┐
│ GET  /health        │    │ POST   /api/v1/seckill/:id   │ ← Lua + 首次 MQ
│ GET  /shop-types    │    │ GET    /api/v1/orders/:id    │ ← MySQL CREATED 或 Redis PENDING
│ GET  /shops         │    └─────────────────────────────┘
│ GET  /shops/nearby  │
│ GET  /shops/hot     │    运维工具 (cmd/seckillctl)
│ GET  /shops/:id     │    ┌─────────────────────────────┐
│ GET  /shops/:id/... │    │ pause  / status / rebuild / resume │
│ POST /seckill-...   │    └─────────────────────────────┘
└─────────────────────┘
```

---

## 11 · 面试可讲点（Day 3 部分）

1. **"怎么解决秒杀超卖？"** → Redis Lua 原子扣减 + UNIQUE 兜底
2. **"消息至少一次投递怎么处理？"** → MySQL event_id UNIQUE + 业务 idempotency
3. **"为什么 Lua + MQ 不够还要 worker 重试？"** → 首次 Send 失败 → pending ZSet → PendingRunner 扫 → 重发
4. **"如果 worker 和 HTTP 都挂，只剩 Redis，数据会丢吗？"** → 不会，Redis 里有 reservation/pending，rebuild 时从 MySQL 反向同步
5. **"为什么 Lua 用 KEYS 而不是参数传 key？"** → Redis Cluster 必须用 KEYS 数组才能保证路由到同一节点

---

## 12 · Day 3 已完成的功能 + Day 4+ 计划

### 12.1 Day 3 真实链路验证（已跑通）

```
HTTP POST /api/v1/seckill-vouchers          →  voucher_id=1788313273810564704
HTTP POST /api/v1/seckill/1788313273810564704
  ├─ 中间件限流、auth
  ├─ OrderService.Seckill → Redis Lua (stock 2→1)
  ├─ RocketMQ publisher 投递（receipt 013E827DA4BFB037260AA9ACAD...）
  └─ HTTP 202 ACCEPTED 立即返回

后台 cmd/order-worker：
  ├─ MQ Consumer 拉到消息（receipt 匹配）
  ├─ repository.CreateOrderFromEvent BEGIN
  │     UPDATE seckill_vouchers SET stock=stock-1
  │     INSERT INTO orders (event_id, request_id, user_id, ...)
  │ COMMIT
  └─ Redis MarkCreated + MQ Ack

MySQL 出现：
  id=2095122929065922560  event_id=a005fcbfd7fb9eeb7615040221c41bc3  status=1
```

### 12.2 Day 3 容器化踩过的坑（已修复）

1. **Named volume 让 x86_64 broker 镜像崩溃**（Docker Desktop macOS）
   - 症状：日志只有 NPE，exit 255，restart 循环
   - 根因：emulated x86 容器在 VirtioFS 挂载的 named volume 上跑 RocketMQ 5.x broker 失败，shutdown 时 `ScheduleMessageService.configFilePath` 为 null
   - **修复**：`docker-compose.yml` 把 broker 的 `rocketmq_broker_logs` / `rocketmq_broker_store` 改成 **bind mount**（`./docker/rocketmq/broker_logs` 等）
   - **namesrv 也改**：同样原因，`rocketmq_namesrv_logs` 也换成 `./docker/rocketmq/namesrv_logs`

2. **broker 必须启 `--enable-proxy`**（v5.x Go SDK 只走 gRPC）
   - 症状：publisher 报 `TOPIC_NOT_FOUND` 或连接超时
   - 根因：Apache RocketMQ 5.x Go SDK v5.1.3 是 gRPC-only，必须连 broker 的 8081 proxy 端口
   - **修复**：命令改回 `sh mqbroker --enable-proxy -c ...`，暴露 `8080`（HTTP）/ `8081`（gRPC）
   - 端口列表：`10909`（HA）、`10911`（broker）、`10912`（vip channel）、`8080`、`8081`

3. **topic 要预创建**（v5.x SDK 启动时不自动创建）
   - 症状：publisher 报 `No topic route info in name server` / `CODE: 17  DESC: TOPIC_NOT_FOUND`
   - **修复**：
     ```bash
     docker exec gorush-rocketmq-broker sh mqadmin updateTopic \
       -n gorush-rocketmq-namesrv:9876 \
       -t gorush_voucher_order_create \
       -c DefaultCluster -w 8 -r 8
     ```
   - 生产可用 broker 的 `autoCreateTopicEnable=true` 自动建（我们 broker.conf 里已经开了），但首次启动还得手动触发一次

4. **healthcheck 必须同时探测 10911 和 8081**
   - broker 进程活 ≠ proxy 就绪。只测 10911 会让 SDK 仍连不上
   - 当前 healthcheck 配置（已生效）：
     ```yaml
     healthcheck:
       test: ["CMD-SHELL", "bash -ec 'exec 3<>/dev/tcp/127.0.0.1/10911 && exec 4<>/dev/tcp/127.0.0.1/8081'"]
     ```
   - 两个端口都通才算 healthy

5. **Go SDK 默认往 `/logs` 写日志失败**（Mac 上 read-only）
   - 症状：stderr 有 `can't make directories for new logfile: mkdir /logs: read-only file system`，**不影响功能**
   - 根因：SDK 内部全局 logger 写默认路径，Config 没暴露 LogPath
   - 解决：升级 SDK 或 monkey-patch 全局 logger；今天不动

### 12.3 `brokerIP1=127.0.0.1` 的巧合

我们的 `docker/rocketmq/broker.conf` 把 `brokerIP1=127.0.0.1`——按理 broker 会把"自己在 127.0.0.1"告诉 namesrv，其他容器访问不到。

**为什么本地能跑？** Docker Desktop 把容器 10911 → host 127.0.0.1:10911，host 上的 Go SDK 看到的 `127.0.0.1:10911` 经端口映射恰好能到达 broker。

**⚠️ 生产环境必改**：多 broker + 跨主机时必须把 `brokerIP1` 改为真实可路由地址（如 broker 容器 IP 或 K8s service DNS）。

### 12.4 Day 4+ 计划

- Prometheus / pprof 性能监控
- wrk / vegeta 压测基线
- 真实 JWT 登录（替代 X-User-ID）
- ELK / Loki 日志聚合
- 优雅 HTTP 429 重试策略
- Order 状态机：PAID（接支付）/ CLOSED（自动取消）
- RocketMQ 4.x 兼容（生产如果不喜欢 5.x 的 gRPC SDK）

---

## 附录 · 关键代码索引

| 你想知道 | 看哪里 |
|---------|--------|
| 秒杀 HTTP 入口 | `internal/handler/order.go:Seckill` |
| 业务编排 | `internal/service/order.go:Seckill` |
| Lua 调用 | `internal/redisx/seckill.go:Qualify` |
| qualification Lua 主脚本 | `internal/redisx/lua/qualification.lua` |
| 13 个 Lua 全集 | `internal/redisx/lua/*.lua` |
| MQ 发送 | `internal/mqx/rocketmq.go` |
| Worker 消费 | `internal/worker/consumer.go` |
| Pending 重试 | `internal/worker/pending.go` |
| 幂等事务 | `internal/repository/seckill.go:CreateOrderFromEvent` |
| 限流中间件 | `internal/middleware/seckill_rate_limit.go` |
| 令牌桶 | `internal/ratelimit/bucket.go` |
| 雪花 ID | `internal/orderflow/id.go` |
| 重建流程 | `internal/seckilladmin/service.go` |
| 管控 CLI | `cmd/seckillctl/main.go` |
| Worker 进程 | `cmd/order-worker/main.go` |
| 集成测试 | `internal/integration/day3_*.go` |

---

## 写在最后

Day 3 看着复杂，拆开就是三件事：

1. **Lua 原子扣减** —— 一份"扣库存脚本"覆盖所有竞态
2. **MQ 异步落库** —— 同步关键路径上没 MySQL
3. **Worker 重试 + 幂等** —— 至少一次 + 业务幂等 = 业务恰好一次

> 技能点不是背出来的，是项目逼着学出来的。
> 读懂 Day 3 你就理解了：**高并发系统的核心不是"快"，而是"不丢不重"**。