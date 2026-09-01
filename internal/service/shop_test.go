package service

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"testing"
	"time"

	"gorush/internal/httpx"
	"gorush/internal/model"
	"gorush/internal/redisx"
	"gorush/internal/repository"
)

type fakeShopData struct {
	shopsByID     map[uint64]model.Shop
	getByIDErr    error
	getByIDsErr   error
	getByIDCalls  []uint64
	getByIDsCalls [][]uint64
}

func (f *fakeShopData) List(context.Context, repository.ListFilter) ([]model.Shop, int64, error) {
	return nil, 0, nil
}

func (f *fakeShopData) GetByID(_ context.Context, id uint64) (*model.Shop, error) {
	f.getByIDCalls = append(f.getByIDCalls, id)
	if f.getByIDErr != nil {
		return nil, f.getByIDErr
	}
	shop, ok := f.shopsByID[id]
	if !ok {
		return nil, repository.ErrShopNotFound
	}
	return &shop, nil
}

func (f *fakeShopData) GetByIDs(_ context.Context, ids []uint64) ([]model.Shop, error) {
	f.getByIDsCalls = append(f.getByIDsCalls, append([]uint64(nil), ids...))
	if f.getByIDsErr != nil {
		return nil, f.getByIDsErr
	}
	shops := make([]model.Shop, 0, len(ids))
	for _, id := range ids {
		if shop, ok := f.shopsByID[id]; ok {
			shops = append(shops, shop)
		}
	}
	return shops, nil
}

type cacheWrite struct {
	key  string
	data []byte
	ttl  time.Duration
}

type fakeShopRedis struct {
	results           map[string]redisx.CacheResult
	getErr            error
	mgetErr           error
	mgetResults       []redisx.CacheResult
	setErr            error
	setNullErr        error
	deleteErr         error
	getCalls          []string
	mgetCalls         [][]string
	sets              []cacheWrite
	setNulls          []cacheWrite
	deletes           []string
	incrementHotCalls []uint64
	incrementHotErr   error
	geoResults        []redisx.GeoResult
	geoErr            error
	geoCalls          []geoSearchCall
	topHotResults     []redisx.RankedShop
	topHotErr         error
	topHotCalls       []topHotCall
}

type topHotCall struct {
	day   time.Time
	limit int
}

type geoSearchCall struct {
	typeID                      uint64
	longitude, latitude, radius float64
	count                       int
}

func (f *fakeShopRedis) Get(_ context.Context, key string) (redisx.CacheResult, error) {
	f.getCalls = append(f.getCalls, key)
	if f.getErr != nil {
		return redisx.CacheResult{}, f.getErr
	}
	if result, ok := f.results[key]; ok {
		return result, nil
	}
	return redisx.CacheResult{State: redisx.CacheMiss}, nil
}

func (f *fakeShopRedis) MGet(_ context.Context, keys []string) ([]redisx.CacheResult, error) {
	f.mgetCalls = append(f.mgetCalls, append([]string(nil), keys...))
	if f.mgetErr != nil {
		return nil, f.mgetErr
	}
	if f.mgetResults != nil {
		return append([]redisx.CacheResult(nil), f.mgetResults...), nil
	}
	results := make([]redisx.CacheResult, len(keys))
	for i, key := range keys {
		if result, ok := f.results[key]; ok {
			results[i] = result
		} else {
			results[i] = redisx.CacheResult{State: redisx.CacheMiss}
		}
	}
	return results, nil
}

func (f *fakeShopRedis) Set(_ context.Context, key string, data []byte, ttl time.Duration) error {
	f.sets = append(f.sets, cacheWrite{key: key, data: append([]byte(nil), data...), ttl: ttl})
	return f.setErr
}

func (f *fakeShopRedis) SetNull(_ context.Context, key string, ttl time.Duration) error {
	f.setNulls = append(f.setNulls, cacheWrite{key: key, ttl: ttl})
	return f.setNullErr
}

