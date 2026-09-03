# GoRush 实现原理 · Day 1 + Day 2 完全指南

> 面向 **完全没写过 Go 后端** 的新手。读完你应该能：
> 1. 说清楚 GoRush 现在能做什么
> 2. 解释"一个 HTTP 请求是怎么从浏览器走到数据库、再把结果带回来的"
> 3. 看懂每一层代码在干什么、为什么这么写
> 4. 知道 Day 1 和 Day 2 各解决了一个什么问题
>
> 不需要先会 Go，本文档里出现的 Go 语法都会顺手解释。

---

## 第 0 章 · 这份文档怎么读

- **粗体词**：专有名词，第一次出现时会用大白话解释一遍
- **代码块**：可以直接复制到终端跑 / 跟着看的真实代码
- **🔍 类比**：用生活里的东西打比比，帮抽象变具象
- **⚠️ 小心**：常见踩坑 / 易混淆点
- **❓ 为什么**：讲"为什么不那么写"，设计取舍比单纯讲"这么写"更有用

---

## 第 1 章 · 项目到底是个啥

### 1.1 用一句话讲

GoRush 是一个**本地生活服务平台的后端**——你可以理解成"美团 / 大众点评背后那个接收 HTTP 请求、处理订单的服务器程序"。

### 1.2 它做了哪些事（用产品语言讲）

- 列出商家（海底捞、星巴克这种）
- 看某个商家有什么优惠券、是不是搞秒杀
- 抢秒杀券
- 给你买过的商家写评价、收藏商家
- 看你自己的订单

### 1.3 它没做的事（也是刻意没做的）

- 真的付款（订单状态永远是"待支付"）
- 发短信验证码登录（用一个假的"X-User-ID"头假装登录）
- 推送通知、消息队列、复杂限流（Day 3 才会加）

### 1.4 技术技术栈是什么（一段话扫盲）

> 如果完全不懂这些名词，先跳到第 4 章看概念解释，看完再回来。

- **Go**：写这个后端用的编程语言（类似 Java / Python / C++ 的一种）
- **Gin**：Go 里很常用的一个 **Web 框架**——帮你处理"接到 HTTP 请求后怎么解析参数、怎么返回 JSON"这种重复活
- **GORM**：Go 里很常用的一个 **ORM**——帮你把数据库表 ↔ Go 结构体（struct）互相转换，省得手写 SQL 拼接
- **MySQL**：经典的 **关系型数据库**——存数据用的，像一个巨大的 Excel
- **Redis**：另一个数据库，但是是**内存**的，比 MySQL 快1000倍，但贵且不能存太多数据——所以只用来当**缓存**或者**热点数据存储**
- **Docker Compose**：一个把"启动 MySQL"和"启动 Redis"这种命令写成 yaml 配置文件的工具，一行 `docker compose up` 就能把所有依赖拉起来

---

## 第 2 章 · 怎么把它跑起来

### 2.1 三步启动法

```bash
# 1. 启动 MySQL 和 Redis（Docker 会自动下载镜像并启动容器）
make up

# 2. 等容器都 healthy 后，建表 + 灌种子数据 + 建 Redis 索引
make migrate seed reindex

# 3. 启动 GoRush 自己
make run
```

启动后浏览器访问 `http://127.0.0.1:18080/health`，看到 `{"status":"ok","checks":{"db":"ok","redis":"ok"}}` 就成功了。

### 2.2 它在背后做了什么

```
你的终端 make up
    ↓
Docker 拉 mysql:8.0 镜像，启动容器 gorush-mysql
Docker 拉 redis:7.4 镜像，启动容器 gorush-redis
    ↓
两个容器各占一个端口，对你电脑"假装"成两个程序：
    127.0.0.1:13306 → MySQL（用 13306 是为了不和你本机可能有的 MySQL 撞）
    127.0.0.1:16379 → Redis
    ↓
你跑 make run，GoRush 进程启动：
    127.0.0.1:18080 → GoRush（HTTP 服务）
    ↓
现在你访问 http://127.0.0.1:18080/xxx，GoRush 会去问 MySQL/Redis 取数据，再返回给你
```

🔍 **类比**：MySQL 是个文件柜，Redis 是你的便签本（很快但只放临时数据），GoRush 是帮你找文件 / 翻便签再把结果给客户的**秘书**。

---

## 第 3 章 · 一个请求是怎么走完全程的（最重要）

> 这一章是全文的核心。读懂了这一章，后面所有代码都是这个套路的不同变体。

### 3.1 我们跟踪一个具体的请求

我们跟踪 `curl http://127.0.0.1:18080/api/v1/shops/1` 这个请求。它问的是"id=1 的商家是啥"。

### 3.2 它走过的路

