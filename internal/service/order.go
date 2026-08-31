package service

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"gorush/internal/httpx"
	"gorush/internal/middleware"
	"gorush/internal/model"
	"gorush/internal/repository"
)

// OrderService 秒杀 + 订单业务。
type OrderService struct {
	SeckillRepo *repository.SeckillRepository
	OrderRepo   *repository.OrderRepository
}

func NewOrderService(sr *repository.SeckillRepository, or *repository.OrderRepository) *OrderService {
	return &OrderService{SeckillRepo: sr, OrderRepo: or}
}

// Seckill 朴素 MySQL 秒杀：
//  1. 业务预检（不持锁），失败时直接返回，不进入事务
//  2. 在一个事务里：条件 UPDATE 库存 + INSERT 订单
//  3. 事务失败 → ROLLBACK，库存自动恢复（由 MySQL 保证）
//
// 这是 Day 1 的"基准版本"。Step 8 会演示：INSERT 失败时 ROLLBACK 真的有效。
// Step 后续会演示：在高并发下"基准版本"会暴露什么问题。
func (s *OrderService) Seckill(ctx context.Context, voucherID uint64) (uint64, error) {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return 0, httpx.NewBadRequest("missing user")
	}

	logger := httpx.FromContext(ctx).With(
		slog.Uint64("user_id", userID),
		slog.Uint64("voucher_id", voucherID),
	)
	logger.Info("seckill attempt start")

	// ---------- 1) 业务预检（不持锁） ----------
	detail, err := s.SeckillRepo.GetByVoucherID(ctx, voucherID)
	if err != nil {
		logger.Error("seckill precheck failed", slog.String("err", err.Error()))
		return 0, httpx.NewInternal(err)
	}
	if detail == nil {
		return 0, httpx.NewNotFound("seckill voucher not found")
	}
	now := time.Now()
	if now.Before(detail.BeginTime) {
		return 0, &httpx.AppError{Code: httpx.CodeVoucherNotStart, Message: "seckill not started"}
	}
	if now.After(detail.EndTime) {
		return 0, &httpx.AppError{Code: httpx.CodeVoucherEnded, Message: "seckill ended"}
	}
	if detail.Stock <= 0 {
		return 0, &httpx.AppError{Code: httpx.CodeOutOfStock, Message: "out of stock"}
	}
	if detail.Status != model.VoucherStatusOnline {
		return 0, httpx.NewBadRequest("voucher offline")
	}

	// ---------- 2) 事务：扣库存 + 写订单 ----------
	orderID, err := s.SeckillRepo.SeckillTx(ctx, userID, detail.ShopID, voucherID, detail)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrOutOfStock):
			logger.Warn("seckill out of stock")
			return 0, &httpx.AppError{Code: httpx.CodeOutOfStock, Message: "out of stock"}
		case errors.Is(err, repository.ErrAlreadyBought):
			logger.Warn("seckill already bought")
			return 0, &httpx.AppError{Code: httpx.CodeAlreadyBought, Message: "already bought"}
		default:
			logger.Error("seckill tx failed", slog.String("err", err.Error()))
			return 0, httpx.NewInternal(err)
		}
	}
	logger.Info("seckill success", slog.Uint64("order_id", orderID))
	return orderID, nil
}

// GetByID 取自己的订单（不是自己的 → 403）。
func (s *OrderService) GetByID(ctx context.Context, id uint64) (*model.Order, error) {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return nil, httpx.NewBadRequest("missing user")
	}
	o, err := s.OrderRepo.GetByIDForUser(ctx, id, userID)
	if err != nil {
		switch {
		case errors.Is(err, repository.ErrOrderNotFound):
			return nil, httpx.NewNotFound("order not found")
		case errors.Is(err, repository.ErrOrderForbidden):
			return nil, &httpx.AppError{Code: 40300, Message: "forbidden"}
		default:
			return nil, httpx.NewInternal(err)
		}
	}
	return o, nil
}
