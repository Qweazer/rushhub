package handler

import (
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// VoucherHandler 营销活动 HTTP 处理。
type VoucherHandler struct {
	Svc *service.VoucherService
}

func NewVoucherHandler(svc *service.VoucherService) *VoucherHandler {
	return &VoucherHandler{Svc: svc}
}

// ListByShop GET /api/v1/shops/:id/vouchers
func (h *VoucherHandler) ListByShop(c *gin.Context) {
	ctx := c.Request.Context()

	id, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid shop id"))
		return
	}
	res, err := h.Svc.ListByShop(ctx, id)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, res)
}

// CreateSeckill POST /api/v1/seckill-vouchers
//
// body:
//
//	{
//	  "shop_id": 1,
//	  "title": "...",
//	  "price": 9900,
//	  "stock": 100,
//	  "begin_time": "2026-09-01T10:00:00+08:00",
//	  "end_time":   "2026-09-02T10:00:00+08:00"
//	}
func (h *VoucherHandler) CreateSeckill(c *gin.Context) {
	var in service.CreateSeckillInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid json: "+err.Error()))
		return
	}
	// 时间字段零值兜底（前后端解析失败时给 12h 默认窗口）
	if in.BeginTime.IsZero() {
		in.BeginTime = time.Now()
	}
	if in.EndTime.IsZero() {
		in.EndTime = in.BeginTime.Add(24 * time.Hour)
	}

	id, err := h.Svc.CreateSeckill(c.Request.Context(), in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"voucher_id": id})
}