```
你在终端输入 curl ...
    ↓ 你的电脑发出一个 HTTP 请求
网络传输
    ↓
GoRush 进程接收到请求（监听 18080 端口）
    ↓
[ 中间件 1: RequestID ]       生成/读取一个 request_id，写进 ctx
[ 中间件 2: AccessLog ]       准备记录访问日志
[ 中间件 3: Recovery ]       准备接 panic（如果后面哪一行崩了）
    ↓
[ Router ]                   路由匹配：发现 URL 是 /api/v1/shops/:id，要交给 shopH.GetByID 这个函数处理
    ↓
[ Handler: shopH.GetByID ]   拿到 URL 里的 :id=1，调一下 Service
    ↓ c.Request.Context()   ← 把"这个请求的上下文"拿出来往下传
[ Service: shopSvc.GetByID ] 做业务检查：商家存在吗？然后问 Repository 要数据
    ↓
[ Repository: shopRepo.GetByID ] 用 ctx 调 MySQL 查 shops 表 表
    ↓ db.WithContext(ctx).First(...)
[ GORM → driver → MySQL ]    真实查表，返回一行商家数据
    ↓
回到 Handler：把 Go 结构体序列化成 JSON
    ↓
回到 [ 中间件 2 AccessLog ]：打印访问日志（含 request_id）
    ↓
HTTP 响应回到你的终端
```

🔍 **类比**：把这套想象成餐厅点餐：
- **HTTP 请求** = 顾客递进来的点菜单
- **中间件** = 门口的迎宾 / 服务员，先做一下标准动作（编号、递纸巾）
- **Router** = 前台分诊台，看看你点的菜属于哪个窗口（粤菜 / 川菜 / 西餐）
- **Handler** = 服务员，把你的点菜单翻译成厨房能看懂的话
- **Service** = 厨房领班，决定做菜的顺序、检查食材够不够
- **Repository** = 负责去仓库（MySQL）取食材的库管
- **MySQL/Redis** = 真正的仓库

### 3.3 每层只依赖下一层

```
Handler 依赖 Service
Service 依赖 Repository
Repository 依赖 数据库驱动
```

**这个顺序很重要**——上面那一层**不能**直接调下面那一层下面的东西。例如 Handler 不能直接写 SQL，那是 Repository 的活；Service 不能直接拼 JSON，那是 Handler 的活。

为什么这么严？因为如果不这么分：
- 想换个数据库（MySQL → Postgres）？得改全栈代码
- 想给"查商家"加个缓存？分不清在哪层加
- 测试？没法单独测业务逻辑

后面 Day 2 加 Redis 缓存时你就看得到好处——只在 Repository 和 Service 之间塞一层，业务代码几乎不改动。

### 3.4 代码长什么样（看完就有感觉了）

我们看真实代码：

```go
// internal/handler/shop.go  (Handler 层)
func (h *ShopHandler) GetByID(c *gin.Context) {
    ctx := c.Request.Context()                  // ① 拿出请求的 ctx
    shop, err := h.Svc.GetByID(ctx, idFromPath)  // ② 调 Service，传 ctx
    if err != nil {
        httpx.Fail(c, err)                      // ③ 出错 → 统一错误格式
        return
    }
    httpx.OK(c, shop)                            // ④ 成功 → 统一成功格式
}
```

```go
// internal/service/shop.go  (Service 层)
func (s *ShopService) GetByID(ctx context.Context, id uint64) (*model.Shop, error) {
    shop, err := s.Repo.GetByID(ctx, id)          // 调 Repository
    if err == repository.ErrShopNotFound {        // 翻译成业务错误
        return nil, httpx.NewNotFound("shop not found")
    }
    return shop, err
}
```

```go
// internal/repository/shop.go  (Repository 层)
func (r *ShopRepository) GetByID(ctx context.Context, id uint64) (*model.Shop, error) {
    var s model.Shop
    err := r.DB.WithContext(ctx).First(&s, id).Error  // 真查库
    return &s, err
}
```

你会发现每层代码都**非常短**——每层只做自己那一件事。

---

## 第 4 章 · 重要概念解释（小白必备字典）

### 4.1 HTTP / REST

**HTTP**：浏览器 / curl 和服务器对话的"语言"。一次对话长这样：
```
你：GET /api/v1/shops/1 HTTP/1.1
    Host: 127.0.0.1:18080

服务器：HTTP/1.1 200 OK
        Content-Type: application/json

        {"code":0, "data":{...}}
```

**REST**：一种"用 HTTP 说话"的设计风格。简单说：
- `GET` 表示"读"
- `POST` 表示"新建"
- `PUT` 表示"改"
- `DELETE` 表示"删"

🔍 **类比**：HTTP 是快递信封，REST 是你写信时的格式约定（收件人地址写哪里、寄件人写哪里）。

### 4.2 JSON

**JSON**：数据的一种表示格式。Go 的结构体 ↔ JSON 之间的转换，Gin 都帮你做掉了，你只需要关心结构体长什么样：

