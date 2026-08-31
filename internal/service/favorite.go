package service

import (
	"context"
	"errors"

	"github.com/go-sql-driver/mysql"

	"gorush/internal/httpx"
	"gorush/internal/middleware"
	"gorush/internal/repository"
)

// FavoriteService 收藏业务。
type FavoriteService struct {
	Repo     *repository.FavoriteRepository
	ShopRepo *repository.ShopRepository
}

func NewFavoriteService(fr *repository.FavoriteRepository, sr *repository.ShopRepository) *FavoriteService {
	return &FavoriteService{Repo: fr, ShopRepo: sr}
}

// Add 幂等收藏：重复收藏不报错。
func (s *FavoriteService) Add(ctx context.Context, shopID uint64) error {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return httpx.NewBadRequest("missing user")
	}

	if _, err := s.ShopRepo.GetByID(ctx, shopID); err != nil {
		if err == repository.ErrShopNotFound {
			return httpx.NewNotFound("shop not found")
		}
		return httpx.NewInternal(err)
	}

	_, err := s.Repo.Add(ctx, userID, shopID)
	if err != nil {
		// UNIQUE(user_id, shop_id) 冲突 → 视为已收藏（幂等）。
		// MySQL 错误码 1062 = ER_DUP_ENTRY。
		var me *mysql.MySQLError
		if errors.As(err, &me) && me.Number == 1062 {
			return nil
		}
		return httpx.NewInternal(err)
	}
	return nil
}

// Remove 幂等取消收藏。
func (s *FavoriteService) Remove(ctx context.Context, shopID uint64) error {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return httpx.NewBadRequest("missing user")
	}

	if _, err := s.ShopRepo.GetByID(ctx, shopID); err != nil {
		if err == repository.ErrShopNotFound {
			return httpx.NewNotFound("shop not found")
		}
		return httpx.NewInternal(err)
	}

	if _, err := s.Repo.Remove(ctx, userID, shopID); err != nil {
		return httpx.NewInternal(err)
	}
	return nil
}

// ListMine 当前用户的所有收藏。
func (s *FavoriteService) ListMine(ctx context.Context) ([]repository.ShopSummary, error) {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return nil, httpx.NewBadRequest("missing user")
	}
	list, err := s.Repo.ListByUser(ctx, userID)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	return list, nil
}