func (f *fakeShopRedis) Delete(_ context.Context, key string) error {
	f.deletes = append(f.deletes, key)
	delete(f.results, key)
	return f.deleteErr
}

func (f *fakeShopRedis) GeoSearch(_ context.Context, typeID uint64, longitude, latitude, radius float64, count int) ([]redisx.GeoResult, error) {
	f.geoCalls = append(f.geoCalls, geoSearchCall{
		typeID: typeID, longitude: longitude, latitude: latitude, radius: radius, count: count,
	})
	return append([]redisx.GeoResult(nil), f.geoResults...), f.geoErr
}

func (f *fakeShopRedis) IncrementHot(_ context.Context, shopID uint64, _ time.Time) error {
	f.incrementHotCalls = append(f.incrementHotCalls, shopID)
	return f.incrementHotErr
}

func (f *fakeShopRedis) TopHot(_ context.Context, day time.Time, limit int) ([]redisx.RankedShop, error) {
	f.topHotCalls = append(f.topHotCalls, topHotCall{day: day, limit: limit})
	return append([]redisx.RankedShop(nil), f.topHotResults...), f.topHotErr
}

func TestShopLookupLogsRedisGetFailureWithKey(t *testing.T) {
	shop := model.Shop{ID: 17, Name: "database"}
	key := redisx.ShopDetailKey(shop.ID)
	logs := captureServiceLogs(t)
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{getErr: errors.New("redis unavailable")}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if got == nil || got.ID != shop.ID {
		t.Fatalf("LookupByID() = %#v, want shop %d", got, shop.ID)
	}
	logs.requireShopEvent(t, "redis_get", key, shop.ID)
}

func TestShopLookupLogsCorruptCacheWithKey(t *testing.T) {
	shop := model.Shop{ID: 18, Name: "database"}
	key := redisx.ShopDetailKey(shop.ID)
	logs := captureServiceLogs(t)
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		key: {State: redisx.CacheHit, Data: []byte("not-json")},
	}}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if got == nil || got.ID != shop.ID {
		t.Fatalf("LookupByID() = %#v, want shop %d", got, shop.ID)
	}
	logs.requireShopEvent(t, "cache_corruption", key, shop.ID)
}

func TestShopDetailLogsHotIncrementFailureWithKey(t *testing.T) {
	shop := model.Shop{ID: 19, Name: "cached"}
	key := redisx.ShopDetailKey(shop.ID)
	data, err := json.Marshal(shop)
	if err != nil {
		t.Fatal(err)
	}
	logs := captureServiceLogs(t)
	cache := &fakeShopRedis{
		results:         map[string]redisx.CacheResult{key: {State: redisx.CacheHit, Data: data}},
		incrementHotErr: errors.New("redis unavailable"),
	}

	if _, err := NewShopService(&fakeShopData{}, cache).GetByID(context.Background(), shop.ID); err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	logs.requireShopEvent(t, "redis_zincrby", redisx.HotKey(time.Now()), shop.ID)
}

func TestShopLoadManyLogsResultCountCorruptionAttributes(t *testing.T) {
	ids := []uint64{20, 21}
	keys := []string{redisx.ShopDetailKey(ids[0]), redisx.ShopDetailKey(ids[1])}
	logs := captureServiceLogs(t)
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{
		ids[0]: {ID: ids[0], Name: "first"},
		ids[1]: {ID: ids[1], Name: "second"},
	}}
	cache := &fakeShopRedis{mgetResults: []redisx.CacheResult{}}

	got, err := NewShopService(repo, cache).loadMany(context.Background(), ids)
	if err != nil {
		t.Fatalf("loadMany() error = %v", err)
	}
	if len(got) != len(ids) {
		t.Fatalf("loadMany() = %#v, want %d shops", got, len(ids))
	}
	logs.requireBatchEvent(t, "cache_corruption", keys, ids)
}

