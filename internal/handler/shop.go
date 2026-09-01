package handler

import (
	"context"
	"strconv"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/service"
)

type shopService interface {
	ListPage(context.Context, uint64, int, int) (*service.ListResult, error)
	GetByID(context.Context, uint64) (*model.Shop, error)
	Nearby(context.Context, service.NearbyQuery) (*service.NearbyResult, error)
	Hot(context.Context, int) (*service.HotResult, error)
}

// ShopHandler 商家 HTTP 处理。
type ShopHandler struct {
	Svc shopService
}

func NewShopHandler(svc shopService) *ShopHandler {
	return &ShopHandler{Svc: svc}
}

// List GET /api/v1/shops?type_id=&page=&size=
func (h *ShopHandler) List(c *gin.Context) {
	ctx := c.Request.Context()

	typeID, err := uintQuery(c, "type_id", 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := intQuery(c, "page", 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	size, err := intQuery(c, "size", 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}

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

// Nearby GET /api/v1/shops/nearby?lng=&lat=&radius=&type_id=&page=&size=
func (h *ShopHandler) Nearby(c *gin.Context) {
	longitude, err := requiredFloatQuery(c, "lng")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	latitude, err := requiredFloatQuery(c, "lat")
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	radius, err := floatQuery(c, "radius", 5_000)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	typeID, err := uintQuery(c, "type_id", 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	page, err := intQuery(c, "page", 1)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	size, err := intQuery(c, "size", 10)
	if err != nil {
		httpx.Fail(c, err)
		return
	}

	result, err := h.Svc.Nearby(c.Request.Context(), service.NearbyQuery{
		Longitude:    longitude,
		Latitude:     latitude,
		RadiusMeters: radius,
		TypeID:       typeID,
		Page:         page,
		Size:         size,
	})
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, result)
}

// Hot GET /api/v1/shops/hot?limit=
func (h *ShopHandler) Hot(c *gin.Context) {
	limit, err := intQuery(c, "limit", 0)
	if err != nil {
		httpx.Fail(c, err)
		return
	}

	result, err := h.Svc.Hot(c.Request.Context(), limit)
	if err != nil {
		httpx.Fail(c, err)
		return
	}
	httpx.OK(c, result)
}

func requiredFloatQuery(c *gin.Context, name string) (float64, error) {
	raw, ok := firstQueryValue(c, name)
	if !ok {
		return 0, httpx.NewBadRequest("invalid " + name)
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, httpx.NewBadRequest("invalid " + name)
	}
	return value, nil
}

func floatQuery(c *gin.Context, name string, defaultValue float64) (float64, error) {
	raw, ok := firstQueryValue(c, name)
	if !ok {
		return defaultValue, nil
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return 0, httpx.NewBadRequest("invalid " + name)
	}
	return value, nil
}

func uintQuery(c *gin.Context, name string, defaultValue uint64) (uint64, error) {
	raw, ok := firstQueryValue(c, name)
	if !ok {
		return defaultValue, nil
	}
	value, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, httpx.NewBadRequest("invalid " + name)
	}
	return value, nil
}

func intQuery(c *gin.Context, name string, defaultValue int) (int, error) {
	raw, ok := firstQueryValue(c, name)
	if !ok {
		return defaultValue, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, httpx.NewBadRequest("invalid " + name)
	}
	return value, nil
}

func firstQueryValue(c *gin.Context, name string) (string, bool) {
	values, ok := c.Request.URL.Query()[name]
	if !ok || len(values) == 0 {
		return "", false
	}
	return values[0], true
}
