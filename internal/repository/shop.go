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

// ShopLocation 是 Redis GEO 索引所需的商家位置数据。
type ShopLocation struct {
	ID        uint64
	TypeID    uint64
	Longitude float64
	Latitude  float64
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

// GetByIDs 批量查询商家。
func (r *ShopRepository) GetByIDs(ctx context.Context, ids []uint64) ([]model.Shop, error) {
	if len(ids) == 0 {
		return []model.Shop{}, nil
	}

	shops := make([]model.Shop, 0)
	if err := r.DB.WithContext(ctx).Where("id IN ?", ids).Find(&shops).Error; err != nil {
		return nil, err
	}
	return shops, nil
}

// ListOnlineLocations 返回在线商家重建 Redis GEO 索引所需的位置数据。
func (r *ShopRepository) ListOnlineLocations(ctx context.Context) ([]ShopLocation, error) {
	locations := make([]ShopLocation, 0)
	if err := r.DB.WithContext(ctx).
		Model(&model.Shop{}).
		Select("id, type_id, longitude, latitude").
		Where("status = ?", model.ShopStatusOnline).
		Order("id ASC").
		Find(&locations).Error; err != nil {
		return nil, err
	}
	return locations, nil
}
