package redisx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestGeoSearchReturnsAscendingNearbyShops(t *testing.T) {
	ctx := context.Background()
	store, _ := testGeoStore(t)

	shops := []GeoShop{
		{ID: 1, TypeID: 1, Longitude: 116.397128, Latitude: 39.916527},
		{ID: 2, TypeID: 1, Longitude: 116.407526, Latitude: 39.90403},
		{ID: 3, TypeID: 2, Longitude: 116.397389, Latitude: 39.908722},
	}
	if err := store.RebuildGeo(ctx, shops); err != nil {
		t.Fatalf("rebuild GEO: %v", err)
	}

	all, err := store.GeoSearch(ctx, 0, 116.397128, 39.916527, 3_000, 10)
	if err != nil {
		t.Fatalf("search all shops: %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("all shops = %#v, want three results", all)
	}
	if all[0].ShopID != 1 || all[1].ShopID != 3 || all[2].ShopID != 2 {
		t.Fatalf("all shop IDs = %#v, want [1 3 2] in ascending distance", all)
	}
	typeOne, err := store.GeoSearch(ctx, 1, 116.397128, 39.916527, 3_000, 10)
	if err != nil {
		t.Fatalf("search type 1 shops: %v", err)
	}
	if len(typeOne) != 2 || typeOne[0].ShopID != 1 || typeOne[1].ShopID != 2 {
		t.Fatalf("type 1 results = %#v, want shops 1 then 2", typeOne)
	}

	limited, err := store.GeoSearch(ctx, 0, 116.397128, 39.916527, 3_000, 1)
	if err != nil {
		t.Fatalf("search limited shops: %v", err)
	}
	if len(limited) != 1 || limited[0].ShopID != 1 {
		t.Fatalf("limited results = %#v, want only shop 1", limited)
	}

	unknown, err := store.GeoSearch(ctx, 999, 116.397128, 39.916527, 3_000, 10)
	if err != nil {
		t.Fatalf("search unknown type: %v", err)
	}
	if len(unknown) != 0 {
		t.Fatalf("unknown type results = %#v, want empty", unknown)
	}
}

func TestGeoSearchRejectsNonPositiveCount(t *testing.T) {
	store, _ := testGeoStore(t)
	ctx := context.Background()

	if _, err := store.GeoSearch(ctx, 0, 116.397128, 39.916527, 3_000, 0); err == nil {
		t.Fatal("GEO search with zero count returned nil error")
	}
	if _, err := store.GeoSearch(ctx, 0, 116.397128, 39.916527, 3_000, -1); err == nil {
		t.Fatal("GEO search with negative count returned nil error")
	}
}

func TestRebuildGeoRemovesOnlyExistingGeoKeys(t *testing.T) {
	ctx := context.Background()
	store, client := testGeoStore(t)

	if err := client.Set(ctx, "gorush:unrelated", "keep", 0).Err(); err != nil {
		t.Fatalf("seed unrelated key: %v", err)
	}
	if err := client.GeoAdd(ctx, GeoTypeKey(999), &redis.GeoLocation{
		Name:      "999",
		Longitude: 116.397128,
		Latitude:  39.916527,
	}).Err(); err != nil {
		t.Fatalf("seed obsolete GEO key: %v", err)
	}

	if err := store.RebuildGeo(ctx, []GeoShop{{
		ID:        1,
		TypeID:    1,
		Longitude: 116.397128,
		Latitude:  39.916527,
	}}); err != nil {
		t.Fatalf("rebuild GEO: %v", err)
	}

	if exists, err := client.Exists(ctx, GeoTypeKey(999)).Result(); err != nil {
		t.Fatalf("check obsolete key: %v", err)
	} else if exists != 0 {
		t.Fatal("obsolete GEO key still exists")
	}
	if value, err := client.Get(ctx, "gorush:unrelated").Result(); err != nil {
		t.Fatalf("read unrelated key: %v", err)
	} else if value != "keep" {
		t.Fatalf("unrelated key = %q, want keep", value)
	}
}

func testGeoStore(t *testing.T) (*Store, *redis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR is not set")
	}

	client := NewClient(ClientOptions{Addr: addr, DB: 15, Timeout: time.Second})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("ping test Redis: %v", err)
	}
	keys := []string{GeoAllKey(), GeoTypeKey(1), GeoTypeKey(2), GeoTypeKey(999), "gorush:unrelated"}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		_ = client.Close()
		t.Fatalf("clear test keys: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), keys...).Err()
		_ = client.Close()
	})

	return NewStore(client, time.Second), client
}
