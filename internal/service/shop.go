package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand/v2"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/redisx"
	"gorush/internal/repository"
)

const (
	shopCacheTTL              = 30 * time.Minute
	shopNullTTL               = 2 * time.Minute
	nearbyDefaultRadiusMeters = 5_000
	nearbyMaxRadiusMeters     = 50_000
	nearbyDefaultPage         = 1
	nearbyMaxPage             = 100
	nearbyDefaultSize         = 10
	nearbyMaxSize             = 50
	hotDefaultLimit           = 10
	hotMaxLimit               = 100
)

type shopData interface {
	List(context.Context, repository.ListFilter) ([]model.Shop, int64, error)
	GetByID(context.Context, uint64) (*model.Shop, error)
	GetByIDs(context.Context, []uint64) ([]model.Shop, error)
}

type shopRedis interface {
	Get(context.Context, string) (redisx.CacheResult, error)
	MGet(context.Context, []string) ([]redisx.CacheResult, error)
	Set(context.Context, string, []byte, time.Duration) error
	SetNull(context.Context, string, time.Duration) error
	Delete(context.Context, string) error
	GeoSearch(context.Context, uint64, float64, float64, float64, int) ([]redisx.GeoResult, error)
	IncrementHot(context.Context, uint64, time.Time) error
	TopHot(context.Context, time.Time, int) ([]redisx.RankedShop, error)
}

// ShopService 商家业务。
type ShopService struct {
	Repo  shopData
	Cache shopRedis
	group singleflight.Group
}

func NewShopService(repo shopData, cache shopRedis) *ShopService {
	return &ShopService{Repo: repo, Cache: cache}
}

