package service

import (
	"context"
	"strings"

	"gorush/internal/httpx"
	"gorush/internal/middleware"
	"gorush/internal/model"
	"gorush/internal/repository"
)

// ReviewService 评价业务。
type ReviewService struct {
	Repo     *repository.ReviewRepository
	ShopRepo *repository.ShopRepository
}

func NewReviewService(rr *repository.ReviewRepository, sr *repository.ShopRepository) *ReviewService {
	return &ReviewService{Repo: rr, ShopRepo: sr}
}

// CreateInput 评价入参。
type CreateReviewInput struct {
	Score   int8   `json:"score"`
	Content string `json:"content"`
}

// Create 写一条评价，user_id 来自 ctx（middleware 注入）。
func (s *ReviewService) Create(ctx context.Context, shopID uint64, in CreateReviewInput) (uint64, error) {
	userID, ok := middleware.UserIDFromContext(ctx)
	if !ok {
		return 0, httpx.NewBadRequest("missing user")
	}

	// 业务校验
	if in.Score < 1 || in.Score > 5 {
		return 0, httpx.NewBadRequest("score must be 1~5")
	}
	in.Content = strings.TrimSpace(in.Content)
	if in.Content == "" {
		return 0, httpx.NewBadRequest("content required")
	}
	if len(in.Content) > 500 {
		return 0, httpx.NewBadRequest("content too long (max 500)")
	}

	// 商家存在性
	if _, err := s.ShopRepo.GetByID(ctx, shopID); err != nil {
		if err == repository.ErrShopNotFound {
			return 0, httpx.NewNotFound("shop not found")
		}
		return 0, httpx.NewInternal(err)
	}

	id, err := s.Repo.Create(ctx, userID, shopID, in.Score, in.Content)
	if err != nil {
		return 0, httpx.NewInternal(err)
	}
	return id, nil
}

// ListByShop 商家评价列表（公开）。
func (s *ReviewService) ListByShop(ctx context.Context, shopID uint64, limit int) ([]model.Review, error) {
	if _, err := s.ShopRepo.GetByID(ctx, shopID); err != nil {
		if err == repository.ErrShopNotFound {
			return nil, httpx.NewNotFound("shop not found")
		}
		return nil, httpx.NewInternal(err)
	}
	list, err := s.Repo.ListByShop(ctx, shopID, limit)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	return list, nil
}
