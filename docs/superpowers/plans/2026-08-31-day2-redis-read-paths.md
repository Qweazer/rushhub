# GoRush Day 2 Redis Read Paths Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make shop detail, nearby shops, daily hot ranking, and voucher reads use Redis through Cache Aside, GEO, and ZSet while preserving MySQL fallbacks where the design permits them.

**Architecture:** Keep the existing handler → service → repository → MySQL flow and add a focused `internal/redisx` adapter beside it. Services own cache policy and JSON DTO encoding; `redisx` owns Redis commands and key formats. Nearby and hot results return ordered shop IDs from Redis, then reuse one batch detail loader to hydrate shop data without N+1 queries.

**Tech Stack:** Go 1.26.1, Gin 1.12, GORM 1.31, MySQL 8, Redis 7.4, `github.com/redis/go-redis/v9` 9.20.x, `golang.org/x/sync/singleflight`, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-08-31-day2-redis-read-paths-design.md`

## Global Constraints

- Preserve all Day 1 routes and response envelopes.
- Use only keys under the `gorush:` namespace; never call `FLUSHDB` or `FLUSHALL`.
- Shop and voucher cache failures fall back to MySQL; GEO and hot-ranking failures return HTTP 503 with business code `50301`.
- Only direct `GET /api/v1/shops/:id` requests increment the daily hot score.
- Normal shop cache TTL is 30 minutes plus bounded jitter; null TTL is 2 minutes; voucher TTL is 10 minutes plus bounded jitter; hot keys expire after 72 hours.
- GEO supports optional category filtering, radius up to 50,000 meters, page up to 100, and size up to 50.
- Follow red-green-refactor for every production Go behavior: run the named failing test before adding implementation.
- The user explicitly requested no Git commits. Replace every commit checkpoint with `git diff --check`, targeted tests, and a short diff review.

## File Map

- `internal/redisx/client.go`: Redis client construction, operation timeout, Ping, and Close.
- `internal/redisx/keys.go`: every `gorush:` key constructor.
- `internal/redisx/cache.go`: byte cache hit/miss/null states, MGET, SET, null SET, and DELETE.
- `internal/redisx/geo.go`: GEOSEARCH and namespaced GEO rebuild.
- `internal/redisx/hot.go`: daily ZINCRBY/EXPIRE and ZREVRANGE WITHSCORES.
- `internal/repository/shop.go`: ordered batch lookup and indexable online shop coordinates.
- `internal/service/shop.go`: detail Cache Aside, batch hydration, nearby flow, hot flow, and singleflight.
- `internal/service/voucher.go`: voucher Cache Aside and post-create invalidation.
- `internal/handler/shop.go`: nearby/hot query parsing and HTTP responses.
- `internal/handler/health.go`: combined MySQL and Redis health.
- `internal/router/router.go`, `cmd/server/main.go`: dependency construction and routes.
- `cmd/reindex/main.go`: explicit GEO rebuild CLI.
- `docker-compose.yml`, `.env.example`, `Makefile`, `README.md`: Redis runtime and operator workflow.

---

### Task 1: Redis configuration, keys, and client lifecycle

**Files:**
- Create: `internal/redisx/client.go`
- Create: `internal/redisx/keys.go`
- Create: `internal/redisx/keys_test.go`
- Modify: `internal/config/config.go`
- Create: `internal/config/config_test.go`
- Modify: `go.mod`
- Modify: `go.sum`
- Modify: `docker-compose.yml`
- Modify: `.env.example`

**Interfaces:**
- Produces `redisx.ClientOptions{Addr string, Password string, DB int, Timeout time.Duration}`.
- Produces `redisx.NewClient(ClientOptions) *redis.Client`, `redisx.NewStore(*redis.Client, time.Duration) *Store`, `(*Store).Ping(context.Context) error`, and `(*Store).Close() error`.
- Produces key helpers `ShopDetailKey(uint64)`, `ShopVouchersKey(uint64)`, `GeoAllKey()`, `GeoTypeKey(uint64)`, and `HotKey(time.Time)`.

- [ ] **Step 1: Add failing config tests**

```go
func TestLoadRedisDefaults(t *testing.T) {
    t.Setenv("REDIS_ADDR", "")
    t.Setenv("REDIS_PASSWORD", "")
    t.Setenv("REDIS_DB", "")
    t.Setenv("REDIS_TIMEOUT_MS", "")
    cfg, err := Load()
    if err != nil { t.Fatal(err) }
    if cfg.RedisAddr != "127.0.0.1:16379" { t.Fatalf("addr=%q", cfg.RedisAddr) }
    if cfg.RedisDB != 0 { t.Fatalf("db=%d", cfg.RedisDB) }
    if cfg.RedisTimeout != 200*time.Millisecond { t.Fatalf("timeout=%s", cfg.RedisTimeout) }
}

