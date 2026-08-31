package repository

import (
	"context"

	"gorm.io/gorm"

	"gorush/internal/model"
)

// ReviewRepository 评价数据访问。
type ReviewRepository struct {
	DB *gorm.DB
}

func NewReviewRepository(db *gorm.DB) *ReviewRepository {
	return &ReviewRepository{DB: db}
}

// Create 插入一条评价，返回新 id。
func (r *ReviewRepository) Create(ctx context.Context, userID, shopID uint64, score int8, content string) (uint64, error) {
	res := r.DB.WithContext(ctx).Exec(`
		INSERT INTO reviews(user_id, shop_id, score, content)
		VALUES (?, ?, ?, ?)
	`, userID, shopID, score, content)
	if res.Error != nil {
		return 0, res.Error
	}
	var id int64
	if err := r.DB.WithContext(ctx).Raw("SELECT LAST_INSERT_ID()").Row().Scan(&id); err != nil {
		return 0, err
	}
	return uint64(id), nil
}

// ListByShop 返回某商家的评价，最新的在前。
func (r *ReviewRepository) ListByShop(ctx context.Context, shopID uint64, limit int) ([]model.Review, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []model.Review
	err := r.DB.WithContext(ctx).
		Where("shop_id = ?", shopID).
		Order("id DESC").
		Limit(limit).
		Find(&list).Error
	return list, err
}