```json
{
  "code": 0,
  "message": "ok",
  "data": {
    "id": 1,
    "name": "海底捞"
  }
}
```

### 4.3 中间件（Middleware）

**中间件**：在"请求 → handler"中间插入的一段函数。它能：
- 在请求到达前做点什么（生成 request_id、检查登录）
- 在响应发出前做点什么（记录访问日志、panic 恢复）
- 决定要不要放行（不登录就拦掉）

🔍 **类比**：中间件像车站的安检门——所有人进出都得过一遍，过了才能继续走。GoRush 的安检流程：
1. RequestID：给每个人贴个编号
2. AccessLog：记录谁进站了
3. Recovery：万一有人摔倒了扶一下

### 4.4 分层架构（Handler / Service / Repository）

**为什么分层**：避免一个文件里塞 2000 行。下面还会展开讲。

### 4.5 数据库 / 表 / SQL

**数据库**：存数据的地方。你可以想象成一摞"表"。

**表**：像 Excel 一样的东西。我们有一张 `shops` 表，长这样：

| id | name | address |
|----|------|---------|
| 1 | 海底捞(望京店) | 北京市朝阳区望京街 9 号 |
| 2 | 星巴克(三里屯店) | 北京市朝阳区三里屯路 19 号 |

**SQL**：跟数据库"说话"的语言。比如：
```sql
SELECT * FROM shops WHERE id = 1;
-- 翻译：从 shops 表里查出 id=1 的那一行
```

### 4.6 ORM（GORM）

**ORM** = Object-Relational Mapping。帮你把"Go 结构体"和"数据库表"互相映射，你不用手写 SQL：

```go
// 直接操作 Go struct
var shop Shop
db.First(&shop, 1)
// GORM 自动帮你拼成：SELECT * FROM shops WHERE id = 1 LIMIT 1;
```

⚠️ **小心**：ORM 不是万能药。有些复杂 SQL 还是得手写。

### 4.7 事务（Transaction）

**事务**：把多个操作打包成"一个动作"。要么**全部成功**，要么**全部失败**。

🔍 **类比**：转账——A 给 B 转账是两步操作：
1. A 的账户 -100
2. B 的账户 +100

如果第一步成功了第二步失败，A 就白白丢了 100 元。事务保证：两步要么都做、要么都不做。

Go 里这样写：
```go
db.Transaction(func(tx *gorm.DB) error {
    // 1. 改 A 账户
    // 2. 改 B 账户
    // 如果任何一个 return err，整个事务回滚（撤销）
    return nil  // return nil 才提交
})
```

**Day 1 的秒杀就是用事务把"扣库存"和"创建订单"打包**，后面会专门讲。

### 4.8 缓存（Cache）

**缓存**：把"经常被人查的东西"放一份到"特别快的存储"里。查的人先看缓存，缓存有就直接返回，没有再查数据库。

🔍 **类比**：缓存像你的桌面。经常用的东西放桌面上，伸手就拿到（快）。不常用的塞抽屉里，要翻半天（慢 = 数据库）。

**为什么需要**：MySQL 每次查询要 5~10 毫秒，Redis 只要 0.5 毫秒。100 倍差距。

### 4.9 Redis 数据类型

Redis 不只是"键值对"，有几种专门的数据结构：

| 类型 | 干嘛的 | 例子 |
|------|-------|------|
| **String** | 普通 KV，"一个 key 对应一个值" | 缓存商家详情 |
| **Hash** | 一个 key 里放多个字段 | 缓存用户资料（id/昵称/手机号）|
| **List** | 链表 | 消息队列 |
| **Set** | 集合（去重）| 点赞用户 ID 集合 |
| **ZSet** | 排序集合，"每个成员带分数，按分数排序" | 排行榜 |
| **GEO** | 地理位置，存经纬度 + 自动算距离 | 附近商家 |

**Day 2 用了 GEO 和 ZSet**，下面会专门讲。

### 4.10 上下文（Context）

**Context**：Go 里用来跨函数传递"这次请求的元信息"的东西。最常见的几个用途：
- **取消**：客户端断开连接了 → ctx.Done() → 后续操作能感知到，主动退出
- **超时**：ctx 限制操作最多 5 秒 → 超过 5 秒还没完就放弃
- **传值**：把 request_id、user_id 这种"贯穿整条链路"的数据塞 ctx 里

GoRush 里 ctx 是这样传的：
```
HTTP Request → ctx = c.Request.Context()
            → Service(ctx, ...)
            → Repository(ctx, ...)
            → db.WithContext(ctx).Exec(...)
```

每层都接 ctx，每层都往下传，**整个调用链共享同一个 ctx**。

### 4.11 结构化日志（slog）

**日志**：程序运行过程中打的"工作日记"。出问题查日志是第一步。

