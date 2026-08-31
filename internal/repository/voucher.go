package repository

import (
	"context"
	"time"

	"gorm.io/gorm"

	"gorush/internal/model"
)

// VoucherRepository 优惠券 / 秒杀活动数据访问。
type VoucherRepository struct {
	DB *gorm.DB
}

func NewVoucherRepository(db *gorm.DB) *VoucherRepository {
	return &VoucherRepository{DB: db}
}

// ListByShop 返回某商家上架中的所有 vouchers（含普通 + 秒杀 + 推广）。
func (r *VoucherRepository) ListByShop(ctx context.Context, shopID uint64) ([]model.Voucher, error) {
	var list []model.Voucher
	err := r.DB.WithContext(ctx).
		Where("shop_id = ? AND status = ?", shopID, model.VoucherStatusOnline).
		Order("id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// ListSeckillStocksByShop 返回某商家所有秒杀外挂（voucher_id, stock, begin, end）。
// 用 LEFT JOIN 一次拉齐 —— 比先查 vouchers 再 IN(...) 查 stocks 简单。
func (r *VoucherRepository) ListSeckillStocksByShop(ctx context.Context, shopID uint64) ([]model.SeckillVoucher, error) {
	var list []model.SeckillVoucher
	err := r.DB.WithContext(ctx).
		Table("seckill_vouchers sv").
		Select("sv.*").
		Joins("JOIN vouchers v ON v.id = sv.voucher_id").
		Where("v.shop_id = ?", shopID).
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// SeckillInput 创建秒杀活动的入参。
type SeckillInput struct {
	ShopID    uint64
	Title     string
	Price     int // 折扣金额(分)
	Stock     int // 库存
	BeginTime time.Time
	EndTime   time.Time
}

// CreateSeckill 在一个事务里写 vouchers + seckill_vouchers，返回新 voucher id。
//
// 注意 MySQL 的 DDL 不能回滚，但 INSERT 可以。所以用 db.Transaction() 包住两次 INSERT：
// 任一失败 → 自动 ROLLBACK，两张表同时回退到原状。
func (r *VoucherRepository) CreateSeckill(ctx context.Context, in SeckillInput) (uint64, error) {
	var voucherID uint64

	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1) 写 vouchers（秒杀券也按普通券存，只是 voucher_type 标 2）
		res := tx.Exec(`
			INSERT INTO vouchers
				(shop_id, title, voucher_type, discount_value, begin_time, end_time, status)
			VALUES (?, ?, ?, ?, ?, ?, 1)
		`, in.ShopID, in.Title, model.VoucherTypeSeckill, in.Price, in.BeginTime, in.EndTime)
		if res.Error != nil {
			return res.Error
		}
		// 2) 拿刚插入的 id
		var lastID int64
		if err := tx.Raw("SELECT LAST_INSERT_ID()").Row().Scan(&lastID); err != nil {
			return err
		}
		voucherID = uint64(lastID)

		// 3) 写 seckill_vouchers（库存外挂）
		if err := tx.Exec(`
			INSERT INTO seckill_vouchers (voucher_id, stock, begin_time, end_time)
			VALUES (?, ?, ?, ?)
		`, voucherID, in.Stock, in.BeginTime, in.EndTime).Error; err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return 0, err
	}
	return voucherID, nil
}
