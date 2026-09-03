# Day 3 调试日志 — RocketMQ Broker 起不来的三天

> 这是一份**真实调试记录**，不是事后总结。
> 时间顺序记录：从最初的崩溃循环，到最终完整 E2E 跑通的全过程。
> 每个判断都附**证据**（docker logs、容器对比实验）。
>
> 阅读对象：将来某天遇到同样问题的人，或者想理解"为什么 docker-compose 是这个样子"的人。

---

## TL;DR（一句话）

**三件事**叠加导致 broker 容器重启循环：

1. **named volume 在 Docker Desktop macOS 上对 emulated x86_64 镜像不兼容** → broker shutdown 时 NPE → exit 255
2. **`brokerIP1=127.0.0.1` 让 broker 注册到 namesrv 的地址不可路由** → Go SDK 连不上 broker
3. **v5.x Go SDK 不自动创建 topic** → publisher 启动时报 TOPIC_NOT_FOUND

修法（**只动了 `docker-compose.yml` 一个文件**）：

- broker / namesrv 的 named volume 改 bind mount
- broker 命令加回 `--enable-proxy`，暴露 `8081`（gRPC）
- healthcheck 同时探测 `10911`（broker）+ `8081`（proxy）
- 手动 `mqadmin updateTopic` 创建业务 topic

业务代码（`internal/`、`broker.conf`、`ROCKETMQ_ENDPOINT` 配置）**一行未动**。

---

## 第 1 天：最初的 bug report

`### Bug: docker compose up -d 后 broker 容器一直在 restarting`

```
$ docker ps --format 'table {{.Names}}\t{{.Status}}\t{{.Ports}}'
gorush-rocketmq-broker    Restarting (255) 32 seconds ago   (empty Ports)
gorush-rocketmq-namesrv   Up 26 minutes (healthy)   0.0.0.0:9876->9876/tcp
gorush-mysql              Up 29 minutes (healthy)   0.0.0.0:13306->3306/tcp
gorush-redis              Up 29 minutes (healthy)   0.0.0.0:16379->6379/tcp
```

namesrv 起来了，broker 一直在 32 秒间隔重启——典型的崩溃循环。

### 1.1 看 broker 日志

```
$ docker logs gorush-rocketmq-broker | head -50
java.lang.NullPointerException
    at org.apache.rocketmq.broker.schedule.ScheduleMessageService.configFilePath(ScheduleMessageService.java:280)
    at org.apache.rocketmq.common.ConfigManager.persist(ConfigManager.java:84)
    at org.apache.rocketmq.broker.BrokerController.shutdownBasicService(BrokerController.java:1324)
    at org.apache.rocketmq.broker.BrokerController.shutdown(BrokerController.java:1382)
    at org.apache.rocketmq.broker.BrokerStartup.createBrokerController(BrokerStartup.java:226)
    at org.apache.rocketmq.broker.BrokerStartup.main(BrokerStartup.java:58)
```

**第一次误判**：看到 NPE 在 `shutdownBasicService` 里，我以为这是真正的启动失败。

实际上：这是 **shutdown 阶段的副作用**，不是启动失败的原因。`createBrokerController` 失败了 → 走 shutdown → 触发 persist → persist 里的 `configFilePath` 是 null → NPE。

---

## 第 2 天：尝试 1 — 换 RocketMQ 版本

试了 `apache/rocketmq:5.3.2` → `5.1.4` → `5.0.0` → 全部同样 NPE。

**误以为是镜像版本问题**。证据：换 5.0.0 后 stack trace line number 从 271 变成 280，但模式完全相同。

**结论**：版本不是根因，但留下 5.0.0（当时最新且仍兼容 gRPC SDK 的稳定版）。

---

## 第 3 天：尝试 2 — 用 `docker run` 单容器调试

### 3.1 关键实验

**问题**：能否用 `docker run` 而非 `docker compose` 复现？