func TestShopLookupCacheHitSkipsMySQL(t *testing.T) {
	shop := model.Shop{ID: 7, Name: "cached"}
	data, err := json.Marshal(shop)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeShopData{}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		redisx.ShopDetailKey(shop.ID): {State: redisx.CacheHit, Data: data},
	}}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("LookupByID() = %#v, want %#v", *got, shop)
	}
	if len(repo.getByIDCalls) != 0 {
		t.Fatalf("GetByID calls = %v, want none", repo.getByIDCalls)
	}
}

func TestShopLookupMissLoadsMySQLAndCaches(t *testing.T) {
	shop := model.Shop{ID: 8, Name: "database"}
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("LookupByID() = %#v, want %#v", *got, shop)
	}
	if !reflect.DeepEqual(repo.getByIDCalls, []uint64{shop.ID}) {
		t.Fatalf("GetByID calls = %v, want [%d]", repo.getByIDCalls, shop.ID)
	}
	if len(cache.sets) != 1 {
		t.Fatalf("Set calls = %d, want 1", len(cache.sets))
	}
	if cache.sets[0].key != redisx.ShopDetailKey(shop.ID) {
		t.Fatalf("Set key = %q, want %q", cache.sets[0].key, redisx.ShopDetailKey(shop.ID))
	}
	var cached model.Shop
	if err := json.Unmarshal(cache.sets[0].data, &cached); err != nil {
		t.Fatalf("Set data is not shop JSON: %v", err)
	}
	if !reflect.DeepEqual(cached, shop) {
		t.Fatalf("Set shop = %#v, want %#v", cached, shop)
	}
	if cache.sets[0].ttl < 30*time.Minute || cache.sets[0].ttl > 35*time.Minute {
		t.Fatalf("Set ttl = %s, want within [30m, 35m]", cache.sets[0].ttl)
	}
}

func TestShopLookupMissingWritesNull(t *testing.T) {
	repo := &fakeShopData{getByIDErr: repository.ErrShopNotFound}
	cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}

	_, err := NewShopService(repo, cache).LookupByID(context.Background(), 9)
	assertNotFound(t, err)
	if len(cache.setNulls) != 1 {
		t.Fatalf("SetNull calls = %d, want 1", len(cache.setNulls))
	}
	if cache.setNulls[0].key != redisx.ShopDetailKey(9) || cache.setNulls[0].ttl != 2*time.Minute {
		t.Fatalf("SetNull = %#v, want key %q ttl 2m", cache.setNulls[0], redisx.ShopDetailKey(9))
	}
}

func TestShopLookupNullHitSkipsMySQL(t *testing.T) {
	key := redisx.ShopDetailKey(10)
	repo := &fakeShopData{}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		key: {State: redisx.CacheNull},
	}}

	_, err := NewShopService(repo, cache).LookupByID(context.Background(), 10)
	assertNotFound(t, err)
	if len(repo.getByIDCalls) != 0 {
		t.Fatalf("GetByID calls = %v, want none", repo.getByIDCalls)
	}
}

func TestShopLookupRedisErrorFallsBackToMySQL(t *testing.T) {
	shop := model.Shop{ID: 11, Name: "fallback"}
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{getErr: errors.New("redis unavailable")}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("LookupByID() = %#v, want %#v", *got, shop)
	}
	if !reflect.DeepEqual(repo.getByIDCalls, []uint64{shop.ID}) {
		t.Fatalf("GetByID calls = %v, want [%d]", repo.getByIDCalls, shop.ID)
	}
}

func TestShopLookupCorruptJSONDeletesAndReloads(t *testing.T) {
	shop := model.Shop{ID: 12, Name: "reloaded"}
	key := redisx.ShopDetailKey(shop.ID)
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		key: {State: redisx.CacheHit, Data: []byte("not-json")},
	}}

	got, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("LookupByID() = %#v, want %#v", *got, shop)
	}
	if !reflect.DeepEqual(cache.deletes, []string{key}) {
		t.Fatalf("Delete calls = %v, want [%q]", cache.deletes, key)
	}
	if !reflect.DeepEqual(repo.getByIDCalls, []uint64{shop.ID}) {
		t.Fatalf("GetByID calls = %v, want [%d]", repo.getByIDCalls, shop.ID)
	}
}

