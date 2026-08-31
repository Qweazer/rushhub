package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// ShopHandler 商家 HTTP 处理。
type ShopHandler struct {
	Svc *service.ShopService
}

func NewShopHandler(svc *service.ShopService) *ShopHandler {
	return &ShopHandler{Svc: svc}
}

// List GET /api/v1/shops?type_id=&page=&size=
func (h *ShopHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	typeID, _ := strconv.ParseUint(c.Query("type_id"), 10, 64)
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("size"))

	res, err := h.Svc.ListPage(ctx, typeID, page, size)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, res)
}

// GetByID GET /api/v1/shops/:id
func (h *ShopHandler) GetByID(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid id"))
		return
	}

	shop, err := h.Svc.GetByID(ctx, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, shop)
}
