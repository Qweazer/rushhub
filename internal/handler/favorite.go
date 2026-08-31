package handler

import (
	"strconv"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/service"
)

// FavoriteHandler 收藏 HTTP 处理。
type FavoriteHandler struct {
	Svc *service.FavoriteService
}

func NewFavoriteHandler(svc *service.FavoriteService) *FavoriteHandler {
	return &FavoriteHandler{Svc: svc}
}

// Add POST /api/v1/shops/:id/favorite
func (h *FavoriteHandler) Add(c *gin.Context) {
	shopID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid shop id"))
		return
	}
	if err := h.Svc.Add(c.Request.Context(), shopID); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"favorited": true})
}

// Remove DELETE /api/v1/shops/:id/favorite
func (h *FavoriteHandler) Remove(c *gin.Context) {
	shopID, err := strconv.ParseUint(c.Param("id"), 10, 64)
	if err != nil {
		httpx.Fail(c, httpx.NewBadRequest("invalid shop id"))
		return
	}
	if err := h.Svc.Remove(c.Request.Context(), shopID); err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, gin.H{"favorited": false})
}

// ListMine GET /api/v1/users/me/favorites
func (h *FavoriteHandler) ListMine(c *gin.Context) {
	list, err := h.Svc.ListMine(c.Request.Context())
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, list)
}
