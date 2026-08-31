package service

import (
	"context"
	"time"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/repository"
)

// VoucherService 营销活动业务。
type VoucherService struct {
	Repo     *repository.VoucherRepository
	ShopRepo *repository.ShopRepository
}

func NewVoucherService(vr *repository.VoucherRepository, sr *repository.ShopRepository) *VoucherService {
	return &VoucherService{Repo: vr, ShopRepo: sr}
}

// VoucherGrouped 商家营销活动分组视图。
type VoucherGrouped struct {
	Normal    []VoucherView `json:"normal"`
	Seckill   []SeckillView `json:"seckill"`
	Promotion []VoucherView `json:"promotion"`
}

// VoucherView 普通/推广活动视图。
type VoucherView struct {
	ID            uint64    `json:"id"`
	ShopID        uint64    `json:"shop_id"`
	Title         string    `json:"title"`
	Description   string    `json:"description"`
	VoucherType   int8      `json:"voucher_type"`
	DiscountValue int       `json:"discount_value"`
	BeginTime     time.Time `json:"begin_time"`
	EndTime       time.Time `json:"end_time"`
}

// SeckillView 秒杀活动视图（带 stock）。
type SeckillView struct {
	VoucherView
	Stock int `json:"stock"`
}

// ListByShop 返回某商家全部活动，按是否在 seckill_vouchers 分组。
func (s *VoucherService) ListByShop(ctx context.Context, shopID uint64) (*VoucherGrouped, error) {
	// 先校验商家存在 —— 404 否则用户拿到空结果不知道是商家不存在还是没券。
	if _, err := s.ShopRepo.GetByID(ctx, shopID); err != nil {
		if err == repository.ErrShopNotFound {
			return nil, httpx.NewNotFound("shop not found")
		}
		return nil, httpx.NewInternal(err)
	}

	vouchers, err := s.Repo.ListByShop(ctx, shopID)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	stocks, err := s.Repo.ListSeckillStocksByShop(ctx, shopID)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	stockMap := make(map[uint64]model.SeckillVoucher, len(stocks))
	for _, sv := range stocks {
		stockMap[sv.VoucherID] = sv
	}

	out := &VoucherGrouped{
		Normal:    []VoucherView{},
		Seckill:   []SeckillView{},
		Promotion: []VoucherView{},
	}
	for _, v := range vouchers {
		view := VoucherView{
			ID:            v.ID,
			ShopID:        v.ShopID,
			Title:         v.Title,
			Description:   v.Description,
			VoucherType:   v.VoucherType,
			DiscountValue: v.DiscountValue,
			BeginTime:     v.BeginTime,
			EndTime:       v.EndTime,
		}
		if sv, ok := stockMap[v.ID]; ok {
			out.Seckill = append(out.Seckill, SeckillView{
				VoucherView: view,
				Stock:       sv.Stock,
			})
			continue
		}
		// 没库存外挂的：type=1 普通券，type=3 推广
		if v.VoucherType == model.VoucherTypeNormal {
			out.Normal = append(out.Normal, view)
		} else {
			out.Promotion = append(out.Promotion, view)
		}
	}
	return out, nil
}

// CreateSeckillInput 创建秒杀活动入参（来自 HTTP）。
type CreateSeckillInput struct {
	ShopID    uint64    `json:"shop_id"`
	Title     string    `json:"title"`
	Price     int       `json:"price"`
	Stock     int       `json:"stock"`
	BeginTime time.Time `json:"begin_time"`
	EndTime   time.Time `json:"end_time"`
}

// CreateSeckill 业务校验后写两张表（事务）。
func (s *VoucherService) CreateSeckill(ctx context.Context, in CreateSeckillInput) (uint64, error) {
	// 1) 业务校验
	if in.Title == "" {
		return 0, httpx.NewBadRequest("title required")
	}
	if in.Stock <= 0 {
		return 0, httpx.NewBadRequest("stock must be > 0")
	}
	if in.Price <= 0 {
		return 0, httpx.NewBadRequest("price must be > 0")
	}
	if !in.EndTime.After(in.BeginTime) {
		return 0, httpx.NewBadRequest("end_time must be after begin_time")
	}
	if _, err := s.ShopRepo.GetByID(ctx, in.ShopID); err != nil {
		if err == repository.ErrShopNotFound {
			return 0, httpx.NewBadRequest("shop_id not exist")
		}
		return 0, httpx.NewInternal(err)
	}

	// 2) 调 repo（事务写两张表）
	id2, err := s.Repo.CreateSeckill(ctx, repository.SeckillInput{
		ShopID:    in.ShopID,
		Title:     in.Title,
		Price:     in.Price,
		Stock:     in.Stock,
		BeginTime: in.BeginTime,
		EndTime:   in.EndTime,
	})
	if err != nil {
		return 0, httpx.NewInternal(err)
	}
	return id2, nil
}