func TestJitterTTLStaysWithinBound(t *testing.T) {
	for range 1_000 {
		got := jitteredShopTTL()
		if got < 30*time.Minute || got > 35*time.Minute {
			t.Fatalf("jitteredShopTTL() = %s, want within [30m, 35m]", got)
		}
	}
}

func TestShopGetByIDIncrementsHotAfterSuccess(t *testing.T) {
	shop := model.Shop{ID: 20, Name: "tracked"}
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}

	got, err := NewShopService(repo, cache).GetByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("GetByID() = %#v, want %#v", *got, shop)
	}
	if !reflect.DeepEqual(cache.incrementHotCalls, []uint64{shop.ID}) {
		t.Fatalf("IncrementHot calls = %v, want [%d]", cache.incrementHotCalls, shop.ID)
	}
}

func TestShopGetByIDDoesNotIncrementOnNotFound(t *testing.T) {
	repo := &fakeShopData{getByIDErr: repository.ErrShopNotFound}
	cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}

	_, err := NewShopService(repo, cache).GetByID(context.Background(), 21)
	assertNotFound(t, err)
	if len(cache.incrementHotCalls) != 0 {
		t.Fatalf("IncrementHot calls = %v, want none", cache.incrementHotCalls)
	}
}

func TestShopGetByIDIgnoresHotIncrementFailure(t *testing.T) {
	shop := model.Shop{ID: 22, Name: "still returned"}
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{
		results:         make(map[string]redisx.CacheResult),
		incrementHotErr: errors.New("redis hot unavailable"),
	}

	got, err := NewShopService(repo, cache).GetByID(context.Background(), shop.ID)
	if err != nil {
		t.Fatalf("GetByID() error = %v", err)
	}
	if !reflect.DeepEqual(*got, shop) {
		t.Fatalf("GetByID() = %#v, want %#v", *got, shop)
	}
	if !reflect.DeepEqual(cache.incrementHotCalls, []uint64{shop.ID}) {
		t.Fatalf("IncrementHot calls = %v, want [%d]", cache.incrementHotCalls, shop.ID)
	}
}

func TestShopLookupByIDDoesNotIncrementHot(t *testing.T) {
	shop := model.Shop{ID: 23, Name: "internal lookup"}
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{shop.ID: shop}}
	cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}

	if _, err := NewShopService(repo, cache).LookupByID(context.Background(), shop.ID); err != nil {
		t.Fatalf("LookupByID() error = %v", err)
	}
	if len(cache.incrementHotCalls) != 0 {
		t.Fatalf("IncrementHot calls = %v, want none", cache.incrementHotCalls)
	}
}

func TestShopLoadManyHydratesMissesInOneBatch(t *testing.T) {
	shops := map[uint64]model.Shop{
		1: {ID: 1, Name: "one"},
		2: {ID: 2, Name: "two"},
		3: {ID: 3, Name: "three"},
	}
	cached, err := json.Marshal(shops[3])
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeShopData{shopsByID: shops}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		redisx.ShopDetailKey(3): {State: redisx.CacheHit, Data: cached},
	}}

	got, err := NewShopService(repo, cache).loadMany(context.Background(), []uint64{3, 1, 2})
	if err != nil {
		t.Fatalf("loadMany() error = %v", err)
	}
	if !reflect.DeepEqual(got, shops) {
		t.Fatalf("loadMany() = %#v, want %#v", got, shops)
	}
	wantKeys := []string{redisx.ShopDetailKey(3), redisx.ShopDetailKey(1), redisx.ShopDetailKey(2)}
	if !reflect.DeepEqual(cache.mgetCalls, [][]string{wantKeys}) {
		t.Fatalf("MGet calls = %v, want [%v]", cache.mgetCalls, wantKeys)
	}
	if !reflect.DeepEqual(repo.getByIDsCalls, [][]uint64{{1, 2}}) {
		t.Fatalf("GetByIDs calls = %v, want [[1 2]]", repo.getByIDsCalls)
	}
	if len(cache.sets) != 2 {
		t.Fatalf("Set calls = %d, want 2", len(cache.sets))
	}
	if cache.sets[0].key != redisx.ShopDetailKey(1) || cache.sets[1].key != redisx.ShopDetailKey(2) {
		t.Fatalf("Set keys = [%q %q], want shops 1 and 2", cache.sets[0].key, cache.sets[1].key)
	}
}