func TestLoadRejectsInvalidRedisDBAndTimeout(t *testing.T) {
    t.Setenv("REDIS_DB", "bad")
    if _, err := Load(); err == nil { t.Fatal("expected REDIS_DB error") }
    t.Setenv("REDIS_DB", "0")
    t.Setenv("REDIS_TIMEOUT_MS", "0")
    if _, err := Load(); err == nil { t.Fatal("expected positive timeout error") }
}
```

Run: `go test ./internal/config -run 'TestLoadRedis' -count=1`

Expected: FAIL because the Redis fields do not exist.

- [ ] **Step 2: Add minimal Redis fields and parsing**

Add these fields to `config.Config` and populate them in `Load`:

```go
RedisAddr     string
RedisPassword string
RedisDB       int
RedisTimeout  time.Duration
```

Parse `REDIS_DB` with default `0`, parse `REDIS_TIMEOUT_MS` with default `200`, and reject non-positive timeout values with `fmt.Errorf("env REDIS_TIMEOUT_MS must be > 0")`.

Run: `go test ./internal/config -run 'TestLoadRedis' -count=1`

Expected: PASS.

- [ ] **Step 3: Add failing key tests**

```go
func TestKeysAreNamespaced(t *testing.T) {
    day := time.Date(2026, 8, 31, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
    cases := map[string]string{
        ShopDetailKey(7):  "gorush:shop:detail:7",
        ShopVouchersKey(7): "gorush:shop:vouchers:7",
        GeoAllKey(): "gorush:geo:shops:all",
        GeoTypeKey(3): "gorush:geo:shops:type:3",
        HotKey(day): "gorush:shop:hot:20260831",
    }
    for got, want := range cases {
        if got != want { t.Fatalf("got %q want %q", got, want) }
    }
}
```

Run: `go test ./internal/redisx -run TestKeysAreNamespaced -count=1`

Expected: FAIL because `internal/redisx` and its key helpers do not exist.

- [ ] **Step 4: Add go-redis and implement client/key helpers**

Run:

```bash
go get github.com/redis/go-redis/v9@v9.20.0
go get golang.org/x/sync/singleflight
```

Implement `client.go` with `redis.NewClient(&redis.Options{Addr, Password, DB, DialTimeout: Timeout, ReadTimeout: Timeout, WriteTimeout: Timeout})`. `NewStore` stores the client and operation timeout; `Ping` uses `context.WithTimeout`; `Close` delegates to the client. Implement all key helpers exactly as asserted above.

Run: `go test ./internal/config ./internal/redisx -count=1`

Expected: PASS.

- [ ] **Step 5: Add Redis 7.4 to local runtime**

Add a `redis` service using `redis:7.4-alpine`, container name `gorush-redis`, port `16379:6379`, `redis-server --appendonly yes`, a `redis-cli ping` healthcheck, and a named `redis_data` volume. Add the four Redis variables to `.env.example`.

Run: `docker compose config`

Expected: exit 0 and rendered services include both `mysql` and `redis`.

- [ ] **Step 6: Checkpoint**

Run: `gofmt -w internal/config internal/redisx && go test ./internal/config ./internal/redisx -count=1 && git diff --check`

Expected: tests PASS and diff check produces no output.

---

### Task 2: Generic byte Cache Aside primitives

**Files:**
- Create: `internal/redisx/cache.go`
- Create: `internal/redisx/cache_integration_test.go`

**Interfaces:**
- Produces `type CacheState uint8` with `CacheMiss`, `CacheHit`, and `CacheNull`.
- Produces `type CacheResult struct { State CacheState; Data []byte }`.
- Produces `Get(ctx context.Context, key string) (CacheResult, error)`, `MGet(ctx context.Context, keys []string) ([]CacheResult, error)`, `Set(ctx context.Context, key string, data []byte, ttl time.Duration) error`, `SetNull(ctx context.Context, key string, ttl time.Duration) error`, and `Delete(ctx context.Context, key string) error` on `*redisx.Store`.
- Uses the exact null sentinel `__gorush_null__` internally; callers see only `CacheNull`.

- [ ] **Step 1: Start Redis and write failing integration tests**

Run: `docker compose up -d redis`

Create a test helper that reads `REDIS_TEST_ADDR`; if unset it calls `t.Skip`, otherwise creates a dedicated client with DB `15` and deletes only keys used by that test via `t.Cleanup`.

Cover these assertions:

```go
got, err := store.Get(ctx, "gorush:test:missing")
// err == nil, got.State == CacheMiss

err = store.Set(ctx, "gorush:test:value", []byte(`{"id":1}`), time.Minute)
// next Get is CacheHit and bytes are identical

err = store.SetNull(ctx, "gorush:test:null", 2*time.Minute)
// next Get is CacheNull with nil Data

items, err := store.MGet(ctx, []string{"gorush:test:value", "gorush:test:missing", "gorush:test:null"})
// states remain [CacheHit, CacheMiss, CacheNull] in input order

err = store.Delete(ctx, "gorush:test:value")
// next Get is CacheMiss
```

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run TestCache -count=1`

Expected: FAIL because cache methods do not exist.

- [ ] **Step 2: Implement cache state semantics**

Implement `Get` so `redis.Nil` maps to `CacheMiss`, the sentinel maps to `CacheNull`, and other Redis errors are returned. Implement `MGet` with one command and preserve input ordering. Reject a non-positive TTL in `Set` and `SetNull` with an ordinary Go error. Copy returned byte slices so callers cannot mutate shared command buffers.

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run TestCache -count=1`

Expected: PASS.

- [ ] **Step 3: Verify TTL behavior**

Extend the integration test to assert `TTL` is positive and no greater than the requested TTL after `Set`/`SetNull`. Do not use sleeps; inspect Redis TTL directly through the test client.

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run TestCache -count=1`

Expected: PASS.

- [ ] **Step 4: Checkpoint**

Run: `gofmt -w internal/redisx && REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -count=1 && git diff --check`

Expected: tests PASS and diff check is clean.

---

### Task 3: Batch shop repository reads and GEO source rows

**Files:**
- Modify: `internal/repository/shop.go`
- Create: `internal/repository/shop_integration_test.go`

**Interfaces:**
- Produces `GetByIDs(ctx context.Context, ids []uint64) ([]model.Shop, error)`.
- Produces `type ShopLocation struct { ID uint64; TypeID uint64; Longitude float64; Latitude float64 }`.
- Produces `ListOnlineLocations(ctx context.Context) ([]ShopLocation, error)`.

- [ ] **Step 1: Write failing MySQL integration tests**

Use `GORUSH_TEST_DSN`; skip only when it is absent. Open GORM with the MySQL driver and use a transaction rolled back by `t.Cleanup`. Insert three uniquely named shops into the transaction.

Assertions:

```go
rows, err := repo.GetByIDs(ctx, []uint64{id3, id1, 999999})
// err == nil; rows contain id1 and id3, each exactly once.

locations, err := repo.ListOnlineLocations(ctx)
// includes online rows with ID, TypeID, Longitude, Latitude;
// excludes a row inserted with status = 0.
```

Run: `GORUSH_TEST_DSN='gorush:gorushpass@tcp(127.0.0.1:13306)/gorush?charset=utf8mb4&parseTime=True&loc=Local' go test ./internal/repository -run 'TestShopRepository_(GetByIDs|ListOnlineLocations)' -count=1`

Expected: FAIL because both repository methods are missing.

- [ ] **Step 2: Implement one-query repository methods**

`GetByIDs` returns an empty slice without querying when `ids` is empty, otherwise uses `WHERE id IN ?`. `ListOnlineLocations` selects only the four fields and filters `status = model.ShopStatusOnline`, ordered by ID for deterministic reindex output.

Run the targeted command from Step 1.

Expected: PASS.

- [ ] **Step 3: Checkpoint**

Run: `gofmt -w internal/repository && GORUSH_TEST_DSN='gorush:gorushpass@tcp(127.0.0.1:13306)/gorush?charset=utf8mb4&parseTime=True&loc=Local' go test ./internal/repository -count=1 && git diff --check`

Expected: repository tests PASS and diff check is clean.

---

### Task 4: Shop detail Cache Aside and hot-view tracking

**Files:**
- Modify: `internal/service/shop.go`
- Create: `internal/service/shop_test.go`
- Modify: `internal/router/router.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Introduces narrow `shopData` and `shopRedis` interfaces in the service package.
- `shopData` contains `List(context.Context, repository.ListFilter) ([]model.Shop, int64, error)`, `GetByID(context.Context, uint64) (*model.Shop, error)`, and `GetByIDs(context.Context, []uint64) ([]model.Shop, error)`.
- `shopRedis` contains the cache signatures from Task 2 plus `GeoSearch(context.Context, uint64, float64, float64, float64, int) ([]redisx.GeoResult, error)`, `IncrementHot(context.Context, uint64, time.Time) error`, and `TopHot(context.Context, time.Time, int) ([]redisx.RankedShop, error)` supplied by `redisx.Store`.
- Produces `LookupByID(ctx, id)` for untracked internal reads and preserves `GetByID(ctx, id)` for tracked HTTP detail reads.

- [ ] **Step 1: Write failing cache hit/miss/null tests with fakes**

Create small hand-written fakes that record DB calls and Redis operations. Cover one behavior per test:

```go
func TestShopLookupCacheHitSkipsMySQL(t *testing.T) { /* cached JSON -> DB calls 0 */ }
func TestShopLookupMissLoadsMySQLAndCaches(t *testing.T) { /* miss -> DB calls 1, Set key */ }
func TestShopLookupMissingWritesNull(t *testing.T) { /* repo ErrShopNotFound -> SetNull, 404 */ }
func TestShopLookupNullHitSkipsMySQL(t *testing.T) { /* CacheNull -> DB calls 0, 404 */ }
func TestShopLookupRedisErrorFallsBackToMySQL(t *testing.T) { /* Get error -> valid shop */ }
func TestShopLookupCorruptJSONDeletesAndReloads(t *testing.T) { /* invalid JSON -> Delete + DB */ }
```

Run: `go test ./internal/service -run 'TestShopLookup' -count=1`

Expected: FAIL because the service has no cache dependency or `LookupByID`.

- [ ] **Step 2: Implement LookupByID with TTL jitter and singleflight**

Change the constructor to `NewShopService(repo shopData, cache shopRedis)`. `LookupByID` follows the spec’s hit/miss/null/error states. Use `singleflight.Group.Do(strconv.FormatUint(id, 10), fn)` only around the second cache check plus MySQL load. Use constants `shopCacheTTL = 30*time.Minute`, `shopNullTTL = 2*time.Minute`, and a helper that adds 0–300 seconds of jitter to normal TTL.

Add `TestJitterTTLStaysWithinBound` that calls the helper repeatedly and asserts every value is `>= 30*time.Minute` and `<= 35*time.Minute`.

Run the targeted tests from Step 1.

Expected: PASS.

- [ ] **Step 3: Write and pass tracking tests**

Add tests proving:

```go
func TestShopGetByIDIncrementsHotAfterSuccess(t *testing.T) { /* IncrementHot calls 1 */ }
func TestShopGetByIDDoesNotIncrementOnNotFound(t *testing.T) { /* calls 0 */ }
func TestShopGetByIDIgnoresHotIncrementFailure(t *testing.T) { /* shop still returned */ }
func TestShopLookupByIDDoesNotIncrementHot(t *testing.T) { /* calls 0 */ }
```

Run before implementation and confirm failure because `GetByID` does not call `IncrementHot`. Then make `GetByID` call `LookupByID` followed by best-effort `IncrementHot(ctx, id, time.Now())`; log failures through `httpx.FromContext`.

Run: `go test ./internal/service -run 'TestShop(GetByID|LookupByID)' -count=1`

Expected: PASS.

- [ ] **Step 4: Add ordered batch hydration tests and implementation**

Test an unexported `loadMany(ctx, ids)` helper with IDs `[3, 1, 2]`: ID 3 is a cache hit, IDs 1 and 2 miss, `GetByIDs` is called exactly once with `[1, 2]`, and the returned map contains all three. Test null entries are omitted and Redis errors still trigger one batch DB query.

Implement one MGET, one `GetByIDs` for all misses, and best-effort cache writes through repeated `Set`; callers restore ordering from the original ID slice.

Run: `go test ./internal/service -run 'TestShopLoadMany' -count=1`

Expected: PASS.

- [ ] **Step 5: Wire the shared Redis store without a startup Ping**

Change `router.New` to `router.New(db *gorm.DB, redisStore *redisx.Store)` and pass the store to `NewShopService`. In `cmd/server/main.go`, construct `redisx.NewClient` from the four Redis config fields, defer client close, construct one shared Store, and pass it to `router.New`. Do not Ping during startup; a stopped Redis must not prevent the MySQL fallback endpoints from starting.

Run: `go test ./internal/service ./internal/router ./cmd/server -count=1`

Expected: PASS.

- [ ] **Step 6: Checkpoint**

Run: `gofmt -w internal/service internal/router cmd/server && go test ./internal/service ./internal/router ./cmd/server -count=1 && git diff --check`

Expected: service tests PASS and diff check is clean.

---

### Task 5: Voucher Cache Aside and invalidation

**Files:**
- Modify: `internal/service/voucher.go`
- Create: `internal/service/voucher_test.go`
- Modify: `internal/router/router.go`

**Interfaces:**
- Introduces `voucherData` with the existing `ListByShop`, `ListSeckillStocksByShop`, and `CreateSeckill` signatures; `voucherCache` with `Get`, `Set`, and `Delete` signatures from Task 2; and `shopLookup`.
- `shopLookup` requires `LookupByID(context.Context, uint64) (*model.Shop, error)` so voucher reads do not add hot views.
- Changes constructor to `NewVoucherService(voucherData, shopLookup, voucherCache)`.

- [ ] **Step 1: Write failing voucher cache tests**

Cover:

```go
func TestVoucherListCacheHitSkipsRepository(t *testing.T) {}
func TestVoucherListMissGroupsAndCaches(t *testing.T) {}
func TestVoucherListRedisFailureFallsBack(t *testing.T) {}
func TestVoucherListCorruptCacheDeletesAndReloads(t *testing.T) {}
func TestVoucherCreateSeckillDeletesCacheAfterCommit(t *testing.T) {}
func TestVoucherCreateSeckillDeleteFailureStillReturnsID(t *testing.T) {}
func TestVoucherCreateSeckillDoesNotDeleteWhenDBFails(t *testing.T) {}
```

Use cached JSON built with `json.Marshal(VoucherGrouped{...})`; record repository call counts and deleted key names.

Run: `go test ./internal/service -run TestVoucher -count=1`

Expected: FAIL because voucher service has no cache or shop-lookup interfaces.

- [ ] **Step 2: Implement voucher caching**

Use `ShopVouchersKey(shopID)`, `voucherCacheTTL = 10*time.Minute`, and the same bounded TTL jitter policy as shops. Cache hit returns the decoded grouped DTO. Miss calls the two repository queries and writes JSON. Redis read/write failure logs and preserves MySQL behavior. Corrupt JSON triggers best-effort delete and reload.

Run: `go test ./internal/service -run TestVoucher -count=1`

Expected: PASS.

- [ ] **Step 3: Wire cached shop lookup and voucher store**

In `router.New`, pass `shopSvc` as `shopLookup` instead of `shopRepo`, and pass the shared `*redisx.Store` as voucher cache. Do not add or reorder routes yet.

Run: `go test ./internal/service ./internal/router -count=1`

Expected: PASS.

- [ ] **Step 4: Checkpoint**

Run: `gofmt -w internal/service internal/router && go test ./internal/service ./internal/router -count=1 && git diff --check`

Expected: tests PASS and diff check is clean.

---

### Task 6: Redis GEO and daily hot ZSet commands

**Files:**
- Create: `internal/redisx/geo.go`
- Create: `internal/redisx/hot.go`
- Create: `internal/redisx/geo_integration_test.go`
- Create: `internal/redisx/hot_integration_test.go`

**Interfaces:**
- Produces `GeoShop{ID, TypeID uint64; Longitude, Latitude float64}`.
- Produces `GeoResult{ShopID uint64; DistanceMeters float64}`.
- Produces `GeoSearch(ctx context.Context, typeID uint64, lng, lat, radiusMeters float64, count int) ([]GeoResult, error)`.
- Produces `RebuildGeo(ctx context.Context, shops []GeoShop) error`.
- Produces `RankedShop{ShopID uint64; Views int64}`.
- Produces `IncrementHot(ctx context.Context, shopID uint64, day time.Time) error` and `TopHot(ctx context.Context, day time.Time, limit int) ([]RankedShop, error)`.

- [ ] **Step 1: Write failing GEO integration tests**

Against `REDIS_TEST_ADDR`, add three known Beijing coordinates: two in type 1 and one in type 2. Assert all-key search is ascending by distance, type 1 excludes type 2, `count` limits results, and unknown type returns an empty slice.

Also seed `gorush:unrelated` and an obsolete `gorush:geo:shops:type:999`; call `RebuildGeo`; assert the obsolete GEO key is gone and unrelated key remains.

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run 'TestGeo' -count=1`

Expected: FAIL because GEO methods do not exist.

- [ ] **Step 2: Implement GEO commands**

Use `GeoAdd` for the all key and each type key. Use `GeoSearchLocation` with `Longitude`, `Latitude`, `Radius`, `RadiusUnit: "m"`, `Sort: "ASC"`, `Count`, and `WithDist: true`. Parse member strings with `strconv.ParseUint`; malformed members return an error rather than disappearing silently.

For rebuild, SCAN only `gorush:geo:shops:type:*`, delete those keys plus `GeoAllKey`, then pipeline `GeoAdd` commands grouped by destination key. Empty input leaves no GoRush GEO keys.

Run the targeted GEO tests.

Expected: PASS.

- [ ] **Step 3: Write failing hot-ranking integration tests**

Use a fixed local-zone day. Increment shop 2 three times and shop 1 once. Assert `TopHot(..., 10)` returns shop 2 with 3 views before shop 1 with 1 view. Assert key TTL is positive and at most 72 hours. Assert a missing day returns an empty slice.

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run 'TestHot' -count=1`

Expected: FAIL because hot methods do not exist.

- [ ] **Step 4: Implement ZSet commands**

Use `TxPipeline` with `ZIncrBy` followed by `Expire(72*time.Hour)` so each successful increment refreshes TTL. Use `ZRevRangeWithScores(0, int64(limit-1))` and convert integral scores to `int64`; reject `limit <= 0`. Parse members as unsigned IDs and return parse errors.

Run: `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -run 'Test(Geo|Hot)' -count=1`

Expected: PASS.

- [ ] **Step 5: Checkpoint**

Run: `gofmt -w internal/redisx && REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -count=1 && git diff --check`

Expected: all Redis integration tests PASS and diff check is clean.

---

### Task 7: Nearby and hot service APIs

**Files:**
- Modify: `internal/httpx/response.go`
- Modify: `internal/service/shop.go`
- Modify: `internal/service/shop_test.go`

**Interfaces:**
- Adds `httpx.CodeRedisUnavailable = 50301` and `httpx.NewRedisUnavailable(error)`.
- Produces `NearbyQuery`, `NearbyItem`, `NearbyResult`, `HotItem`, and `HotResult` JSON DTOs.
- Produces `ShopService.Nearby(ctx, NearbyQuery)` and `ShopService.Hot(ctx, limit)`.

Use these DTO shapes:

```go
type NearbyQuery struct { Longitude, Latitude, RadiusMeters float64; TypeID uint64; Page, Size int }
type NearbyItem struct { Shop model.Shop `json:"shop"`; DistanceMeters float64 `json:"distance_m"` }
type NearbyResult struct { Items []NearbyItem `json:"items"`; Page int `json:"page"`; Size int `json:"size"` }
type HotItem struct { Shop model.Shop `json:"shop"`; Views int64 `json:"views"` }
type HotResult struct { Items []HotItem `json:"items"` }
```

- [ ] **Step 1: Write failing error mapping test**

Create `internal/httpx/response_test.go` with a Gin recorder. Call `Fail` with `NewRedisUnavailable(errors.New("down"))`; assert HTTP 503, JSON code `50301`, and message `redis unavailable` without the internal error string.

Run: `go test ./internal/httpx -run TestFailRedisUnavailable -count=1`

Expected: FAIL because the code and 503 mapping do not exist.

- [ ] **Step 2: Implement Redis error mapping**

Add the constant/constructor and add `case 503: httpStatus = http.StatusServiceUnavailable` to `Fail`.

Run: `go test ./internal/httpx -run TestFailRedisUnavailable -count=1`

Expected: PASS.

- [ ] **Step 3: Write failing nearby service tests**

Test validation and orchestration separately:

```go
// Invalid longitude, latitude, radius, page, or size -> CodeBadRequest.
// Redis GeoSearch error -> CodeRedisUnavailable.
// page 2 size 2 asks Redis for count 4, slices results [2:4].
// loadMany hydrates shops and output preserves GEO order and distance.
// missing hydrated shop is omitted without shifting another distance onto it.
```

Run: `go test ./internal/service -run TestShopNearby -count=1`

Expected: FAIL because `Nearby` and DTOs do not exist.

- [ ] **Step 4: Implement Nearby**

Define defaults and maxima as named constants. Select all/type GEO key through the `typeID` argument accepted by `redisx.Store`. Request `page*size`, slice from `(page-1)*size`, batch hydrate only current-page IDs, and pair each retained shop with its own `GeoResult.DistanceMeters`.

Run the targeted nearby tests.

Expected: PASS.

- [ ] **Step 5: Write failing hot service tests and implement**

Test default/maximum limit normalization, Redis error → 50301, preserved ZSet order, exact view counts, and omission of stale shop IDs. Then implement `Hot` using `TopHot(ctx, time.Now(), limit)` plus `loadMany`.

Run: `go test ./internal/service -run TestShopHot -count=1`

Expected: PASS after implementation.

- [ ] **Step 6: Checkpoint**

Run: `gofmt -w internal/httpx internal/service && go test ./internal/httpx ./internal/service -count=1 && git diff --check`

Expected: tests PASS and diff check is clean.

---

### Task 8: HTTP handlers, routes, server assembly, and combined health

**Files:**
- Modify: `internal/handler/shop.go`
- Create: `internal/handler/shop_test.go`
- Modify: `internal/handler/health.go`
- Create: `internal/handler/health_test.go`
- Modify: `internal/router/router.go`
- Modify: `cmd/server/main.go`

**Interfaces:**
- Changes `handler.ShopHandler` to depend on a local interface with `ListPage(context.Context, uint64, int, int) (*service.ListResult, error)`, `GetByID(context.Context, uint64) (*model.Shop, error)`, `Nearby(context.Context, service.NearbyQuery) (*service.NearbyResult, error)`, and `Hot(context.Context, int) (*service.HotResult, error)`.
- Changes `NewHealthHandler` to accept MySQL plus a `Ping(context.Context) error` Redis dependency.
- Uses the `router.New(db *gorm.DB, redisStore *redisx.Store)` signature introduced in Task 4.

- [ ] **Step 1: Write failing handler tests**

Use a fake shop service and `httptest` to cover:

```go
GET /api/v1/shops/nearby?lng=116.48&lat=39.99
// passes parsed defaults radius=5000, page=1, size=10 and returns 200 envelope.

GET /api/v1/shops/nearby?lng=bad&lat=39.99
// returns 40000 without calling service.

GET /api/v1/shops/hot?limit=5
// passes 5 and returns 200 envelope.
```

Run: `go test ./internal/handler -run 'TestShopHandler_(Nearby|Hot)' -count=1`

Expected: FAIL because handlers and interface do not exist.

- [ ] **Step 2: Implement handlers**

Parse every supplied numeric query strictly: invalid text is a 400 rather than silently becoming zero. Let service range validation handle parsed values. Return results through `httpx.OK` and errors through `httpx.Fail`.

Run the targeted handler tests.

Expected: PASS.

- [ ] **Step 3: Write failing combined health tests**

Run `go get github.com/DATA-DOG/go-sqlmock@v1.5.2`, then use a GORM MySQL connection backed by `sqlmock` for `SELECT 1` and a fake Redis pinger. Build GORM with `mysql.Config{Conn: sqlDB, SkipInitializeWithVersion: true}` so setup performs no unplanned version query. Cover both healthy → 200 with `checks.db=ok`, `checks.redis=ok`, `status=ok`, and Redis failure → 503 with `checks.redis` beginning `down:`, DB remaining `ok`, and `status=down`.

Run: `go test ./internal/handler -run TestHealth -count=1`

Expected: FAIL because health accepts only DB and has no Redis check.

- [ ] **Step 4: Implement combined health**

Run DB and Redis checks under the existing two-second request context. Keep process-level Redis startup non-fatal, but return 503 when either check fails. Preserve the existing response shape and add only `checks.redis`.

Run: `go test ./internal/handler -run TestHealth -count=1`

Expected: PASS.

- [ ] **Step 5: Wire routes and health dependency**

Register `/shops/nearby` and `/shops/hot` before `/shops/:id`. Pass the existing shared Redis Store to `NewHealthHandler`; client construction and non-fatal startup behavior remain in `cmd/server/main.go` from Task 4.

Run: `go test ./internal/handler ./internal/router ./cmd/server -count=1`

Expected: PASS.

- [ ] **Step 6: Checkpoint**

Run: `gofmt -w internal/handler internal/router cmd/server && go test ./internal/handler ./internal/router ./cmd/server -count=1 && git diff --check`

Expected: tests PASS and diff check is clean.

---

### Task 9: GEO reindex CLI and developer commands

**Files:**
- Create: `cmd/reindex/main.go`
- Create: `cmd/reindex/main_test.go`
- Modify: `Makefile`

**Interfaces:**
- Produces `run(ctx context.Context, repo locationRepository, geo geoRebuilder) error` in `cmd/reindex` for testability.
- `locationRepository.ListOnlineLocations` returns repository rows; `geoRebuilder.RebuildGeo` consumes converted `[]redisx.GeoShop`.

- [ ] **Step 1: Write failing reindex orchestration tests**

Use fakes to assert repository errors are returned, Redis rebuild errors are returned, and successful rows are converted without changing ID/type/coordinates. Include an empty input success case that still invokes `RebuildGeo` so stale GEO keys are removed.

Run: `go test ./cmd/reindex -count=1`

Expected: FAIL because the command does not exist.

- [ ] **Step 2: Implement reindex command**

`main` loads config, opens MySQL, creates Redis client/store, calls `run`, and exits through `log.Fatalf` on error. `run` performs exactly one repository read and one `RebuildGeo` call.

Run: `go test ./cmd/reindex -count=1`

Expected: PASS.

- [ ] **Step 3: Add Makefile workflow**

Add `reindex` to `.PHONY`, implement `reindex: go run ./cmd/reindex`, and change reset’s final line from `$(MAKE) migrate seed` to `$(MAKE) migrate seed reindex`. Update the waiting logic to require both MySQL and Redis health before migration/reindex.

Run: `make -n reindex` and `docker compose config`

Expected: dry run prints `go run ./cmd/reindex`; compose config exits 0.

- [ ] **Step 4: Run real reindex integration**

Run:

```bash
docker compose up -d mysql redis
make migrate
make seed
make reindex
docker compose exec -T redis redis-cli ZCARD gorush:geo:shops:all
```

Expected: reindex exits 0 and ZCARD prints `4` for the current seed data.

- [ ] **Step 5: Checkpoint**

Run: `gofmt -w cmd/reindex && go test ./cmd/reindex -count=1 && git diff --check`

Expected: tests PASS and diff check is clean.

---

### Task 10: Documentation, full verification, and Redis failure drill

**Files:**
- Modify: `README.md`
- Modify: `.env.example` if verification exposes missing operator guidance
- Modify: `Makefile` if verification exposes a missing deterministic command

**Interfaces:**
- Documents the two new APIs, Redis keys, reindex workflow, degradation matrix, and exact curl commands.
- Adds `test-redis` and `verify` Make targets with deterministic command lists.

- [ ] **Step 1: Add operator documentation**

Update README Day 2 status, directory tree, startup order, API tables, curl examples, Redis key table, and limitations. State explicitly that Redis is mandatory for nearby/hot but optional through fallback for detail/vouchers.

- [ ] **Step 2: Add deterministic verification targets**

`test-redis` runs `REDIS_TEST_ADDR=127.0.0.1:16379 go test ./internal/redisx -count=1`. `verify` runs `go test ./...`, `go vet ./...`, and builds server, migrate, seed, and reindex into a temporary directory created with `mktemp -d`; it removes only that explicit temporary directory on exit.

Run: `make test-redis && make verify`

Expected: all commands exit 0.

- [ ] **Step 3: Run HTTP acceptance checks with live MySQL and Redis**

Start the server in a PTY after migrate/seed/reindex. Execute:

```bash
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/1'
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/1'
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/nearby?lng=116.48&lat=39.99&radius=50000&page=1&size=10'
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/hot?limit=10'
curl -fsS 'http://127.0.0.1:18080/api/v1/shops/4/vouchers'
```

Inspect Redis with `GET gorush:shop:detail:1`, `ZSCORE gorush:shop:hot:$(date +%Y%m%d) 1`, `GEOPOS gorush:geo:shops:all 1`, and `GET gorush:shop:vouchers:4`. Expected: both cache keys exist, hot score for shop 1 is `2`, GEO position exists, nearby is distance ordered, and hot output starts with shop 1 unless another shop has more test views.

- [ ] **Step 4: Verify voucher invalidation**

Populate shop 4’s voucher cache, create a new seckill voucher for shop 4 with a currently active 24-hour window, assert `EXISTS gorush:shop:vouchers:4` becomes `0`, then query vouchers again and assert the new voucher ID is present.

- [ ] **Step 5: Run Redis failure drill**

Stop only Redis with `docker compose stop redis`. Assert shop detail and voucher endpoints still return HTTP 200 from MySQL, while nearby and hot return HTTP 503 with JSON code `50301`; `/health` returns 503 with `checks.db=ok` and `checks.redis` down. Restart Redis and run `make reindex` before the final verification.

- [ ] **Step 6: Fresh final verification**

Run:

```bash
make test-redis
make verify
git diff --check
git status --short
```

Expected: Redis tests, full tests, vet, and all four builds exit 0; diff check is silent; status shows only the intended Day 2 source, test, configuration, and documentation changes plus pre-existing `.understand-anything/` analysis artifacts.