**结构化** vs 普通文本：
```
普通："[INFO] user 1 bought voucher 5"   ← 机器很难解析
结构化：
{
  "level": "INFO",
  "msg": "seckill success",
  "user_id": 1,
  "voucher_id": 5,
  "order_id": 7,
  "request_id": "abc123"
}
```

机器读结构化日志超方便（可以直接喂给 ELK / Loki 这种日志系统）。

Go 1.21+ 内置的 `log/slog` 就是做这个的。

### 4.12 测试（Unit Test / Integration Test）

**单元测试**：测一个函数是不是对的，**不依赖外部**（不连数据库）。
**集成测试**：测多个组件一起工作对不对，**真连数据库 / Redis**。

Go 内置测试：
```go
func TestAdd(t *testing.T) {
    if got := Add(1, 2); got != 3 {
        t.Errorf("Add(1, 2) = %d, want 3", got)
    }
}
```

跑测试：
```bash
go test ./...
```

---

## 第 5 章 · Day 1 详细解读（"最朴素的实现"）

Day 1 的目标是：**用一个最普通的 Gin + MySQL + GORM 实现一个能跑的本地生活后端**。所有"高级优化"都故意不加——为 Day 2 / Day 3 制造"对比基准"。

### 5.1 数据库表是怎么设计的

我们建了 8 张表。最关键的几张：

**shops（商家）**
```sql
CREATE TABLE shops (
    id          BIGINT,             -- 主键
    type_id     BIGINT,             -- 属于哪个分类
    name        VARCHAR(128),       -- 店名
    longitude   DECIMAL(10, 6),     -- 经度（Day 2 要用）
    latitude    DECIMAL(10, 6),     -- 纬度（Day 2 要用）
    score       DECIMAL(2, 1),      -- 评分（Day 2 排行榜要用）
    ...
);
```

为什么经纬度要提前存？Day 2 才能用 GEO 功能。这叫**"为未来留接口"**——虽然今天不用，但表里得有。

**orders（订单）**
```sql
CREATE TABLE orders (
    id          BIGINT,
    user_id     BIGINT,
    voucher_id  BIGINT,
    ...
    UNIQUE KEY (user_id, voucher_id)   -- 关键！
);
```

🔍 **`UNIQUE KEY` 是啥**？给数据库加一个"这俩字段组合起来不能重复"的规矩。
比如 Alice 抢了 voucher_id=5，就再创建 (user_id=1, voucher_id=5) 的订单会**直接报错**。这就是"一人一券"的数据库兜底。

**Day 1 强调**："任何一步失败 → ROLLBACK"——因为有事务，UPDATE库存 和 INSERT 订单要么都做、要么都不做。

### 5.2 秒杀是怎么写的（朴素版）

朴素秒杀 = "用 MySQL 事务包住两步"：

```go
db.Transaction(func(tx *gorm.DB) error {
    // 1. 扣库存（条件 UPDATE：只在还有库存时扣）
    res := tx.Exec(`
        UPDATE seckill_vouchers
        SET stock = stock - 1
        WHERE voucher_id = ? AND stock > 0
    `, voucherID)

    if res.RowsAffected == 0 {
        return errors.New("out of stock")  // 售罄
    }

    // 2. 创建订单（UNIQUE 兜底）
    tx.Exec(`
        INSERT INTO orders (user_id, shop_id, voucher_id, status)
        VALUES (?, ?, ?, 1)
    `, userID, shopID, voucherID)

    return nil  // 提交
})
```

**关键技巧 1：`WHERE stock > 0`**

不要先 `SELECT` 看库存再 `UPDATE`——两步操作之间会被别人插队（**竞态条件**）。把"查"和"改"合成一条 SQL，让数据库自己保证不会重复扣。

**关键技巧 2：`RowsAffected == 0`**

如果库存已经是 0，UPDATE 不改任何行，RowsAffected=0，这就是"售罄"信号。比先 SELECT 再判断简单得多。

**关键技巧 3：`UNIQUE(user_id, voucher_id)`**

万一两个请求几乎同时打到（Alice 用两台手机同时点抢），条件 UPDATE 都通过了，两个 INSERT 都跑——但 UNIQUE 约束让其中一个报错回滚。

### 5.3 怎么证明事务真的回滚了（Day 1 Step 8）

你不能光看代码"用了事务"就相信它真的回滚。所以 Day 1 加了一个**故意搞破坏的开关**：

```go
// 在 UPDATE 之后、INSERT 之前
if r.FailMode == "after_update" {
    return errors.New("simulated failure")
}
```

通过环境变量 `SECKILL_FAIL_AFTER_UPDATE=1` 开启。跑一次 → 看数据库：库存没变、订单没创建——证明 ROLLBACK 真的把"看似扣掉的库存"还回来了。

🔍 **这个思路很重要**：**你能用一段代码制造失败，证明另一段代码真的能扛住失败**。这比单元测试更接近真实事故。

