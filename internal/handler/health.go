package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// HealthHandler 健康检查处理单元。
// 从函数升级为 struct 的原因：依赖变多（DB）后，struct 持有依赖是 Go 的自然形态。
// 仍然没有引入 interface —— Day 1 不做"为抽象而抽象"。
type HealthHandler struct {
	DB *gorm.DB
}

func NewHealthHandler(db *gorm.DB) *HealthHandler {
	return &HealthHandler{DB: db}
}

// Handle 返回进程存活 + DB 连通性。
// 任何一项不健康都返回 503，让 k8s readiness probe 能正确把流量切走。
func (h *HealthHandler) Handle(c *gin.Context) {
	ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
	defer cancel()

	dbStatus := "ok"
	httpStatus := http.StatusOK
	if err := h.DB.WithContext(ctx).Exec("SELECT 1").Error; err != nil {
		dbStatus = "down: " + err.Error()
		httpStatus = http.StatusServiceUnavailable
	}

	c.JSON(httpStatus, gin.H{
		"status": "ok",
		"checks": gin.H{
			"db": dbStatus,
		},
	})
}
