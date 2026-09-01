package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type redisPinger interface {
	Ping(context.Context) error
}

// HealthHandler 健康检查处理单元，持有 MySQL 与 Redis 探活依赖。
type HealthHandler struct {
	DB    *gorm.DB
	Redis redisPinger
}

func NewHealthHandler(db *gorm.DB, redis redisPinger) *HealthHandler {
	return &HealthHandler{DB: db, Redis: redis}
}

// Handle 返回进程存活 + DB 连通性。
// 任何一项不健康都返回 503，让 k8s readiness probe 能正确把流量切走。
func (h *HealthHandler) Handle(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "ok"
	redisStatus := "ok"
	status := "ok"
	httpStatus := http.StatusOK
	if err := h.DB.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		dbStatus = "down: " + err.Error()
		status = "down"
		httpStatus = http.StatusServiceUnavailable
	}
	if err := h.Redis.Ping(ctx); err != nil {
		redisStatus = "down: " + err.Error()
		status = "down"
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, gin.H{
		"status": status,
		"checks": gin.H{
			"db":    dbStatus,
			"redis": redisStatus,
		},
	})
}