### 5.4 怎么记录"哪条日志是哪条请求的"（Day 1 Step 9）

每个 HTTP 请求进来时：
1. 生成一个随机 ID（叫 request_id），或复用客户端发来的
2. 写到 ctx 里
3. 写到响应头 `X-Request-ID` 里，让客户端也能看到

业务代码只要用 `httpx.FromContext(ctx)` 拿 logger，打出来的日志**自动带 request_id**。

效果：
```
{"msg":"seckill attempt", "request_id":"abc123", ...}
{"msg":"seckill success",  "request_id":"abc123", ...}
{"msg":"http",             "request_id":"abc123", ...}
```

线上事故排查时，给客户一个 request_id 就能 grep 出整条调用链的所有日志。

### 5.5 Day 1 没解决什么问题（伏笔）

- 每次抢秒杀都打 MySQL → QPS 高时 MySQL 撑不住（**Day 3 用 Redis 解决**）
- 商家详情每次都查库 → 热门商家被刷爆（**Day 2 用 Redis 缓存解决**）
- 没有"附近商家"功能（**Day 2 用 Redis GEO 解决**）
- 没有排行榜功能（**Day 2 用 Redis ZSet 解决**）

---

## 第 6 章 · Day 2 详细解读（"引入 Redis 优化读路径"）

Day 2 的目标：**让读请求变快**，同时**保证 Redis 挂了不会让整个服务瘫**。

### 6.1 为什么需要 Redis（一次类比）

想象你是一个**图书馆管理员**：

- **MySQL** = 地下书库。书都整齐地放在架子上，但每次有人借书，你都得跑到地下、找到那本书、搬上来——**慢**。
- **Redis** = 你办公桌上的一摞"最近常被借的书"。顾客问"有没有《Go 语言圣经》？"——你不用下楼，看一眼桌面就知道了——**快**。

如果顾客要一本你桌面上没有的书，你下楼去取，顺便放一本到桌上（**回填**）。以后再有人问就快了。

但是！如果桌面这摞书和书库里的书**不一致**怎么办？比如有人还了一本书到书库，桌面却没撤掉——顾客会拿到"已经借出去"的书。这就是**缓存一致性**问题。

### 6.2 怎么保证缓存和数据库一致（Cache Aside 模式）

GoRush 用的是最经典的"Cache Aside"模式：

```
读：
    1. 先看 Redis 有没有
    2. 有 → 直接返回（缓存命中）
    3. 没有 → 查 MySQL（缓存未命中）
    4. 拿到后回填 Redis（写一份到桌面）
    5. 返回结果
```

**写：**
```
1. 先改 MySQL
2. 再删 Redis（不是更新，是直接删！）
```

🔍 **为什么是"删"不是"更新"？**

两种做法：
- **方案 A**：写 MySQL → 改 Redis（更新缓存为新值）
- **方案 B**：写 MySQL → 删 Redis（下次读会回填）

GoRush 用方案 B。理由：

方案 A 有并发问题：两个请求同时改 MySQL，顺序可能是 A 写完 MySQL → B 写完 MySQL → B 改 Redis → A 改 Redis——**A 的旧值把 B 的新值覆盖了**，缓存变成旧数据。

方案 B 没这个问题：不管谁先删 Redis，下次读都会重新查 MySQL 拿最新值。

### 6.3 三个常见缓存问题（GoRush 全部解决了）

#### 问题 1：缓存穿透（cache penetration）
**场景**：有人疯狂请求 `GET /shops/99999`，而 id=99999 不存在。每次都查 MySQL 都查不到，每次都"未命中"——每次都要打 MySQL。
**解决**：把"不存在"也缓存一份（null cache），2 分钟过期。这样 2 分钟内所有 99999 请求都从 Redis 直接返"不存在"。

代码：
```go
shop, err := s.Repo.GetByID(ctx, id)
if err == repository.ErrShopNotFound {
    s.Cache.SetNull(ctx, key, 2*time.Minute)  // 缓存"不存在"
    return nil, httpx.NewNotFound(...)
}
```

#### 问题 2：缓存击穿（cache breakdown）
**场景**：某个热门商家的缓存刚好过期，同时有 1000 个请求涌进来。1000 个请求同时发现"缓存没了"，同时去查 MySQL——MySQL 被瞬间打爆。
**解决**：用 **singleflight**（单航班）——同一时间只允许一个请求真的去查 MySQL，其他 999 个等结果。

代码（用了 `golang.org/x/sync/singleflight` 包）：
```go
value, err, _ := s.group.Do(strconv.FormatUint(id, 10), func() (any, error) {
    // 第一个进来的查 MySQL，其他 999 个等它的结果
    return s.Repo.GetByID(ctx, id)
})
```

