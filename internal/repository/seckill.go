package repository

import (
	"context"
	"errors"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"gorush/internal/model"
)

// SeckillRepository 秒杀相关数据访问。
type SeckillRepository struct {
	DB       *gorm.DB
	FailMode string // 失败注入模式；空表示关闭。详见 SeckillTx 注释。
}

func NewSeckillRepository(db *gorm.DB, failMode string) *SeckillRepository {
	return &SeckillRepository{DB: db, FailMode: failMode}
}

// SeckillDetail 秒杀活动完整信息（vouchers + seckill_vouchers JOIN 结果）。
type SeckillDetail struct {
	VoucherID uint64    `gorm:"column:voucher_id"`
	ShopID    uint64    `gorm:"column:shop_id"`
	Title     string    `gorm:"column:title"`
	Stock     int       `gorm:"column:stock"`
	BeginTime time.Time `gorm:"column:begin_time"`
	EndTime   time.Time `gorm:"column:end_time"`
	Status    int8      `gorm:"column:status"`
}

// GetByVoucherID 返回某个 voucher 的秒杀详情；非秒杀券返回 (nil, nil)。
func (r *SeckillRepository) GetByVoucherID(ctx context.Context, voucherID uint64) (*SeckillDetail, error) {
	var d SeckillDetail
	err := r.DB.WithContext(ctx).
		Table("seckill_vouchers sv").
		Select("v.id AS voucher_id, v.shop_id AS shop_id, v.title AS title, "+
			"sv.stock AS stock, sv.begin_time AS begin_time, sv.end_time AS end_time, v.status AS status").
		Joins("JOIN vouchers v ON v.id = sv.voucher_id").
		Where("sv.voucher_id = ?", voucherID).
		Take(&d).Error
	if err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// 业务用 sentinel error
var (
	ErrOutOfStock    = errors.New("out of stock")
	ErrAlreadyBought = errors.New("already bought")
	ErrVoucherStale  = errors.New("voucher status changed") // 并发时别人改了 status
)

// SeckillTx 在一个事务里完成：扣库存 + 写订单。
// 返回新订单 id。任何一步失败 → ROLLBACK，库存自动恢复。
//
// 这是 Day 1 的"朴素"实现 —— 没有 Redis / Lua / 分布式锁。
// Step 8 会用并发压测证明这种方式有什么问题。
//
// 实现细节：
//   - UPDATE 用条件 WHERE stock > 0，RowsAffected==0 视为售罄
//   - INSERT 靠 UNIQUE(user_id, voucher_id) 兜底防重复
//   - 把 detail 当作"快照"传入，避免事务内再去查（一致性 + 减少锁竞争）
//   - **不**加 SELECT FOR UPDATE，让 MySQL 默认锁机制处理并发
func (r *SeckillRepository) SeckillTx(
	ctx context.Context,
	userID, shopID, voucherID uint64,
	preCheckSnapshot *SeckillDetail,
) (uint64, error) {
	var orderID uint64

	err := r.DB.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		// 1) 条件 UPDATE 库存
		res := tx.Exec(`
			UPDATE seckill_vouchers
			SET stock = stock - 1
			WHERE voucher_id = ? AND stock > 0
		`, voucherID)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			// 库存已耗尽（即便快照里有库存，也被别人抢光了）
			return ErrOutOfStock
		}

		// ---------- 失败注入点 ----------
		// Step 8 教学用：人为制造 UPDATE 之后、INSERT 之前的失败，
		// 用来证明事务回滚会让库存自动恢复。
		// 由 SECKILL_FAIL_AFTER_UPDATE 环境变量开启。
		if r.FailMode == "after_update" {
			return errors.New("simulated failure after UPDATE stock")
		}

		// 2) INSERT 订单（UNIQUE 兜底一人一券）
		ins := tx.Exec(`
			INSERT INTO orders(user_id, shop_id, voucher_id, status)
			VALUES (?, ?, ?, ?)
		`, userID, shopID, voucherID, model.OrderStatusPending)
		if ins.Error != nil {
			var me *mysql.MySQLError
			if errors.As(ins.Error, &me) && me.Number == 1062 {
				return ErrAlreadyBought
			}
			return ins.Error
		}

		// 拿刚插入的 id
		var id int64
		if err := tx.Raw("SELECT LAST_INSERT_ID()").Row().Scan(&id); err != nil {
			return err
		}
		orderID = uint64(id)
		return nil
	})

	if err != nil {
		return 0, err
	}
	return orderID, nil
}

// ============================================================
// OrderRepository
// ============================================================

type OrderRepository struct {
	DB *gorm.DB
}

func NewOrderRepository(db *gorm.DB) *OrderRepository {
	return &OrderRepository{DB: db}
}

// ErrOrderNotFound 单查不到。
var ErrOrderNotFound = gorm.ErrRecordNotFound

// ErrOrderForbidden 订单存在但不属于当前用户。
var ErrOrderForbidden = gorm.ErrInvalidData

// GetByIDForUser 取一条订单，校验所有权。
func (r *OrderRepository) GetByIDForUser(ctx context.Context, id, userID uint64) (*model.Order, error) {
	var o model.Order
	err := r.DB.WithContext(ctx).Where("id = ?", id).Take(&o).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ErrOrderNotFound
		}
		return nil, err
	}
	if o.UserID != userID {
		return nil, ErrOrderForbidden
	}
	return &o, nil
}