func TestShopLoadManyOmitsNullEntries(t *testing.T) {
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{
		4: {ID: 4, Name: "four"},
	}}
	cache := &fakeShopRedis{results: map[string]redisx.CacheResult{
		redisx.ShopDetailKey(3): {State: redisx.CacheNull},
	}}

	got, err := NewShopService(repo, cache).loadMany(context.Background(), []uint64{3, 4})
	if err != nil {
		t.Fatalf("loadMany() error = %v", err)
	}
	if _, ok := got[3]; ok {
		t.Fatalf("loadMany() contains null-cached shop 3: %#v", got[3])
	}
	if !reflect.DeepEqual(got[4], repo.shopsByID[4]) {
		t.Fatalf("loadMany()[4] = %#v, want %#v", got[4], repo.shopsByID[4])
	}
	if !reflect.DeepEqual(repo.getByIDsCalls, [][]uint64{{4}}) {
		t.Fatalf("GetByIDs calls = %v, want [[4]]", repo.getByIDsCalls)
	}
}

func TestShopLoadManyRedisErrorUsesOneBatchQuery(t *testing.T) {
	shops := map[uint64]model.Shop{
		1: {ID: 1, Name: "one"},
		2: {ID: 2, Name: "two"},
		3: {ID: 3, Name: "three"},
	}
	repo := &fakeShopData{shopsByID: shops}
	cache := &fakeShopRedis{getErr: errors.New("redis unavailable")}

	got, err := NewShopService(repo, cache).loadMany(context.Background(), []uint64{3, 1, 2})
	if err != nil {
		t.Fatalf("loadMany() error = %v", err)
	}
	if !reflect.DeepEqual(got, shops) {
		t.Fatalf("loadMany() = %#v, want %#v", got, shops)
	}
	if !reflect.DeepEqual(repo.getByIDsCalls, [][]uint64{{3, 1, 2}}) {
		t.Fatalf("GetByIDs calls = %v, want [[3 1 2]]", repo.getByIDsCalls)
	}
}

func TestShopNearbyRejectsInvalidQuery(t *testing.T) {
	tests := []struct {
		name  string
		query NearbyQuery
	}{
		{name: "longitude below minimum", query: NearbyQuery{Longitude: -180.1, Latitude: 0, RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "longitude above maximum", query: NearbyQuery{Longitude: 180.1, Latitude: 0, RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "longitude NaN", query: NearbyQuery{Longitude: math.NaN(), Latitude: 0, RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "latitude below minimum", query: NearbyQuery{Longitude: 0, Latitude: -90.1, RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "latitude above maximum", query: NearbyQuery{Longitude: 0, Latitude: 90.1, RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "latitude NaN", query: NearbyQuery{Longitude: 0, Latitude: math.NaN(), RadiusMeters: 1, Page: 1, Size: 1}},
		{name: "zero radius", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 0, Page: 1, Size: 1}},
		{name: "radius above maximum", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 50_000.1, Page: 1, Size: 1}},
		{name: "radius NaN", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: math.NaN(), Page: 1, Size: 1}},
		{name: "zero page", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 1, Page: 0, Size: 1}},
		{name: "page above maximum", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 1, Page: 101, Size: 1}},
		{name: "zero size", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 1, Page: 1, Size: 0}},
		{name: "size above maximum", query: NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 1, Page: 1, Size: 51}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &fakeShopRedis{}
			_, err := NewShopService(&fakeShopData{}, cache).Nearby(context.Background(), tt.query)
			assertAppErrorCode(t, err, httpx.CodeBadRequest)
			if len(cache.geoCalls) != 0 {
				t.Fatalf("GeoSearch calls = %v, want none", cache.geoCalls)
			}
		})
	}
}