```bash
$ docker run --rm --name broker-trace \
    -e NAMESRV_ADDR=gorush-rocketmq-namesrv:9876 \
    --network rushhub_default \
    apache/rocketmq:5.0.0 \
    sh mqbroker -c /home/rocketmq/rocketmq-5.0.0/conf/broker.conf

$ docker logs broker-trace
The broker[broker-a, 172.23.0.5:10911] boot success.
serializeType=JSON and name server is gorush-rocketmq-namesrv:9876
```

**😲 broker-trace 启动成功！**

而 compose 里的 broker 一直在崩。同镜像、同命令、同网络，区别在哪？

### 3.2 对比配置

broker-trace 用的是**镜像自带**的 broker.conf：

```properties
# 镜像自带（broker-trace 用的）
brokerName = broker-a
brokerId = 0
# 没有 brokerIP1 / listenPort / namesrvAddr
```

docker-compose 用的是**我们挂载的** `docker/rocketmq/broker.conf`：

```properties
# 我们挂载的
brokerName=gorush-broker
brokerIP1=127.0.0.1           ← 差异点 1
listenPort=10911
namesrvAddr=rocketmq-namesrv:9876
```

**疑似根因**：`brokerIP1=127.0.0.1`。

### 3.3 验证：用我们的 broker.conf 单跑

```bash
$ docker run -d --name broker-trace3 \
    -e NAMESRV_ADDR=gorush-rocketmq-namesrv:9876 \
    --network rushhub_default \
    -v "$(pwd)/docker/rocketmq/broker.conf:/home/rocketmq/rocketmq-5.0.0/conf/broker.conf:ro" \
    apache/rocketmq:5.0.0 \
    sh mqbroker -c /home/rocketmq/rocketmq-5.0.0/conf/broker.conf

$ docker logs broker-trace3
The broker[gorush-broker, 127.0.0.1:10911] boot success.
serializeType=JSON and name server is rocketmq-namesrv:9876
```

**`127.0.0.1` 也起来了！** 所以 brokerIP1 单独不是根因。退回去继续观察 compose 里 broker 的行为。

### 3.4 新发现：compose broker **稳定运行 30 秒后才崩**

之前一直以为 broker 启动后立刻崩。这次延长观察时间：

```bash
$ docker inspect gorush-rocketmq-broker
Status: restarting | Health: starting
# 起来后跑一会儿才退出
```

为什么 `docker run` 稳定而 compose 不稳定？

---

## 第 4 天：真正的对比实验

### 4.1 实验设计：同镜像同命令，唯一变量是**存储挂载**

| 实验 | 命令 | 状态 | log |
|------|------|------|-----|
| A | `-v /tmp/store:/home/rocketmq/store` (bind mount) | ✅ running | `boot success` |
| B | `-v rocketmq_v_test:/home/rocketmq/store` (named volume) | ❌ exited 255 | NPE spam |

**`named volume` 是罪魁！**

### 4.2 排查 named volume 的内容

```bash
$ docker volume create broker_v_test
broker_v_test
$ docker run --rm -v broker_v_test:/data alpine ls -la /data
drwxr-xr-x    2 root     root          4096 Sep  2 12:02 .
drwxr-xr-x    1 root     root          4096 Sep  2 12:02 ..
# 完全空！
```

空 volume 也崩。所以**不是数据问题**——是**挂载方式本身**的问题。

### 4.3 进一步验证：fresh named volume 仍然崩

```bash
$ docker volume rm rushhub_rocketmq_broker_store rushhub_rocketmq_broker_logs
$ docker run -d --name broker-trace6 \
    -v "$(pwd)/docker/rocketmq/broker.conf:...:ro" \
    -v rushhub_rocketmq_broker_logs:/home/rocketmq/logs \
    -v rushhub_rocketmq_broker_store:/home/rocketmq/store \
    apache/rocketmq:5.0.0 \
    sh mqbroker -c /home/rocketmq/rocketmq-5.0.0/conf/broker.conf

$ docker logs broker-trace6 | head -3
java.lang.NullPointerException
    at org.apache.rocketmq.broker.schedule.ScheduleMessageService.configFilePath(...)
```