#### 问题 3：缓存雪崩（cache avalanche）
**场景**：一批商家缓存**同一时间**过期（比如所有缓存都设了 30 分钟），30 分钟后全部过期，下一波请求全部打到 MySQL。
**解决**：给 TTL 加**随机抖动**。

代码：
```go
func jitteredShopTTL() time.Duration {
    return 30*time.Minute + time.Duration(rand.IntN(301))*time.Second
    // TTL 是 30 分钟 + 0~5 分钟随机
}
```

这样所有商家缓存的过期时间都稍微错开，**不会同时过期**。

### 6.4 附近商家怎么实现（Redis GEO）

**Redis GEO** 是 Redis 自带的功能，存经纬度，自动算两个点的距离。

我们用了两个 GEO key：
- `gorush:geo:shops:all` —— 所有在线商家
- `gorush:geo:shops:type:1` —— 美食类的在线商家
- `gorush:geo:shops:type:2` —— 电影类的在线商家
- 等等

存数据：
```bash
GEOADD gorush:geo:shops:all 116.480 39.997 "1"   # 商户 id=1 在经度 116.480 纬度 39.997
GEOADD gorush:geo:shops:all 116.453 39.937 "2"   # 商户 id=2
```

查附近商家：
```bash
GEOSEARCH gorush:geo:shops:all FROMLONLAT 116.48 39.99 BYRADIUS 50000 m WITHCOORD WITHDIST
# 在 (116.48, 39.99) 周围 50000 米内的所有商家，按距离升序，返回坐标+距离。

# 结果：
1) "1"   # 商户 id=1
   116.48  39.997       # 坐标
   778.4507              # 距离 778 米
2) "2"
   ...
```

代码：
```go
locations, err := s.client.GeoSearchLocation(ctx, key, &redis.GeoSearchLocationQuery{
    GeoSearchQuery: redis.GeoSearchQuery{
        Longitude:  lng,
        Latitude:   lat,
        Radius:     radius,
        RadiusUnit: "m",
        Sort:       "ASC",  // 按距离升序
        Count:      size,
    },
})
```

### 6.5 排行榜怎么实现（Redis ZSet）

**ZSet** = 有序集合，每个成员有个分数，按分数自动排序。

存数据：
```bash
ZADD gorush:shop:hot:20260901 5 "1"   # 2026-09-01 这天，商户 1 被访问 5 次
ZADD gorush:shop:hot:20260901 1 "2"
```

每次访问商户详情时 +1：
```bash
ZINCRBY gorush:shop:hot:20260901 1 "1"   # 商户 1 当日热度 +1
```

查排行榜（倒序）：
```bash
ZREVRANGE gorush:shop:hot:20260901 0 9 WITHSCORES
# 拿热度前 10 的商户，按热度从高到低
```

🔍 **为什么按日切 key？** 一个 key 永远累计会越来越大，而且老数据没意义。每天一个新 key，72 小时后过期——简单又自动。

### 6.6 缓存怎么重建（cmd/reindex）

GEO 索引**不会自动跟踪** MySQL 里商家经纬度的变化。所以有个独立的命令 `cmd/reindex`：

```go
// 1. 从 MySQL 查出所有 status=1（在线）的商家
// 2. 删掉旧的 GEO key
// 3. 重新 GEOADD 所有商家
// 4. 删掉旧的 type-specific GEO key
// 5. 按 type_id 分组，重新 GEOADD
```

跑法：
```bash
make reindex
```

什么时候需要跑？
- 第一次启动服务（Redis 是空的）
- 加了新商家
- 改了商家经纬度
- Redis 容器被删了 / 数据丢了

⚠️ **小心**：`make reindex` 只重建 GEO 索引，不会 `FLUSHDB`——其他缓存键（详情缓存、热度榜）不会丢。

### 6.7 Redis 挂了怎么办（优雅降级）

GoRush 把 Redis 分成两类依赖：

| Redis 功能 | 是不是必需的 | Redis 挂了怎么办 |
|-----------|------------|----------------|
| 商家详情缓存 | ❌ 可选加速 | 直接查 MySQL，返回 HTTP 200 |
| 商家券缓存 | ❌ 可选加速 | 直接查 MySQL，返回 HTTP 200 |
| 附近商家（nearby） | ✅ 必需 | 返回 HTTP 503 + 业务码 50301 |
| 热度榜（hot） | ✅ 必需 | 返回 HTTP 503 + 业务码 50301 |
| `/health` | ✅ 监控必需 | 返回 HTTP 503 |

🔍 **为什么这样分？** 详情和券是"加速器"——有它更快，没它也能用。附近商家和热度榜则**完全靠 Redis 实现**，MySQL 算不出来（要现算两个点的距离会很慢、要现排榜要全表扫描）。

### 6.8 /health 现在检查什么

