package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// OrderHandler 秒杀 + 订单 HTTP 处理。
type OrderHandler struct {
	Svc *service.OrderService
}

func NewOrderHandler(svc *service.OrderService) *OrderHandler {
	return &OrderHandler{Svc: svc}
}

// Seckill POST /api/v1/seckill/:voucher_id
// 鉴权：必须带 X-User-ID。
func (h *OrderHandler) Seckill(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("voucher_id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid voucher_id"))
		return
	}
	orderID, err := h.Svc.Seckill(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"order_id": orderID})
}

// GetByID GET /api/v1/orders/:id
func (h *OrderHandler) GetByID(c *gin.Context) {
	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid id"))
		return
	}
	o, err := h.Svc.GetByID(c.Request.Context(), id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, o)
}