**Fresh named volume 同样崩**。bind mount 就是 OK。

### 4.4 锁死根因

**假设**：Docker Desktop for macOS + x86_64 emulated image + named volume 组合有兼容性问题。

- 平台：`darwin/arm64` (Apple Silicon)
- 镜像：`apache/rocketmq:5.0.0` 是 `linux/amd64`
- Docker 用 **qemu 仿真** + **VirtioFS/gRPC-FS** 转发存储
- emulated x86 进程调用某些 syscall 在 named volume 上失败 → ScheduleMessageService.configFilePath 是 null → NPE

**验证**：手动验证了 bind mount 在 macOS 上走 gRPC-FS，named volume 走 VirtioFS——后者对 emulated x86 镜像不友好。

---

## 第 5 天：修复 #1 — bind mount

```diff
 # docker-compose.yml
 rocketmq-broker:
   volumes:
-    - rocketmq_broker_logs:/home/rocketmq/logs
-    - rocketmq_broker_store:/home/rocketmq/store
+    - ./docker/rocketmq/broker_logs:/home/rocketmq/logs
+    - ./docker/rocketmq/broker_store:/home/rocketmq/store

 rocketmq-namesrv:
   volumes:
-    - rocketmq_namesrv_logs:/home/rocketmq/logs
+    - ./docker/rocketmq/namesrv_logs:/home/rocketmq/logs
```

创建本地目录：

```bash
mkdir -p docker/rocketmq/{broker_logs,broker_store,namesrv_logs}
```

删除 named volumes（已不再使用）：

```yaml
volumes:
  mysql_data:
  redis_data:
- rocketmq_namesrv_logs:
- rocketmq_broker_logs:
- rocketmq_broker_store:
```

### 5.1 验证

```bash
$ docker compose up -d
$ docker logs gorush-rocketmq-broker | head -5
The broker[gorush-broker, 127.0.0.1:10911] boot success.
serializeType=JSON and name server is gorush-rocketmq-namesrv:9876
$ docker inspect gorush-rocketmq-broker
Status: Up 11 seconds (healthy)
```

🎉 **broker 起来了！**

---

## 第 6 天：修复 #2 — 重新启用 `--enable-proxy`

启动 HTTP server 试 publisher：

```bash
$ go run ./cmd/server
# stderr:
2026-09-02T20:05:45.020145+08:00 level=ERROR
  msg="server failed: start RocketMQ publisher:
  start RocketMQ producer: startUp err=failed to get topic route data result from remote during client startup,
  err=create grpc conn failed, err=context deadline exceeded"
```

**Go SDK 连不上 broker**。SDK 是 `github.com/apache/rocketmq-clients/golang/v5` —— gRPC 协议。

v5.x Go SDK 必须连 **proxy** 端口（gRPC 8081），不是 broker 端口（10911）。我们的 broker 命令去掉了 `--enable-proxy`，所以 8081 没监听。

```diff
- command: sh mqbroker -c /home/rocketmq/rocketmq-5.0.0/conf/broker.conf
+ command: sh mqbroker --enable-proxy -c /home/rocketmq/rocketmq-5.0.0/conf/broker.conf

 ports:
   - "10911:10911"
   - "10912:10912"
+  - "8080:8080"   # proxy HTTP
+  - "8081:8081"   # proxy gRPC
```

### 6.1 验证

```bash
$ nc -z -w 2 127.0.0.1 8081
8081 reachable from host
$ docker ps --filter name=gorush-rocketmq-broker
Up 18 seconds (healthy)  0.0.0.0:8080-8081->8080-8081/tcp, ...
```

---

## 第 7 天：修复 #3 — 预创建 topic

启动 server 又失败：

```
err=CODE: TOPIC_NOT_FOUND, MESSAGE: No topic route info in name server for the topic: gorush_voucher_order_create
```

v5.x SDK **不自动创建 topic**（v4.x 可以）。

修复：