```go
func (h *HealthHandler) Handle(c *gin.Context) {
    // 1. SELECT 1 打 MySQL
    // 2. PING 打 Redis
    // 都 ok → 200 {"status":"ok","checks":{"db":"ok","redis":"ok"}}
    // 任一挂 → 503 {"checks":{"db":"ok","redis":"down: ..."}}
}
```

这样 k8s 的 readiness probe 能能根据这个摘流量——Redis 不可时挂，/health 会主动告诉 k8s 别把流量打过来。

---

## 第 7 章 · 一些设计上的取舍（理解"为什么这么写"）

### 7.1 为什么不用 interface 和依赖注入框架

**取舍**：Day 1 故意**没有**为 Repository / Service 定义 interface，没有用 Wire / fx 之类的 DI 框架。

**原因**：
- Go 里的小项目，写 interface 是过度设计
- 具体类型已经足够灵活——换实现只需改 `router.New()` 里那几行
- 等到真有多个实现（mock、真假两种数据源）再补 interface 不迟

### 7.2 为什么把 user_id 塞在 HTTP 头里（X-User-ID）

**取舍**：Day 1 没做真正的登录态。客户端发请求时带 `X-User-ID: 1` 就假装是用户 1。

**原因**：
- Day 1 关注点不在这——登录态是单独的大功能（JWT / OAuth）
- 但下游业务（"我的收藏"、"抢我的券"）需要知道是谁，所以得有个机制先占位
- 中间件读 `X-User-ID` 写 ctx 的方式**和真实登录态完全一样**——只是来源不同

Day 4+ 会换成 JWT：客户端发 `Authorization: Bearer <token>`，中间件验签后从 token 里抽 user_id 写 ctx。**业务代码一行都不用改**——这就是中间件的好处。

### 7.3 为什么把"事务失败的库存回滚实验"做成开关

**取舍**：代码里有个 `FailMode` 字段，由环境变量控制。

**原因**：
- 实验"**"不能用单元测试做（要真打 MySQL）
- 但生产代码里又不能留"测试代码"
- 用环境变量开关是个折中——开发测试期间开启，生产关掉
- 比单独写一个 `cmd/failtest` 命令更简单

### 7.4 为什么 ctx 一路传到数据库

代码里到处是 `db.WithContext(ctx)`：

```go
// repository/shop.go
err := r.DB.WithContext(ctx).First(&s, id).Error
```

**原因**：客户端断开连接时，HTTP server 会 cancel 请求的 ctx。如果 ctx 传到 driver，MySQL 查询也会自动取消——**避免无效查询浪费 MySQL 资源**。

🔍 **不传 ctx 会怎样**？MySQL 不知道客户端已经走了，还在傻跑查询，浪费资源。10 万 QPS 时这种浪费会很致命。

### 7.5 为什么统一响应格式 `{code, message, data}`

**取舍**：所有接口都返回 `{code, message, data}`。

**原因**：
- 前端能统一解析：永远先看 `code === 0` 判断成功失败
- 不混 HTTP 状态码（200/404/500）和业务码——HTTP 表示"传输成功"，业务码表示"业务成功"。两者各管一摊。

例如：库存售罄是"业务失败但传输成功"——HTTP 200 + body `{code: 60003, message: "out of stock"}`。

---

## 第 8 章 · 常见疑问 FAQ

### Q: GoRush 现在能扛多少 QPS？

**A:** 没压测过，所以不知道。但可以推断：
- **读接口**（商家详情、列表）：Day 2 加了 Redis 缓存后，命中时单实例几千 QPS 不是问题
- **秒杀接口**（Day 1 朴素版）：直接打 MySQL，**几百 QPS** 就开始吃力——这就是 Day 3 要解决的

### Q: 为什么不用 ORM 的 AutoMigrate？

**A:** AutoMigrate 不能表达外键约束、复杂索引、存储过程——真实项目通常用 SQL migration 工具。Day 1 用纯 SQL migration 文件 + 自写的 runner，更接近生产实践。

### Q: 怎么测试我的代码没破其他东西？

**A:** 改完代码跑：

```bash
make verify
```

它会：
1. 跑所有测试（包括真打 Redis 的集成测试）
2. `go vet ./...` 静态检查
3. 编译 4 个 binary（server / migrate / seed / reindex）

任何一个失败都会停在第一步。

### Q: 改了商家经纬度后多久生效？

**A:** **不会自动生效**——必须 `make reindex`。Redis GEO 不像 SQL 那样会主动拉数据。这是 GEO 的一个**已知限制**（README 也写了）。

### Q: 抢秒杀返 `60004 already bought` 是什么意思？

**A:** 你已经抢过这张券了。`UNIQUE(user_id, voucher_id)` 兜底——一人一券。

### Q: 怎么让前端开发我不用看代码就能用？

**A:** 现在 13 个接口的 curl 示例都在 README 里。等 Day 4 会接 Swagger / OpenAPI 自动生成文档。

### Q: 没看到任何 panic 出现，怎么办？

