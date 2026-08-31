package repository

import (
	"context"

	"gorm.io/gorm"
)

// FavoriteRepository 收藏数据访问。
type FavoriteRepository struct {
	DB *gorm.DB
}

func NewFavoriteRepository(db *gorm.DB) *FavoriteRepository {
	return &FavoriteRepository{DB: db}
}

// Add 给 (user_id, shop_id) 插入一条收藏。
// UNIQUE(user_id, shop_id) 兜底：调用方根据 RowsAffected 判断是否新增。
// 返回 rowsAffected：1=新增成功, 0=已存在(幂等)。
func (r *FavoriteRepository) Add(ctx context.Context, userID, shopID uint64) (int64, error) {
	res := r.DB.WithContext(ctx).Exec(`
		INSERT INTO favorites(user_id, shop_id) VALUES (?, ?)
	`, userID, shopID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// Remove 删除某用户的某商家收藏。
// rowsAffected：1=删了一条, 0=本来就没收藏(幂等)。
func (r *FavoriteRepository) Remove(ctx context.Context, userID, shopID uint64) (int64, error) {
	res := r.DB.WithContext(ctx).Exec(`
		DELETE FROM favorites WHERE user_id = ? AND shop_id = ?
	`, userID, shopID)
	if res.Error != nil {
		return 0, res.Error
	}
	return res.RowsAffected, nil
}

// ShopSummary 收藏列表里嵌入的商家精简信息。
type ShopSummary struct {
	ID      uint64  `json:"id"`
	Name    string  `json:"name"`
	Address string  `json:"address"`
	Score   float32 `json:"score"`
}

// ListByUser 返回某用户的所有收藏，JOIN shops 带出商家精简信息。
func (r *FavoriteRepository) ListByUser(ctx context.Context, userID uint64) ([]ShopSummary, error) {
	var out []ShopSummary
	err := r.DB.WithContext(ctx).
		Table("favorites f").
		Select("s.id AS id, s.name AS name, s.address AS address, s.score AS score").
		Joins("JOIN shops s ON s.id = f.shop_id").
		Where("f.user_id = ?", userID).
		Order("f.id DESC").
		Find(&out).Error
	return out, err
}