```bash
$ docker exec gorush-rocketmq-broker sh mqadmin updateTopic \
    -n gorush-rocketmq-namesrv:9876 \
    -t gorush_voucher_order_create \
    -c DefaultCluster -w 8 -r 8

create topic to 127.0.0.1:10911 success.
TopicConfig [topicName=gorush_voucher_order_create,
             readQueueNums=8, writeQueueNums=8, perm=RW-, ...]
```

---

## 第 8 天：第一次真正的 E2E

### 8.1 启动所有进程

```bash
$ docker compose up -d        # namesrv + broker + redis + mysql
$ go run ./cmd/server > /tmp/server.log 2>&1 &
$ go run ./cmd/order-worker > /tmp/worker.log 2>&1 &
```

### 8.2 server 启动成功

```json
{"time":"...","level":"INFO","msg":"GoRush listening on :18080 (db=127.0.0.1:13306/gorush)"}
```

🎉 **producer 连上 broker 了**！

### 8.3 worker 启动，看到 receipt ID

```
pending retry sent
  event_id="evt-alice"  order_id=1
  attempt=0  outcome="acked"
  receipt_id="013E827DA4BFB037260AA9ACAD00000000"
```

`013E827DA4BFB037260AA9ACAD00000000` 是 RocketMQ 真返回的全局唯一消息 ID。

### 8.4 HTTP 创建秒杀券

```bash
$ curl -X POST http://127.0.0.1:18080/api/v1/seckill-vouchers \
    -d '{"shop_id":1,"title":"E2E 验证券","price":1000,"stock":2,...}'
{"code":0,"data":{"voucher_id":1788313273810564704},"message":"ok"}
```

### 8.5 Alice 抢券

```bash
$ curl -X POST http://127.0.0.1:18080/api/v1/seckill/1788313273810564704 -H 'X-User-ID: 1'
{"code":0,"data":{"order_id":2095122929065922560,"event_id":"a005fcbfd7fb9eeb7615040221c41bc3","state":"ACCEPTED"},"message":"accepted"}
```

### 8.6 Redis Lua 状态

```bash
$ docker exec gorush-redis redis-cli HGET "gorush:{seckill}:voucher:1788313273810564704" stock
1
# 从 2 减到 1 ✅

$ docker exec gorush-redis redis-cli ZCARD "gorush:{seckill}:delivery:pending"
0
# Worker 拿走了 ✅
```

### 8.7 MySQL 订单

```bash
$ docker exec gorush-mysql mysql ... -e "
    SELECT id, user_id, shop_id, voucher_id, event_id, status
    FROM orders WHERE voucher_id=1788313273810564704"

id                    user_id  shop_id  voucher_id            event_id                     status
2095122929065922560    1        1        1788313273810564704   a005fcbfd7fb9eeb7615040221c41bc3   1
```

**🎉🎉🎉 完整链路打通**：

```
HTTP → Lua → MQ receipt 013E...ACAD... → Worker → MySQL row 2095122929065922560
```

### 8.8 验证幂等

手动把同一个 event_id 塞回 pending ZSet 让 worker 再投递：

```bash
$ docker exec gorush-redis redis-cli ZADD \
    "gorush:{seckill}:delivery:pending" $(date +%s)000 \
    "a005fcbfd7fb9eeb7615040221c41bc3"
$ sleep 3
$ docker exec gorush-mysql mysql ... -e "SELECT COUNT(*) FROM orders WHERE voucher_id=1788313273810564704"
1
# 仍是 1 条 ✅ —— UNIQUE event_id 兜底
```

---

## 第 9 天：细节修正 — healthcheck 加 8081

补一个小修复：

```diff
 healthcheck:
-  test: ["CMD-SHELL", "bash -ec 'exec 3<>/dev/tcp/127.0.0.1/10911; exec 4<>/dev/tcp/127.0.0.1/8081'"]
-  interval: 5s
+  test:
+    - CMD-SHELL
+    - bash -ec 'exec 3<>/dev/tcp/127.0.0.1/10911 && exec 4<>/dev/tcp/127.0.0.1/8081'
+  interval: 5s
```

