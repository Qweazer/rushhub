package repository

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"gorush/internal/model"
)

// ShopRepository 商家数据访问。
type ShopRepository struct {
	DB *gorm.DB
}

func NewShopRepository(db *gorm.DB) *ShopRepository {
	return &ShopRepository{DB: db}
}

// ListFilter 列表过滤条件。零值字段表示"不过滤"。
type ListFilter struct {
	TypeID uint64 // 0 = 全部
	Offset int
	Limit  int
}

// List 按 filter 返回商家列表，同时返回总数（用于分页）。
func (r *ShopRepository) List(ctx context.Context, f ListFilter) ([]model.Shop, int64, error) {
	q := r.DB.WithContext(ctx).Model(&model.Shop{})
	if f.TypeID != 0 {
		q = q.Where("type_id = ?", f.TypeID)
	}

	var total int64
	if err := q.Count(&total).Error; err != nil {
		return nil, 0, err
	}

	var shops []model.Shop
	if err := q.
		Where("status = ?", model.ShopStatusOnline).
		Order("id ASC").
		Offset(f.Offset).Limit(f.Limit).
		Find(&shops).Error; err != nil {
		return nil, 0, err
	}
	return shops, total, nil
}

// GetByID 单条查询。
var ErrShopNotFound = errors.New("shop not found")

func (r *ShopRepository) GetByID(ctx context.Context, id uint64) (*model.Shop, error) {
	var s model.Shop
	err := r.DB.WithContext(ctx).First(&s, id).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, ErrShopNotFound
	}
	if err != nil {
		return nil, err
	}
	return &s, nil
}
