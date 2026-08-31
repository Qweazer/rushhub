package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// ReviewHandler 评价 HTTP 处理。
type ReviewHandler struct {
	Svc *service.ReviewService
}

func NewReviewHandler(svc *service.ReviewService) *ReviewHandler {
	return &ReviewHandler{Svc: svc}
}

// Create POST /api/v1/shops/:id/reviews
func (h *ReviewHandler) Create(c *gin.Context) {
	ctx := c.Request.Context()

	shopID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid shop id"))
		return
	}
	var in service.CreateReviewInput
	if err := c.ShouldBindJSON(&in); err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid json: "+err.Error()))
		return
	}

	id, err := h.Svc.Create(ctx, shopID, in)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"review_id": id})
}

// ListByShop GET /api/v1/shops/:id/reviews
func (h *ReviewHandler) ListByShop(c *gin.Context) {
	ctx := c.Request.Context()

	shopID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid shop id"))
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := h.Svc.ListByShop(ctx, shopID, limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, list)
}