// ListResult 商家列表 + 分页元数据。
type ListResult struct {
	Items []model.Shop `json:"items"`
	Total int64        `json:"total"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

type NearbyQuery struct {
	Longitude, Latitude, RadiusMeters float64
	TypeID                            uint64
	Page, Size                        int
}

type NearbyItem struct {
	Shop           model.Shop `json:"shop"`
	DistanceMeters float64    `json:"distance_m"`
}

type NearbyResult struct {
	Items []NearbyItem `json:"items"`
	Page  int          `json:"page"`
	Size  int          `json:"size"`
}

type HotItem struct {
	Shop  model.Shop `json:"shop"`
	Views int64      `json:"views"`
}

type HotResult struct {
	Items []HotItem `json:"items"`
}

// ListPage 商家分页列表。
//   - page 从 1 开始
//   - size 默认 10，限制在 [1, 100]
//   - typeID = 0 表示不按分类过滤
func (s *ShopService) ListPage(ctx context.Context, typeID uint64, page, size int) (*ListResult, error) {
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 10
	}
	if size > 100 {
		size = 100
	}

	items, total, err := s.Repo.List(ctx, repository.ListFilter{
		TypeID: typeID,
		Offset: (page - 1) * size,
		Limit:  size,
	})
	if err != nil {
		return nil, httpx.NewInternal(err)
	}

	return &ListResult{
		Items: items,
		Total: total,
		Page:  page,
		Size:  size,
	}, nil
}

// LookupByID 返回单个商家详情，但不记录热度；找不到 → 404 业务错误。
func (s *ShopService) LookupByID(ctx context.Context, id uint64) (*model.Shop, error) {
	key := redisx.ShopDetailKey(id)
	if shop, done, err := s.lookupCached(ctx, id, key); done {
		return shop, err
	}

	value, err, _ := s.group.Do(strconv.FormatUint(id, 10), func() (any, error) {
		if shop, done, err := s.lookupCached(ctx, id, key); done {
			return shop, err
		}

		shop, err := s.Repo.GetByID(ctx, id)
		if err == repository.ErrShopNotFound {
			if err := s.Cache.SetNull(ctx, key, shopNullTTL); err != nil {
				logShopCacheError(ctx, "redis_set_null", key, id, err)
			}
			return nil, httpx.NewNotFound("shop not found")
		}
		if err != nil {
			return nil, httpx.NewInternal(err)
		}

		if data, err := json.Marshal(shop); err != nil {
			logShopCacheError(ctx, "cache_marshal", key, id, err)
		} else if err := s.Cache.Set(ctx, key, data, jitteredShopTTL()); err != nil {
			logShopCacheError(ctx, "redis_set", key, id, err)
		}
		return shop, nil
	})
	if err != nil {
		return nil, err
	}
	return value.(*model.Shop), nil
}

func (s *ShopService) lookupCached(ctx context.Context, id uint64, key string) (*model.Shop, bool, error) {
	result, err := s.Cache.Get(ctx, key)
	if err != nil {
		logShopCacheError(ctx, "redis_get", key, id, err)
		return nil, false, nil
	}

	switch result.State {
	case redisx.CacheNull:
		return nil, true, httpx.NewNotFound("shop not found")
	case redisx.CacheHit:
		var shop model.Shop
		if err := json.Unmarshal(result.Data, &shop); err != nil {
			logShopCacheError(ctx, "cache_corruption", key, id, err)
			if err := s.Cache.Delete(ctx, key); err != nil {
				logShopCacheError(ctx, "redis_delete", key, id, err)
			}
			return nil, false, nil
		}
		return &shop, true, nil
	default:
		return nil, false, nil
	}
}

func logShopCacheError(ctx context.Context, operation, key string, shopID uint64, err error) {
	httpx.FromContext(ctx).Error("shop cache operation failed",
		"operation", operation,
		"key", key,
		"shop_id", shopID,
		"error", err,
	)
}

func jitteredShopTTL() time.Duration {
	return shopCacheTTL + time.Duration(rand.IntN(301))*time.Second
}

func (s *ShopService) loadMany(ctx context.Context, ids []uint64) (map[uint64]model.Shop, error) {
	shopsByID := make(map[uint64]model.Shop, len(ids))
	if len(ids) == 0 {
		return shopsByID, nil
	}

	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = redisx.ShopDetailKey(id)
	}

	results, err := s.Cache.MGet(ctx, keys)
	misses := make([]uint64, 0, len(ids))
	if err != nil {
		httpx.FromContext(ctx).Error("shop cache batch operation failed",
			"operation", "redis_mget",
			"key", keys,
			"shop_ids", ids,
			"error", err,
		)
		misses = append(misses, ids...)
	} else if len(results) != len(ids) {
		resultCountErr := fmt.Errorf("Redis MGET returned %d results for %d keys", len(results), len(ids))
		httpx.FromContext(ctx).Error("shop cache batch result corrupt",
			"operation", "cache_corruption",
			"key", keys,
			"shop_ids", ids,
			"result_count", len(results),
			"expected_count", len(ids),
			"error", resultCountErr,
		)
		misses = append(misses, ids...)
	} else {
		for i, result := range results {
			switch result.State {
			case redisx.CacheNull:
				continue
			case redisx.CacheHit:
				var shop model.Shop
				if err := json.Unmarshal(result.Data, &shop); err == nil {
					shopsByID[shop.ID] = shop
					continue
				}
				logShopCacheError(ctx, "cache_corruption", keys[i], ids[i], err)
				if err := s.Cache.Delete(ctx, keys[i]); err != nil {
					logShopCacheError(ctx, "redis_delete", keys[i], ids[i], err)
				}
			}
			misses = append(misses, ids[i])
		}
	}

	if len(misses) == 0 {
		return shopsByID, nil
	}
	shops, err := s.Repo.GetByIDs(ctx, misses)
	if err != nil {
		return nil, httpx.NewInternal(err)
	}
	for _, shop := range shops {
		shopsByID[shop.ID] = shop
		key := redisx.ShopDetailKey(shop.ID)
		if data, err := json.Marshal(shop); err != nil {
			logShopCacheError(ctx, "cache_marshal", key, shop.ID, err)
		} else if err := s.Cache.Set(ctx, key, data, jitteredShopTTL()); err != nil {
			logShopCacheError(ctx, "redis_set", key, shop.ID, err)
		}
	}
	return shopsByID, nil
}

func (s *ShopService) Nearby(ctx context.Context, query NearbyQuery) (*NearbyResult, error) {
	if math.IsNaN(query.Longitude) || query.Longitude < -180 || query.Longitude > 180 {
		return nil, httpx.NewBadRequest("invalid longitude")
	}
	if math.IsNaN(query.Latitude) || query.Latitude < -90 || query.Latitude > 90 {
		return nil, httpx.NewBadRequest("invalid latitude")
	}
	if math.IsNaN(query.RadiusMeters) || query.RadiusMeters <= 0 || query.RadiusMeters > nearbyMaxRadiusMeters {
		return nil, httpx.NewBadRequest("invalid radius")
	}
	if query.Page < nearbyDefaultPage || query.Page > nearbyMaxPage {
		return nil, httpx.NewBadRequest("invalid page")
	}
	if query.Size < 1 || query.Size > nearbyMaxSize {
		return nil, httpx.NewBadRequest("invalid size")
	}

	geoKey := redisx.GeoAllKey()
	if query.TypeID != 0 {
		geoKey = redisx.GeoTypeKey(query.TypeID)
	}
	geoResults, err := s.Cache.GeoSearch(
		ctx,
		query.TypeID,
		query.Longitude,
		query.Latitude,
		query.RadiusMeters,
		query.Page*query.Size,
	)
	if err != nil {
		httpx.FromContext(ctx).Error("nearby shop Redis search failed",
			"operation", "redis_geosearch",
			"key", geoKey,
			"type_id", query.TypeID,
			"error", err,
		)
		return nil, httpx.NewRedisUnavailable(err)
	}

	start := (query.Page - 1) * query.Size
	if start >= len(geoResults) {
		return &NearbyResult{Items: []NearbyItem{}, Page: query.Page, Size: query.Size}, nil
	}
	end := min(start+query.Size, len(geoResults))
	pageResults := geoResults[start:end]
	ids := make([]uint64, len(pageResults))
	for i, result := range pageResults {
		ids[i] = result.ShopID
	}

	shopsByID, err := s.loadMany(ctx, ids)
	if err != nil {
		return nil, err
	}
	items := make([]NearbyItem, 0, len(pageResults))
	for _, result := range pageResults {
		if shop, ok := shopsByID[result.ShopID]; ok {
			items = append(items, NearbyItem{Shop: shop, DistanceMeters: result.DistanceMeters})
		}
	}
	return &NearbyResult{Items: items, Page: query.Page, Size: query.Size}, nil
}

func (s *ShopService) Hot(ctx context.Context, limit int) (*HotResult, error) {
	if limit < 1 {
		limit = hotDefaultLimit
	}
	if limit > hotMaxLimit {
		limit = hotMaxLimit
	}

	day := time.Now()
	ranked, err := s.Cache.TopHot(ctx, day, limit)
	if err != nil {
		httpx.FromContext(ctx).Error("hot shop Redis ranking failed",
			"operation", "redis_zrevrange",
			"key", redisx.HotKey(day),
			"error", err,
		)
		return nil, httpx.NewRedisUnavailable(err)
	}
	ids := make([]uint64, len(ranked))
	for i, item := range ranked {
		ids[i] = item.ShopID
	}
	shopsByID, err := s.loadMany(ctx, ids)
	if err != nil {
		return nil, err
	}

	items := make([]HotItem, 0, len(ranked))
	for _, item := range ranked {
		if shop, ok := shopsByID[item.ShopID]; ok {
			items = append(items, HotItem{Shop: shop, Views: item.Views})
		}
	}
	return &HotResult{Items: items}, nil
}

// GetByID 返回单个商家详情；找不到 → 404 业务错误。
func (s *ShopService) GetByID(ctx context.Context, id uint64) (*model.Shop, error) {
	shop, err := s.LookupByID(ctx, id)
	if err != nil {
		return nil, err
	}
	day := time.Now()
	if err := s.Cache.IncrementHot(ctx, id, day); err != nil {
		httpx.FromContext(ctx).Error("increment shop hot views",
			"operation", "redis_zincrby",
			"key", redisx.HotKey(day),
			"shop_id", id,
			"error", err,
		)
	}
	return shop, nil
}
