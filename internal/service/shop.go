package service

import (
	"context"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/repository"
)

// ShopService 商家业务。
type ShopService struct {
	Repo *repository.ShopRepository
}

func NewShopService(repo *repository.ShopRepository) *ShopService {
	return &ShopService{Repo: repo}
}

// ListResult 商家列表 + 分页元数据。
type ListResult struct {
	Items []model.Shop `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

// ListPage 商家分页列表。
//   - page 从 1 开始
//   - size 默认 10，限制在 [1, 100]
//   - typeID = 0 表示不按分类过滤
func (s *ShopService) ListPage(ctx context.Context, typeID uint64, page, size int) (*ListResult, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 100 {
		size = 100
	}

	items, total, err := s.Repo.List(ctx, repository.ListFilter{
		TypeID: typeID,
		Offset: (page - 1) * size,
		Limit:  size,
	})
	if err != nil {
		return nil, httpx.NewInternal(err)
	}

	return &ListResult{
		Items: items,
		Total: total,
		Page:  page,
		Size:  size,
	}, nil
}

// GetByID 返回单个商家详情；找不到 → 404 业务错误。
func (s *ShopService) GetByID(ctx context.Context, id uint64) (*model.Shop, error) {
	shop, err := s.Repo.GetByID(ctx, id)
	if err == repository.ErrShopNotFound {
		return nil, httpx.NewNotFound("shop not found")
	}
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	return shop, nil
}
