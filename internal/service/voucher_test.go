package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"testing"
	"time"

	"gorush/internal/model"
	"gorush/internal/redisx"
	"gorush/internal/repository"
)

type fakeVoucherData struct {
	vouchers       []model.Voucher
	stocks         []model.SeckillVoucher
	listErr        error
	stocksErr      error
	createID       uint64
	createErr      error
	listCalls      []uint64
	stockListCalls []uint64
	createCalls    []repository.SeckillInput
}

func (f *fakeVoucherData) ListByShop(_ context.Context, shopID uint64) ([]model.Voucher, error) {
	f.listCalls = append(f.listCalls, shopID)
	return f.vouchers, f.listErr
}

func (f *fakeVoucherData) ListSeckillStocksByShop(_ context.Context, shopID uint64) ([]model.SeckillVoucher, error) {
	f.stockListCalls = append(f.stockListCalls, shopID)
	return f.stocks, f.stocksErr
}

func (f *fakeVoucherData) CreateSeckill(_ context.Context, in repository.SeckillInput) (uint64, error) {
	f.createCalls = append(f.createCalls, in)
	return f.createID, f.createErr
}

type fakeShopLookup struct {
	shop  *model.Shop
	err   error
	calls []uint64
}

func (f *fakeShopLookup) LookupByID(_ context.Context, shopID uint64) (*model.Shop, error) {
	f.calls = append(f.calls, shopID)
	return f.shop, f.err
}

type voucherCacheWrite struct {
	key  string
	data []byte
	ttl  time.Duration
}

type fakeVoucherCache struct {
	results   map[string]redisx.CacheResult
	getErr    error
	setErr    error
	deleteErr error
	getCalls  []string
	sets      []voucherCacheWrite
	deletes   []string
}

func (f *fakeVoucherCache) Get(_ context.Context, key string) (redisx.CacheResult, error) {
	f.getCalls = append(f.getCalls, key)
	if f.getErr != nil {
		return redisx.CacheResult{}, f.getErr
	}
	if result, ok := f.results[key]; ok {
		return result, nil
	}
	return redisx.CacheResult{State: redisx.CacheMiss}, nil
}

func (f *fakeVoucherCache) Set(_ context.Context, key string, data []byte, ttl time.Duration) error {
	f.sets = append(f.sets, voucherCacheWrite{key: key, data: append([]byte(nil), data...), ttl: ttl})
	return f.setErr
}

func (f *fakeVoucherCache) Delete(_ context.Context, key string) error {
	f.deletes = append(f.deletes, key)
	delete(f.results, key)
	return f.deleteErr
}

func TestVoucherListLogsRedisGetFailureWithKey(t *testing.T) {
	shopID := uint64(17)
	key := redisx.ShopVouchersKey(shopID)
	logs := captureServiceLogs(t)
	repo := &fakeVoucherData{}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{getErr: errors.New("redis unavailable")}

	if _, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID); err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	logs.requireShopEvent(t, "redis_get", key, shopID)
}

func TestVoucherListLogsCacheCorruptionWithKey(t *testing.T) {
	shopID := uint64(18)
	key := redisx.ShopVouchersKey(shopID)
	logs := captureServiceLogs(t)
	repo := &fakeVoucherData{}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{results: map[string]redisx.CacheResult{
		key: {State: redisx.CacheHit, Data: []byte("not-json")},
	}}

	if _, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID); err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	logs.requireShopEvent(t, "cache_corruption", key, shopID)
}

func TestVoucherCreateSeckillLogsCacheInvalidationFailureWithKey(t *testing.T) {
	shopID := uint64(19)
	key := redisx.ShopVouchersKey(shopID)
	logs := captureServiceLogs(t)
	repo := &fakeVoucherData{createID: 99}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{deleteErr: errors.New("redis unavailable")}

	if _, err := NewVoucherService(repo, lookup, cache).CreateSeckill(context.Background(), validSeckillInput(shopID)); err != nil {
		t.Fatalf("CreateSeckill() error = %v", err)
	}
	logs.requireShopEvent(t, "redis_delete", key, shopID)
}

