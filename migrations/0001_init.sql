-- =====================================================================
-- GoRush 初始化 schema (Day 1)
-- 8 张表：users / shop_types / shops / vouchers / seckill_vouchers /
--        orders / reviews / favorites
-- 设计取舍写在每段 SQL 的注释里，方便回头查。
-- =====================================================================

-- ---------------------------------------------------------------------
-- 1) users：用户表
--    今天不实现登录态，靠 X-User-ID 头模拟当前用户。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS users (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    nickname    VARCHAR(64)     NOT NULL DEFAULT '',
    phone       VARCHAR(20)     NOT NULL DEFAULT '',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_users_phone (phone)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 2) shop_types：商家分类（美食/电影/酒店...）
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS shop_types (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    name        VARCHAR(32)     NOT NULL,
    sort        INT             NOT NULL DEFAULT 0 COMMENT '列表展示顺序，越小越靠前',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_shop_types_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 3) shops：商家
--    longitude/latitude 看似今天没用，但 Day 2 起 Redis GEO 要用，提前留好。
--    score 是 0~5 的平均评分；avg_price 是人均价格（分单位，避免小数）。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS shops (
    id           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    type_id      BIGINT UNSIGNED NOT NULL,
    name         VARCHAR(128)    NOT NULL,
    address      VARCHAR(256)    NOT NULL DEFAULT '',
    longitude    DECIMAL(10, 6)  NOT NULL DEFAULT 0 COMMENT '经度',
    latitude     DECIMAL(10, 6)  NOT NULL DEFAULT 0 COMMENT '纬度',
    score        DECIMAL(2, 1)   NOT NULL DEFAULT 5.0 COMMENT '0.0~5.0 平均评分',
    avg_price    INT             NOT NULL DEFAULT 0 COMMENT '人均价格，分',
    description  VARCHAR(512)    NOT NULL DEFAULT '',
    status       TINYINT         NOT NULL DEFAULT 1 COMMENT '1=正常 0=下架',
    created_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_shops_type_id (type_id),
    KEY idx_shops_status (status)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 4) vouchers：营销活动（普通券 / 推广活动 / 秒杀外挂）
--    voucher_type 区分业务类型：
--      1 = 普通优惠券（满减、折扣）
--      2 = 推广活动（仅展示，无库存）
--    秒杀券在第 5 张表单独挂库存。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS vouchers (
    id              BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    shop_id         BIGINT UNSIGNED NOT NULL,
    title           VARCHAR(128)    NOT NULL,
    description     VARCHAR(512)    NOT NULL DEFAULT '',
    voucher_type    TINYINT         NOT NULL DEFAULT 1 COMMENT '1=普通券 2=推广活动',
    discount_value  INT             NOT NULL DEFAULT 0 COMMENT '满减券填抵扣金额(分)；折扣券填比例*100',
    begin_time      DATETIME        NOT NULL,
    end_time        DATETIME        NOT NULL,
    status          TINYINT         NOT NULL DEFAULT 1 COMMENT '1=上架 0=下架',
    created_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at      DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_vouchers_shop_id (shop_id),
    KEY idx_vouchers_status_time (status, begin_time, end_time)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 5) seckill_vouchers：秒杀券库存外挂
--    voucher_id 既主键又外键：每个 voucher 至多一条秒杀外挂。
--    Day 1 库存全部存 MySQL，故意用最朴素 UPDATE stock=stock-1。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS seckill_vouchers (
    voucher_id  BIGINT UNSIGNED NOT NULL,
    stock       INT             NOT NULL DEFAULT 0,
    begin_time  DATETIME        NOT NULL,
    end_time    DATETIME        NOT NULL,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (voucher_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 6) orders：订单
--    status:
--      1 = PENDING (已下单)
--      2 = PAID    (已支付, 今天不实现支付)
--      3 = CLOSED  (已关闭/超时)
--    UNIQUE(user_id, voucher_id) 是"一人一券"的数据库兜底。
--    version 字段后续学习乐观锁 (WHERE version=? AND version=version+1)。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS orders (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    shop_id     BIGINT UNSIGNED NOT NULL,
    voucher_id  BIGINT UNSIGNED NOT NULL,
    status      TINYINT         NOT NULL DEFAULT 1,
    version     INT             NOT NULL DEFAULT 0,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_orders_user_voucher (user_id, voucher_id),
    KEY idx_orders_user_id (user_id),
    KEY idx_orders_voucher_id (voucher_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 7) reviews：评价
--    score 1~5。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS reviews (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    shop_id     BIGINT UNSIGNED NOT NULL,
    score       TINYINT         NOT NULL,
    content     VARCHAR(512)    NOT NULL DEFAULT '',
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    KEY idx_reviews_shop_id (shop_id),
    KEY idx_reviews_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

-- ---------------------------------------------------------------------
-- 8) favorites：收藏
--    UNIQUE(user_id, shop_id) 防重复收藏。
-- ---------------------------------------------------------------------
CREATE TABLE IF NOT EXISTS favorites (
    id          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    user_id     BIGINT UNSIGNED NOT NULL,
    shop_id     BIGINT UNSIGNED NOT NULL,
    created_at  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (id),
    UNIQUE KEY uk_favorites_user_shop (user_id, shop_id),
    KEY idx_favorites_user_id (user_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;