**A:** 这说明你的代码没问题。但 Day 1 的 Recovery 中间件已经准备好了——一旦 panic，会自动返 500 + 错误日志，不会让进程崩。

### Q: 怎么加一个新的接口（比如"猜你喜欢"）？

按这个套路：
1. `internal/model/models.go` 加新表（如果你需要新数据）
2. 写 migration 文件 `migrations/0002_add_xxx.sql`
3. `internal/repository/xxx.go` 写数据库访问
4. `internal/service/xxx.go` 写业务逻辑
5. `internal/handler/xxx.go` 写 HTTP 入口
6. `internal/router/router.go` 注册路由

### Q: 日志怎么送到 ELK / Loki？

现在的日志是 JSON 写到 stderr。接 ELK 只需：
1. 部署一个 filebeat / fluentd 收容器日志
2. 配置好解析规则（直接吃 JSON）
3. 业务代码**完全不用改**

---

## 第 9 章 · Day 3 会做什么（预览）

读了 Day 1 的朴素秒杀、Day 2 的 Redis 缓存，你应该能感受到两个遗留问题：

1. **秒杀性能不够**：每次抢都打 MySQL 的 UPDATE，QPS 高时 MySQL 撑不住
2. **数据库和缓存的一致性有隐患**：万一 Redis 缓存和 MySQL 数据不一致

Day 3 会：

1. **Lua 原子扣减**：把"查库存 + 扣库存"两件事合成一个 Redis Lua 脚本，原子执行。比"先 Redis GET、再 SET"快 10 倍。
2. **RocketMQ 异步落库**：秒杀成功后只改 Redis，异步发消息给 RocketMQ，消费者再写 MySQL。MySQL 不再是性能瓶颈。
3. **幂等键**：用 `request_id` 防重复请求。
4. **限流**：令牌桶限流，恶意刷接口会直接被拦。
5. **分布式锁**：多实例部署时的协调。

---

## 第 10 章 · 一图看完所有接口

```
公开接口 (无需登录)                          需要登录 (X-User-ID)
┌─────────────────────────────────┐    ┌─────────────────────────────────┐
│ GET  /health                    │    │ POST   /shops/:id/reviews       │
│ GET  /api/v1/shop-types          │    │ POST   /shops/:id/favorite     │
│ GET  /api/v1/shops              │    │ DELETE /shops/:id/favorite     │
│ GET  /api/v1/shops/nearby       │    │ GET    /users/me/favorites     │
│ GET  /api/v1/shops/hot          │    │ POST   /seckill/:voucher_id    │
│ GET  /api/v1/shops/:id          │    │ GET    /orders/:id             │
│ GET  /api/v1/shops/:id/vouchers │    └─────────────────────────────────┘
│ GET  /api/v1/shops/:id/reviews  │
│ POST /api/v1/seckill-vouchers   │
└─────────────────────────────────┘
```

---

## 附录 · 关键代码索引（想深入看时来这里查）

| 你想知道 | 看哪里 |
|---------|--------|
| 一个请求从进入到返回的完整代码 | `internal/router/router.go` + `handler/shop.go` + `service/shop.go` + `repository/shop.go` |
| 缓存怎么读 / 怎么写 | `internal/service/shop.go:LookupByID` |
| TTL 随机抖动 | `internal/service/shop.go:jitteredShopTTL` |
| null cache 防穿透 | `internal/redisx/cache.go:SetNull` |
| singleflight 防击穿 | `internal/service/shop.go:s.group.Do(...)` |
| GEO 附近商家 | `internal/redisx/geo.go` + `handler/shop.go:Nearby` |
| ZSet 热度榜 | `internal/redisx/hot.go` + `handler/shop.go:Hot` |
| 朴素秒杀 | `internal/repository/seckill.go:SeckillTx` |
| RequestID 中间件 | `internal/middleware/request_id.go` |
| 日志 + request_id | `internal/httpx/logger.go:FromContext` |
| 事务失败注入 | `internal/repository/seckill.go` (FailMode 字段) |
| 健康检查 + 降级 | `internal/handler/health.go` |
| Redis 索引重建 | `cmd/reindex/main.go` |

---

## 写在最后

读完这份文档，你应该已经能：

- ✅ 说出 GoRush 实现的每个接口
- ✅ 解释一个 HTTP 请求是怎么走的
- ✅ 说清楚 Day 2 解决了 Day 1 的什么问题
- ✅ 解释 Cache Aside / null cache / singleflight / jitter TTL 都是什么
- ✅ 看懂四层代码各在干什么

如果还有不清楚的，按文档里的"关键代码索引"找对应源码看一遍，**比读 100 篇博客有用**。

> **记住**：技能点不是背出来的，是项目逼着学出来的。Day 1 / Day 2 走下来，你已经不知不觉会用很多"高级工程师才懂"的东西了——只是你还没意识到而已。