func TestVoucherListCacheHitSkipsRepository(t *testing.T) {
	shopID := uint64(7)
	want := &VoucherGrouped{Normal: []VoucherView{{ID: 1, ShopID: shopID, Title: "cached"}}, Seckill: []SeckillView{}, Promotion: []VoucherView{}}
	data, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	repo := &fakeVoucherData{}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{results: map[string]redisx.CacheResult{
		redisx.ShopVouchersKey(shopID): {State: redisx.CacheHit, Data: data},
	}}

	got, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID)
	if err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ListByShop() = %#v, want %#v", got, want)
	}
	if len(repo.listCalls) != 0 || len(repo.stockListCalls) != 0 {
		t.Fatalf("voucher repository calls = lists %v stocks %v, want none", repo.listCalls, repo.stockListCalls)
	}
}

func TestVoucherListMissGroupsAndCaches(t *testing.T) {
	shopID := uint64(8)
	begin := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	repo := &fakeVoucherData{
		vouchers: []model.Voucher{
			{ID: 1, ShopID: shopID, Title: "normal", VoucherType: model.VoucherTypeNormal, BeginTime: begin, EndTime: begin.Add(time.Hour)},
			{ID: 2, ShopID: shopID, Title: "seckill", VoucherType: model.VoucherTypeSeckill, BeginTime: begin, EndTime: begin.Add(time.Hour)},
			{ID: 3, ShopID: shopID, Title: "promotion", VoucherType: model.VoucherTypePromo, BeginTime: begin, EndTime: begin.Add(time.Hour)},
		},
		stocks: []model.SeckillVoucher{{VoucherID: 2, Stock: 12}},
	}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{results: make(map[string]redisx.CacheResult)}

	got, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID)
	if err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	if len(got.Normal) != 1 || len(got.Seckill) != 1 || len(got.Promotion) != 1 || got.Seckill[0].Stock != 12 {
		t.Fatalf("ListByShop() grouping = %#v", got)
	}
	if !reflect.DeepEqual(repo.listCalls, []uint64{shopID}) || !reflect.DeepEqual(repo.stockListCalls, []uint64{shopID}) {
		t.Fatalf("repository calls = lists %v stocks %v, want one each for %d", repo.listCalls, repo.stockListCalls, shopID)
	}
	if len(cache.sets) != 1 {
		t.Fatalf("Set calls = %d, want 1", len(cache.sets))
	}
	if cache.sets[0].key != redisx.ShopVouchersKey(shopID) {
		t.Fatalf("Set key = %q, want %q", cache.sets[0].key, redisx.ShopVouchersKey(shopID))
	}
	if cache.sets[0].ttl < 10*time.Minute || cache.sets[0].ttl > 15*time.Minute {
		t.Fatalf("Set ttl = %s, want within [10m, 15m]", cache.sets[0].ttl)
	}
	var cached VoucherGrouped
	if err := json.Unmarshal(cache.sets[0].data, &cached); err != nil {
		t.Fatalf("Set data is not voucher JSON: %v", err)
	}
	if !reflect.DeepEqual(cached, *got) {
		t.Fatalf("cached voucher groups = %#v, want %#v", cached, *got)
	}
}

func TestVoucherListRedisFailureFallsBack(t *testing.T) {
	shopID := uint64(9)
	repo := &fakeVoucherData{}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{getErr: errors.New("redis unavailable")}

	got, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID)
	if err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	if got == nil || got.Normal == nil || got.Seckill == nil || got.Promotion == nil {
		t.Fatalf("ListByShop() = %#v, want initialized groups", got)
	}
	if !reflect.DeepEqual(repo.listCalls, []uint64{shopID}) || !reflect.DeepEqual(repo.stockListCalls, []uint64{shopID}) {
		t.Fatalf("repository calls = lists %v stocks %v, want one each for %d", repo.listCalls, repo.stockListCalls, shopID)
	}
}

