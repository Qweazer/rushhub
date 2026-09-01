package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/service"
)

type fakeShopService struct {
	listResult   *service.ListResult
	listTypeID   uint64
	listPage     int
	listSize     int
	listCalls    int
	nearbyResult *service.NearbyResult
	nearbyQuery  service.NearbyQuery
	nearbyCalls  int
	hotResult    *service.HotResult
	hotLimit     int
	hotCalls     int
}

func (f *fakeShopService) ListPage(_ context.Context, typeID uint64, page, size int) (*service.ListResult, error) {
	f.listCalls++
	f.listTypeID = typeID
	f.listPage = page
	f.listSize = size
	return f.listResult, nil
}

func (f *fakeShopService) GetByID(context.Context, uint64) (*model.Shop, error) {
	return nil, nil
}

func (f *fakeShopService) Nearby(_ context.Context, query service.NearbyQuery) (*service.NearbyResult, error) {
	f.nearbyCalls++
	f.nearbyQuery = query
	return f.nearbyResult, nil
}

func (f *fakeShopService) Hot(_ context.Context, limit int) (*service.HotResult, error) {
	f.hotCalls++
	f.hotLimit = limit
	return f.hotResult, nil
}

func TestShopHandler_Nearby(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("parses coordinates and supplies defaults", func(t *testing.T) {
		wantResult := &service.NearbyResult{Items: []service.NearbyItem{}, Page: 1, Size: 10}
		svc := &fakeShopService{nearbyResult: wantResult}
		router := gin.New()
		router.GET("/api/v1/shops/nearby", NewShopHandler(svc).Nearby)

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/shops/nearby?lng=116.48&lat=39.99", nil)
		router.ServeHTTP(response, request)

		if response.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
		}
		if svc.nearbyCalls != 1 {
			t.Fatalf("nearby calls = %d, want 1", svc.nearbyCalls)
		}
		wantQuery := service.NearbyQuery{
			Longitude:    116.48,
			Latitude:     39.99,
			RadiusMeters: 5_000,
			Page:         1,
			Size:         10,
		}
		if svc.nearbyQuery != wantQuery {
			t.Fatalf("nearby query = %#v, want %#v", svc.nearbyQuery, wantQuery)
		}

		var body struct {
			Code int                   `json:"code"`
			Data *service.NearbyResult `json:"data"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if body.Code != httpx.CodeOK {
			t.Fatalf("code = %d, want %d", body.Code, httpx.CodeOK)
		}
		if body.Data == nil || body.Data.Page != 1 || body.Data.Size != 10 {
			t.Fatalf("data = %#v, want nearby result envelope", body.Data)
		}
	})

	malformedQueries := []string{
		"lng=bad&lat=39.99",
		"lng=116.48&lat=bad",
		"lng=116.48&lat=39.99&radius=bad",
		"lng=116.48&lat=39.99&type_id=bad",
		"lng=116.48&lat=39.99&page=bad",
		"lng=116.48&lat=39.99&size=bad",
	}
	for _, query := range malformedQueries {
		t.Run("rejects malformed numeric query "+query, func(t *testing.T) {
			svc := &fakeShopService{}
			router := gin.New()
			router.GET("/api/v1/shops/nearby", NewShopHandler(svc).Nearby)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/shops/nearby?"+query, nil)
			router.ServeHTTP(response, request)

			assertBadRequestWithoutServiceCall(t, response, svc.nearbyCalls)
		})
	}
}

func TestShopHandler_Hot(t *testing.T) {
	gin.SetMode(gin.TestMode)

	wantResult := &service.HotResult{Items: []service.HotItem{}}
	svc := &fakeShopService{hotResult: wantResult}
	router := gin.New()
	router.GET("/api/v1/shops/hot", NewShopHandler(svc).Hot)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/v1/shops/hot?limit=5", nil)
	router.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusOK, response.Body.String())
	}
	if svc.hotCalls != 1 {
		t.Fatalf("hot calls = %d, want 1", svc.hotCalls)
	}
	if svc.hotLimit != 5 {
		t.Fatalf("hot limit = %d, want 5", svc.hotLimit)
	}
	var body struct {
		Code int                `json:"code"`
		Data *service.HotResult `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != httpx.CodeOK {
		t.Fatalf("code = %d, want %d", body.Code, httpx.CodeOK)
	}
	if body.Data == nil || body.Data.Items == nil {
		t.Fatalf("data = %#v, want hot result envelope", body.Data)
	}

	t.Run("rejects malformed limit before calling service", func(t *testing.T) {
		svc := &fakeShopService{}
		router := gin.New()
		router.GET("/api/v1/shops/hot", NewShopHandler(svc).Hot)

		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodGet, "/api/v1/shops/hot?limit=bad", nil)
		router.ServeHTTP(response, request)

		assertBadRequestWithoutServiceCall(t, response, svc.hotCalls)
	})
}

func TestShopHandler_ListRejectsMalformedNumericQueries(t *testing.T) {
	gin.SetMode(gin.TestMode)

	for _, query := range []string{"type_id=bad", "page=bad", "size=bad"} {
		t.Run(query, func(t *testing.T) {
			svc := &fakeShopService{}
			router := gin.New()
			router.GET("/api/v1/shops", NewShopHandler(svc).List)

			response := httptest.NewRecorder()
			request := httptest.NewRequest(http.MethodGet, "/api/v1/shops?"+query, nil)
			router.ServeHTTP(response, request)

			assertBadRequestWithoutServiceCall(t, response, svc.listCalls)
		})
	}
}

func assertBadRequestWithoutServiceCall(t *testing.T, response *httptest.ResponseRecorder, calls int) {
	t.Helper()

	if response.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d; body = %s", response.Code, http.StatusBadRequest, response.Body.String())
	}
	if calls != 0 {
		t.Fatalf("service calls = %d, want 0", calls)
	}
	var body struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if body.Code != httpx.CodeBadRequest {
		t.Fatalf("code = %d, want %d", body.Code, httpx.CodeBadRequest)
	}
}
