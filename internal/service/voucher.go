package service

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"time"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/redisx"
	"gorush/internal/repository"
)

const voucherCacheTTL = 10 * time.Minute

type voucherData interface {
	ListByShop(context.Context, uint64) ([]model.Voucher, error)
	ListSeckillStocksByShop(context.Context, uint64) ([]model.SeckillVoucher, error)
	CreateSeckill(context.Context, repository.SeckillInput) (uint64, error)
}

type voucherCache interface {
	Get(context.Context, string) (redisx.CacheResult, error)
	Set(context.Context, string, []byte, time.Duration) error
	Delete(context.Context, string) error
}

type shopLookup interface {
	LookupByID(context.Context, uint64) (*model.Shop, error)
}

// VoucherService 营销活动业务。
type VoucherService struct {
	Repo       voucherData
	ShopLookup shopLookup
	Cache      voucherCache
}

func NewVoucherService(repo voucherData, shops shopLookup, cache voucherCache) *VoucherService {
	return &VoucherService{Repo: repo, ShopLookup: shops, Cache: cache}
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
	if _, err := s.ShopLookup.LookupByID(ctx, shopID); err != nil {
		return nil, shopLookupError(err, httpx.NewNotFound("shop not found"))
	}

	key := redisx.ShopVouchersKey(shopID)
	if cached, ok := s.loadCached(ctx, shopID, key); ok {
		return cached, nil
	}

	vouchers, err := s.Repo.ListByShop(ctx, shopID)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	stocks, err := s.Repo.ListSeckillStocksByShop(ctx, shopID)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	out := groupVouchers(vouchers, stocks)
	if data, err := json.Marshal(out); err != nil {
		logVoucherCacheError(ctx, "cache_marshal", key, shopID, err)
	} else if err := s.Cache.Set(ctx, key, data, jitteredVoucherTTL()); err != nil {
		logVoucherCacheError(ctx, "redis_set", key, shopID, err)
	}
	return out, nil
}

func (s *VoucherService) loadCached(ctx context.Context, shopID uint64, key string) (*VoucherGrouped, bool) {
	result, err := s.Cache.Get(ctx, key)
	if err != nil {
		logVoucherCacheError(ctx, "redis_get", key, shopID, err)
		return nil, false
	}
	if result.State != redisx.CacheHit {
		return nil, false
	}

	var grouped VoucherGrouped
	if err := json.Unmarshal(result.Data, &grouped); err != nil {
		logVoucherCacheError(ctx, "cache_corruption", key, shopID, err)
		if err := s.Cache.Delete(ctx, key); err != nil {
			logVoucherCacheError(ctx, "redis_delete", key, shopID, err)
		}
		return nil, false
	}
	return &grouped, true
}

func logVoucherCacheError(ctx context.Context, operation, key string, shopID uint64, err error) {
	httpx.FromContext(ctx).Error("voucher cache operation failed",
		"operation", operation,
		"key", key,
		"shop_id", shopID,
		"error", err,
	)
}

func groupVouchers(vouchers []model.Voucher, stocks []model.SeckillVoucher) *VoucherGrouped {
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
			out.Seckill = append(out.Seckill, SeckillView{VoucherView: view, Stock: sv.Stock})
			continue
		}
		if v.VoucherType == model.VoucherTypeNormal {
			out.Normal = append(out.Normal, view)
		} else {
			out.Promotion = append(out.Promotion, view)
		}
	}
	return out
}

func jitteredVoucherTTL() time.Duration {
	return voucherCacheTTL + time.Duration(rand.IntN(301))*time.Second
}

func shopLookupError(err error, missing error) error {
	if errors.Is(err, repository.ErrShopNotFound) {
		return missing
	}
	var appErr *httpx.AppError
	if errors.As(err, &appErr) {
		if appErr.Code == httpx.CodeNotFound {
			return missing
		}
		return err
	}
	return httpx.NewInternal(err)
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
	if _, err := s.ShopLookup.LookupByID(ctx, in.ShopID); err != nil {
		return 0, shopLookupError(err, httpx.NewBadRequest("shop_id not exist"))
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
	key := redisx.ShopVouchersKey(in.ShopID)
	if err := s.Cache.Delete(ctx, key); err != nil {
		logVoucherCacheError(ctx, "redis_delete", key, in.ShopID, err)
	}
	return id2, nil
}
