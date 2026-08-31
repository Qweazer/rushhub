// Package service 是业务层。
//
// 设计原则：
//   - 不依赖 Gin / HTTP 任何东西，方便以后换 gRPC / 队列消费等场景。
//   - 不直接拼 SQL；所有数据访问走 repository。
//   - 校验 + 业务规则在这一层；非法输入返回 *httpx.AppError。
//   - ctx 一路传下去，取消 / 超时由调用方控制。
package service

import (
	"context"

	"gorush/internal/model"
	"gorush/internal/repository"
)

// ShopTypeService 商家分类业务。
type ShopTypeService struct {
	Repo *repository.ShopTypeRepository
}

func NewShopTypeService(repo *repository.ShopTypeRepository) *ShopTypeService {
	return &ShopTypeService{Repo: repo}
}

// List 返回所有分类。Day 1 不分页，分类总共不到 20 个。
func (s *ShopTypeService) List(ctx context.Context) ([]model.ShopType, error) {
	return s.Repo.List(ctx)
}