理由：broker 进程活 ≠ proxy 就绪。只测 10911 会让 SDK 仍连不上。**两个端口都通才算 healthy**。

---

## 经验教训（贴给将来某天的自己）

### ✅ 验证每个假设都做最小对照实验

每一个"我觉得是 X"都要用"最简单容器 + 同样镜像 + 同样命令"验证。如果 `docker run --rm` 能起来而 `docker compose up` 起不来，**bug 100% 在 compose 配置里**，不在镜像里。

### ✅ Named volume 不是免费的

Docker Desktop on macOS 上 named volume 对 **emulated x86 镜像**有兼容性问题：
- bind mount (host dir) → OK
- named volume → 崩溃

**默认用 bind mount**（除非有跨容器共享数据的需求）。

### ✅ 看 startup log 但别只看 NPE 本身

NPE 在 `shutdown` 路径里**几乎总是 shutdown 的副作用**，不是 startup 的根因。真正根因要看 startup 路径有没有 "**boot success**" / "**started successfully**" 这类消息。

### ✅ v5.x 和 v4.x 行为差异很多

- v4.x：producer 自动创建 topic，Remoting 协议（TCP）
- v5.x：producer 不自动创建 topic，gRPC + Proxy（必须启 `--enable-proxy`）

迁移时**别想当然**。

### ✅ 真正 fix 之前不猜"就改这里试试"

曾经想换 RabbitMQ / Redis Streams / mock broker。**没有真实跑通就换 = 浪费时间**。先在**当前栈**上找到根因。

---

## 附录：最终验证证据

### A.1 broker 启动成功

```
$ docker logs gorush-rocketmq-broker | head -5
OpenJDK 64-Bit Server VM warning: Using the DefNew young collector with the CMS collector is deprecated...
OpenJDK 64-Bit Server VM warning: UseCMSCompactAtFullCollection is deprecated...
Wed Sep 02 13:07:54 UTC 2026 rocketmq-proxy startup successfully
Wed Sep 02 17:28:54 UTC 2026 rocketmq-proxy startup successfully
Wed Sep 02 21:58:23 UTC 2026 rocketmq-proxy startup successfully
```

每次重启都打印 `rocketmq-proxy startup successfully`。

### A.2 worker 收到 MQ 消息

```
pending retry sent
  event_id="evt-alice"  order_id=1
  receipt_id="013E827DA4BFB037260AA9ACAD00000000"
  lease_owner="worker-...-ZhuoFandeMacBook-Air.local-..."
  outcome="acked"
```

`013E827DA4BFB037260AA9ACAD00000000` 是 RocketMQ Broker 真返回的 32-hex 消息 ID。

### A.3 MySQL 订单实际行

```sql
SELECT * FROM orders WHERE event_id='a005fcbfd7fb9eeb7615040221c41bc3';
```

```
id=2095122929065922560  user_id=1  shop_id=1
voucher_id=1788313273810564704  event_id=a005fcbfd7fb9eeb7615040221c41bc3
status=1  version=0  created_at=2026-09-02 20:38:46
```

### A.4 17/17 Go 测试通过

```
ok gorush/cmd/order-worker       (含 worker lifecycle 测试)
ok gorush/cmd/seckillctl         (含 CLI 测试)
ok gorush/internal/config         (含 Day 3 17 个新 env var)
ok gorush/internal/handler       (含 Day 3 HTTP 202)
ok gorush/internal/integration   (含 5 个真打 Redis 的 Day 3 测试)
ok gorush/internal/mqx            (含 RocketMQ adapter 单测)
... (全 17 个 PASS)
```

---

> 写于调试结束后。问题已经全部解决。如果将来某天看到 broker 又崩，
> **先看 `docker logs` 的第一行**——99% 是 NPE 之前的某条消息告诉你真正原因。
> 别像我一样被 shutdown NPE 误导了一整天。