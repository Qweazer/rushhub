package redisx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestHotRanksShopsAndSetsTTL(t *testing.T) {
	ctx := context.Background()
	store, client := testHotStore(t)
	day := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.FixedZone("CST", 8*60*60))

	for range 3 {
		if err := store.IncrementHot(ctx, 2, day); err != nil {
			t.Fatalf("increment shop 2: %v", err)
		}
	}
	if err := store.IncrementHot(ctx, 1, day); err != nil {
		t.Fatalf("increment shop 1: %v", err)
	}

	ranked, err := store.TopHot(ctx, day, 10)
	if err != nil {
		t.Fatalf("get top hot shops: %v", err)
	}
	if len(ranked) != 2 {
		t.Fatalf("ranked shops = %#v, want two results", ranked)
	}
	if ranked[0] != (RankedShop{ShopID: 2, Views: 3}) {
		t.Fatalf("first ranked shop = %#v, want shop 2 with 3 views", ranked[0])
	}
	if ranked[1] != (RankedShop{ShopID: 1, Views: 1}) {
		t.Fatalf("second ranked shop = %#v, want shop 1 with 1 view", ranked[1])
	}

	ttl, err := client.TTL(ctx, HotKey(day)).Result()
	if err != nil {
		t.Fatalf("get hot key TTL: %v", err)
	}
	if ttl <= 0 || ttl > 72*time.Hour {
		t.Fatalf("hot key TTL = %s, want positive and at most 72h", ttl)
	}

	missingDay := day.AddDate(0, 0, 1)
	missing, err := store.TopHot(ctx, missingDay, 10)
	if err != nil {
		t.Fatalf("get missing day's hot shops: %v", err)
	}
	if len(missing) != 0 {
		t.Fatalf("missing day results = %#v, want empty", missing)
	}
}

func TestTopHotRejectsNonPositiveLimit(t *testing.T) {
	store, _ := testHotStore(t)
	day := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.Local)

	if _, err := store.TopHot(context.Background(), day, 0); err == nil {
		t.Fatal("top hot with zero limit returned nil error")
	}
	if _, err := store.TopHot(context.Background(), day, -1); err == nil {
		t.Fatal("top hot with negative limit returned nil error")
	}
}

func testHotStore(t *testing.T) (*Store, *redis.Client) {
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
	keys := []string{
		HotKey(time.Date(2026, time.August, 31, 0, 0, 0, 0, time.Local)),
		HotKey(time.Date(2026, time.September, 1, 0, 0, 0, 0, time.Local)),
	}
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
