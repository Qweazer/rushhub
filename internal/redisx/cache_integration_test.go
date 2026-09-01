package redisx

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

func TestCache(t *testing.T) {
	ctx := context.Background()
	keys := []string{
		"gorush:test:value",
		"gorush:test:missing",
		"gorush:test:null",
	}
	store, client := testCacheStore(t, keys)

	missing, err := store.Get(ctx, "gorush:test:missing")
	if err != nil {
		t.Fatalf("get missing key: %v", err)
	}
	if missing.State != CacheMiss {
		t.Fatalf("missing state = %v, want CacheMiss", missing.State)
	}

	value := []byte(`{"id":1}`)
	valueTTL := time.Minute
	if err := store.Set(ctx, "gorush:test:value", value, valueTTL); err != nil {
		t.Fatalf("set value: %v", err)
	}
	assertTTL(t, ctx, client, "gorush:test:value", valueTTL)

	hit, err := store.Get(ctx, "gorush:test:value")
	if err != nil {
		t.Fatalf("get value: %v", err)
	}
	if hit.State != CacheHit {
		t.Fatalf("value state = %v, want CacheHit", hit.State)
	}
	if string(hit.Data) != string(value) {
		t.Fatalf("value data = %q, want %q", hit.Data, value)
	}

	nullTTL := 2 * time.Minute
	if err := store.SetNull(ctx, "gorush:test:null", nullTTL); err != nil {
		t.Fatalf("set null: %v", err)
	}
	assertTTL(t, ctx, client, "gorush:test:null", nullTTL)

	null, err := store.Get(ctx, "gorush:test:null")
	if err != nil {
		t.Fatalf("get null: %v", err)
	}
	if null.State != CacheNull {
		t.Fatalf("null state = %v, want CacheNull", null.State)
	}
	if null.Data != nil {
		t.Fatalf("null data = %q, want nil", null.Data)
	}

	items, err := store.MGet(ctx, keys)
	if err != nil {
		t.Fatalf("mget: %v", err)
	}
	if len(items) != len(keys) {
		t.Fatalf("mget returned %d items, want %d", len(items), len(keys))
	}
	states := []CacheState{CacheHit, CacheMiss, CacheNull}
	for i, want := range states {
		if items[i].State != want {
			t.Fatalf("mget state at index %d = %v, want %v", i, items[i].State, want)
		}
	}
	if string(items[0].Data) != string(value) {
		t.Fatalf("mget value data = %q, want %q", items[0].Data, value)
	}
	if items[2].Data != nil {
		t.Fatalf("mget null data = %q, want nil", items[2].Data)
	}

	if err := store.Delete(ctx, "gorush:test:value"); err != nil {
		t.Fatalf("delete value: %v", err)
	}
	deleted, err := store.Get(ctx, "gorush:test:value")
	if err != nil {
		t.Fatalf("get deleted value: %v", err)
	}
	if deleted.State != CacheMiss {
		t.Fatalf("deleted state = %v, want CacheMiss", deleted.State)
	}
}

func TestCacheRejectsNonPositiveTTL(t *testing.T) {
	ctx := context.Background()
	client := redis.NewClient(&redis.Options{})
	t.Cleanup(func() { _ = client.Close() })
	store := NewStore(client, time.Second)

	if err := store.Set(ctx, "gorush:test:invalid-ttl", []byte("value"), 0); err == nil {
		t.Fatal("set with zero TTL returned nil error")
	}
	if err := store.SetNull(ctx, "gorush:test:invalid-ttl", -time.Second); err == nil {
		t.Fatal("set null with negative TTL returned nil error")
	}
}

func testCacheStore(t *testing.T, keys []string) (*Store, *redis.Client) {
	t.Helper()

	addr := os.Getenv("REDIS_TEST_ADDR")
	if addr == "" {
		t.Skip("REDIS_TEST_ADDR is not set")
	}

	client := NewClient(ClientOptions{Addr: addr, DB: 15, Timeout: time.Second})
	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		client.Close()
		t.Fatalf("ping test Redis: %v", err)
	}
	if err := client.Del(ctx, keys...).Err(); err != nil {
		client.Close()
		t.Fatalf("clear test keys: %v", err)
	}
	t.Cleanup(func() {
		_ = client.Del(context.Background(), keys...).Err()
		_ = client.Close()
	})

	return NewStore(client, time.Second), client
}

func assertTTL(t *testing.T, ctx context.Context, client *redis.Client, key string, requested time.Duration) {
	t.Helper()

	ttl, err := client.TTL(ctx, key).Result()
	if err != nil {
		t.Fatalf("ttl for %q: %v", key, err)
	}
	if ttl <= 0 || ttl > requested {
		t.Fatalf("ttl for %q = %s, want positive and no greater than %s", key, ttl, requested)
	}
}