func TestShopNearbyMapsGeoSearchFailureToRedisUnavailable(t *testing.T) {
	cache := &fakeShopRedis{geoErr: errors.New("redis down")}
	query := NearbyQuery{Longitude: 116.48, Latitude: 39.99, RadiusMeters: 5_000, Page: 1, Size: 10}

	_, err := NewShopService(&fakeShopData{}, cache).Nearby(context.Background(), query)

	assertAppErrorCode(t, err, httpx.CodeRedisUnavailable)
}

func TestShopNearbyPaginatesHydratesAndPreservesGeoOrder(t *testing.T) {
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{
		3: {ID: 3, Name: "three"},
		4: {ID: 4, Name: "four"},
	}}
	cache := &fakeShopRedis{
		results: make(map[string]redisx.CacheResult),
		geoResults: []redisx.GeoResult{
			{ShopID: 1, DistanceMeters: 10.5},
			{ShopID: 2, DistanceMeters: 20.5},
			{ShopID: 3, DistanceMeters: 30.5},
			{ShopID: 4, DistanceMeters: 40.5},
		},
	}
	query := NearbyQuery{
		Longitude: 116.48, Latitude: 39.99, RadiusMeters: 5_000,
		TypeID: 7, Page: 2, Size: 2,
	}

	got, err := NewShopService(repo, cache).Nearby(context.Background(), query)
	if err != nil {
		t.Fatalf("Nearby() error = %v", err)
	}
	wantCall := geoSearchCall{typeID: 7, longitude: 116.48, latitude: 39.99, radius: 5_000, count: 4}
	if !reflect.DeepEqual(cache.geoCalls, []geoSearchCall{wantCall}) {
		t.Fatalf("GeoSearch calls = %#v, want %#v", cache.geoCalls, []geoSearchCall{wantCall})
	}
	if !reflect.DeepEqual(repo.getByIDsCalls, [][]uint64{{3, 4}}) {
		t.Fatalf("GetByIDs calls = %v, want [[3 4]]", repo.getByIDsCalls)
	}
	want := &NearbyResult{
		Items: []NearbyItem{
			{Shop: repo.shopsByID[3], DistanceMeters: 30.5},
			{Shop: repo.shopsByID[4], DistanceMeters: 40.5},
		},
		Page: 2,
		Size: 2,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Nearby() = %#v, want %#v", got, want)
	}
	if len(cache.incrementHotCalls) != 0 {
		t.Fatalf("IncrementHot calls = %v, want none", cache.incrementHotCalls)
	}
}

func TestShopNearbyOmitsStaleShopWithoutReassigningDistance(t *testing.T) {
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{
		10: {ID: 10, Name: "ten"},
		12: {ID: 12, Name: "twelve"},
	}}
	cache := &fakeShopRedis{
		results: make(map[string]redisx.CacheResult),
		geoResults: []redisx.GeoResult{
			{ShopID: 10, DistanceMeters: 100},
			{ShopID: 11, DistanceMeters: 200},
			{ShopID: 12, DistanceMeters: 300},
		},
	}
	query := NearbyQuery{Longitude: 0, Latitude: 0, RadiusMeters: 500, Page: 1, Size: 3}

	got, err := NewShopService(repo, cache).Nearby(context.Background(), query)
	if err != nil {
		t.Fatalf("Nearby() error = %v", err)
	}
	want := []NearbyItem{
		{Shop: repo.shopsByID[10], DistanceMeters: 100},
		{Shop: repo.shopsByID[12], DistanceMeters: 300},
	}
	if !reflect.DeepEqual(got.Items, want) {
		t.Fatalf("Nearby().Items = %#v, want %#v", got.Items, want)
	}
}

