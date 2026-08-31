// Package model 集中放 GORM 模型 struct。
//
// 设计原则：
//   - 表结构由 migrations/*.sql 维护（更接近生产实践）。
//   - struct 仅用作"行 ↔ Go 值"的映射，承载 GORM tags、Repository 的返回类型。
//   - 不在这里写业务方法 —— 业务在 service 层。
package model

import "time"

// ===== Status 常量 =====
const (
	ShopStatusOnline  = 1
	ShopStatusOffline = 0

	VoucherStatusOnline  = 1
	VoucherStatusOffline = 0

	VoucherTypeNormal  = 1 // 普通优惠券
	VoucherTypeSeckill = 2 // 秒杀券（必有 seckill_vouchers 行）
	VoucherTypePromo   = 3 // 推广活动（仅展示）

	OrderStatusPending = 1
	OrderStatusPaid    = 2
	OrderStatusClosed  = 3
)

// ===== 用户 =====
type User struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	Nickname  string    `gorm:"column:nickname"`
	Phone     string    `gorm:"column:phone;uniqueIndex:uk_users_phone"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (User) TableName() string { return "users" }

// ===== 商家分类 =====
type ShopType struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	Name      string    `gorm:"column:name"`
	Sort      int       `gorm:"column:sort"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (ShopType) TableName() string { return "shop_types" }

// ===== 商家 =====
type Shop struct {
	ID          uint64    `gorm:"primaryKey;column:id"`
	TypeID      uint64    `gorm:"column:type_id"`
	Name        string    `gorm:"column:name"`
	Address     string    `gorm:"column:address"`
	Longitude   float64   `gorm:"column:longitude"`
	Latitude    float64   `gorm:"column:latitude"`
	Score       float32   `gorm:"column:score"`
	AvgPrice    int       `gorm:"column:avg_price"`
	Description string    `gorm:"column:description"`
	Status      int8      `gorm:"column:status"`
	CreatedAt   time.Time `gorm:"column:created_at"`
	UpdatedAt   time.Time `gorm:"column:updated_at"`
}

func (Shop) TableName() string { return "shops" }

// ===== 营销活动 =====
type Voucher struct {
	ID            uint64    `gorm:"primaryKey;column:id"`
	ShopID        uint64    `gorm:"column:shop_id"`
	Title         string    `gorm:"column:title"`
	Description   string    `gorm:"column:description"`
	VoucherType   int8      `gorm:"column:voucher_type"`
	DiscountValue int       `gorm:"column:discount_value"`
	BeginTime     time.Time `gorm:"column:begin_time"`
	EndTime       time.Time `gorm:"column:end_time"`
	Status        int8      `gorm:"column:status"`
	CreatedAt     time.Time `gorm:"column:created_at"`
	UpdatedAt     time.Time `gorm:"column:updated_at"`
}

func (Voucher) TableName() string { return "vouchers" }

// ===== 秒杀库存外挂 =====
type SeckillVoucher struct {
	VoucherID uint64    `gorm:"primaryKey;column:voucher_id"`
	Stock     int       `gorm:"column:stock"`
	BeginTime time.Time `gorm:"column:begin_time"`
	EndTime   time.Time `gorm:"column:end_time"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (SeckillVoucher) TableName() string { return "seckill_vouchers" }

// ===== 订单 =====
type Order struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	UserID    uint64    `gorm:"column:user_id"`
	ShopID    uint64    `gorm:"column:shop_id"`
	VoucherID uint64    `gorm:"column:voucher_id"`
	Status    int8      `gorm:"column:status"`
	Version   int       `gorm:"column:version"`
	CreatedAt time.Time `gorm:"column:created_at"`
	UpdatedAt time.Time `gorm:"column:updated_at"`
}

func (Order) TableName() string { return "orders" }

// ===== 评价 =====
type Review struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	UserID    uint64    `gorm:"column:user_id"`
	ShopID    uint64    `gorm:"column:shop_id"`
	Score     int8      `gorm:"column:score"`
	Content   string    `gorm:"column:content"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (Review) TableName() string { return "reviews" }

// ===== 收藏 =====
type Favorite struct {
	ID        uint64    `gorm:"primaryKey;column:id"`
	UserID    uint64    `gorm:"column:user_id"`
	ShopID    uint64    `gorm:"column:shop_id"`
	CreatedAt time.Time `gorm:"column:created_at"`
}

func (Favorite) TableName() string { return "favorites" }