func TestVoucherListCorruptCacheDeletesAndReloads(t *testing.T) {
	shopID := uint64(10)
	key := redisx.ShopVouchersKey(shopID)
	repo := &fakeVoucherData{vouchers: []model.Voucher{{ID: 1, ShopID: shopID, VoucherType: model.VoucherTypeNormal}}}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{results: map[string]redisx.CacheResult{
		key: {State: redisx.CacheHit, Data: []byte("not-json")},
	}}

	got, err := NewVoucherService(repo, lookup, cache).ListByShop(context.Background(), shopID)
	if err != nil {
		t.Fatalf("ListByShop() error = %v", err)
	}
	if len(got.Normal) != 1 {
		t.Fatalf("ListByShop() = %#v, want reloaded normal voucher", got)
	}
	if !reflect.DeepEqual(cache.deletes, []string{key}) {
		t.Fatalf("Delete calls = %v, want [%q]", cache.deletes, key)
	}
	if !reflect.DeepEqual(repo.listCalls, []uint64{shopID}) || !reflect.DeepEqual(repo.stockListCalls, []uint64{shopID}) {
		t.Fatalf("repository calls = lists %v stocks %v, want one each for %d", repo.listCalls, repo.stockListCalls, shopID)
	}
}

func TestVoucherCreateSeckillDeletesCacheAfterCommit(t *testing.T) {
	shopID := uint64(11)
	repo := &fakeVoucherData{createID: 99}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{results: make(map[string]redisx.CacheResult)}

	id, err := NewVoucherService(repo, lookup, cache).CreateSeckill(context.Background(), validSeckillInput(shopID))
	if err != nil {
		t.Fatalf("CreateSeckill() error = %v", err)
	}
	if id != 99 {
		t.Fatalf("CreateSeckill() id = %d, want 99", id)
	}
	if !reflect.DeepEqual(cache.deletes, []string{redisx.ShopVouchersKey(shopID)}) {
		t.Fatalf("Delete calls = %v, want [%q]", cache.deletes, redisx.ShopVouchersKey(shopID))
	}
}

func TestVoucherCreateSeckillDeleteFailureStillReturnsID(t *testing.T) {
	shopID := uint64(12)
	repo := &fakeVoucherData{createID: 100}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{deleteErr: errors.New("redis unavailable")}

	id, err := NewVoucherService(repo, lookup, cache).CreateSeckill(context.Background(), validSeckillInput(shopID))
	if err != nil {
		t.Fatalf("CreateSeckill() error = %v", err)
	}
	if id != 100 {
		t.Fatalf("CreateSeckill() id = %d, want 100", id)
	}
	if !reflect.DeepEqual(cache.deletes, []string{redisx.ShopVouchersKey(shopID)}) {
		t.Fatalf("Delete calls = %v, want [%q]", cache.deletes, redisx.ShopVouchersKey(shopID))
	}
}

func TestVoucherCreateSeckillDoesNotDeleteWhenDBFails(t *testing.T) {
	shopID := uint64(13)
	repo := &fakeVoucherData{createErr: errors.New("transaction failed")}
	lookup := &fakeShopLookup{shop: &model.Shop{ID: shopID}}
	cache := &fakeVoucherCache{}

	_, err := NewVoucherService(repo, lookup, cache).CreateSeckill(context.Background(), validSeckillInput(shopID))
	if err == nil {
		t.Fatal("CreateSeckill() error = nil, want error")
	}
	if len(cache.deletes) != 0 {
		t.Fatalf("Delete calls = %v, want none", cache.deletes)
	}
}

func validSeckillInput(shopID uint64) CreateSeckillInput {
	begin := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	return CreateSeckillInput{
		ShopID:    shopID,
		Title:     "flash sale",
		Price:     9900,
		Stock:     10,
		BeginTime: begin,
		EndTime:   begin.Add(time.Hour),
	}
}