func TestShopHotNormalizesLimit(t *testing.T) {
	tests := []struct {
		name      string
		limit     int
		wantLimit int
	}{
		{name: "default", limit: 0, wantLimit: 10},
		{name: "negative uses default", limit: -1, wantLimit: 10},
		{name: "maximum", limit: 101, wantLimit: 100},
		{name: "provided", limit: 7, wantLimit: 7},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cache := &fakeShopRedis{results: make(map[string]redisx.CacheResult)}
			got, err := NewShopService(&fakeShopData{}, cache).Hot(context.Background(), tt.limit)
			if err != nil {
				t.Fatalf("Hot() error = %v", err)
			}
			if len(cache.topHotCalls) != 1 || cache.topHotCalls[0].limit != tt.wantLimit {
				t.Fatalf("TopHot calls = %#v, want one call with limit %d", cache.topHotCalls, tt.wantLimit)
			}
			if got == nil || got.Items == nil || len(got.Items) != 0 {
				t.Fatalf("Hot() = %#v, want result with empty items", got)
			}
		})
	}
}

func TestShopHotMapsTopHotFailureToRedisUnavailable(t *testing.T) {
	cache := &fakeShopRedis{topHotErr: errors.New("redis down")}

	_, err := NewShopService(&fakeShopData{}, cache).Hot(context.Background(), 10)

	assertAppErrorCode(t, err, httpx.CodeRedisUnavailable)
}

func TestShopHotPreservesRankOrderViewsAndOmitsStaleShops(t *testing.T) {
	repo := &fakeShopData{shopsByID: map[uint64]model.Shop{
		3: {ID: 3, Name: "three"},
		1: {ID: 1, Name: "one"},
	}}
	cache := &fakeShopRedis{
		results: make(map[string]redisx.CacheResult),
		topHotResults: []redisx.RankedShop{
			{ShopID: 3, Views: 99},
			{ShopID: 2, Views: 50},
			{ShopID: 1, Views: 7},
		},
	}

	got, err := NewShopService(repo, cache).Hot(context.Background(), 3)
	if err != nil {
		t.Fatalf("Hot() error = %v", err)
	}
	want := &HotResult{Items: []HotItem{
		{Shop: repo.shopsByID[3], Views: 99},
		{Shop: repo.shopsByID[1], Views: 7},
	}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Hot() = %#v, want %#v", got, want)
	}
	if !reflect.DeepEqual(repo.getByIDsCalls, [][]uint64{{3, 2, 1}}) {
		t.Fatalf("GetByIDs calls = %v, want [[3 2 1]]", repo.getByIDsCalls)
	}
	if len(cache.incrementHotCalls) != 0 {
		t.Fatalf("IncrementHot calls = %v, want none", cache.incrementHotCalls)
	}
}

func TestShopHotReturnsHydrationFailure(t *testing.T) {
	repo := &fakeShopData{getByIDsErr: errors.New("database down")}
	cache := &fakeShopRedis{
		results:       make(map[string]redisx.CacheResult),
		topHotResults: []redisx.RankedShop{{ShopID: 1, Views: 5}},
	}

	_, err := NewShopService(repo, cache).Hot(context.Background(), 1)

	assertAppErrorCode(t, err, httpx.CodeInternal)
}

func assertNotFound(t *testing.T, err error) {
	t.Helper()
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Code != httpx.CodeNotFound {
		t.Fatalf("error = %v, want not-found AppError", err)
	}
}

func assertAppErrorCode(t *testing.T, err error, code int) {
	t.Helper()
	var appErr *httpx.AppError
	if !errors.As(err, &appErr) || appErr.Code != code {
		t.Fatalf("error = %v, want AppError code %d", err, code)
	}
}
