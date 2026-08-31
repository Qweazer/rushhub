// Package repository 是数据库访问层。
//
// 设计原则：
//   - 只暴露最朴素的 CRUD，业务规则全部上交给 service。
//   - 所有方法接收 ctx，整条调用链的 cancel/deadline 都能传到底层 driver。
//   - 不返回 GORM 特有的类型给上层 —— 让 service 不依赖 ORM，方便以后替换。
package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"gorush/internal/model"
)

// ShopTypeRepository 商家分类数据访问。
type ShopTypeRepository struct {
	DB *gorm.DB
}

func NewShopTypeRepository(db *gorm.DB) *ShopTypeRepository {
	return &ShopTypeRepository{DB: db}
}

// List 返回所有分类，按 sort asc, id asc。
func (r *ShopTypeRepository) List(ctx context.Context) ([]model.ShopType, error) {
	var list []model.ShopType
	err := r.DB.WithContext(ctx).
		Order("sort ASC, id ASC").
		Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

// GetByID 单条查询，找不到返回 (nil, ErrShopTypeNotFound)。
var ErrShopTypeNotFound = errors.New("shop type not found")

func (r *ShopTypeRepository) GetByID(ctx context.Context, id uint64) (*model.ShopType, error) {
	var t model.ShopType
	err := r.DB.WithContext(ctx).First(&t, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShopTypeNotFound
	}
	if err != nil {
		return nil, err
	}
	return &t, nil
}
