package handler

import (
	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// ShopTypeHandler 商家分类 HTTP 处理。
type ShopTypeHandler struct {
	Svc *service.ShopTypeService
}

func NewShopTypeHandler(svc *service.ShopTypeService) *ShopTypeHandler {
	return &ShopTypeHandler{Svc: svc}
}

// List GET /api/v1/shop-types
func (h *ShopTypeHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	list, err := h.Svc.List(ctx)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, list)
}
