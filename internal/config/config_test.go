package config

import (
	"testing"
	"time"
)

func TestLoadRedisDefaults(t *testing.T) {
	t.Setenv("REDIS_ADDR", "")
	t.Setenv("REDIS_PASSWORD", "")
	t.Setenv("REDIS_DB", "")
	t.Setenv("REDIS_TIMEOUT_MS", "")

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RedisAddr != "127.0.0.1:16379" {
		t.Fatalf("addr=%q", cfg.RedisAddr)
	}
	if cfg.RedisDB != 0 {
		t.Fatalf("db=%d", cfg.RedisDB)
	}
	if cfg.RedisTimeout != 200*time.Millisecond {
		t.Fatalf("timeout=%s", cfg.RedisTimeout)
	}
}

func TestLoadRejectsInvalidRedisDBAndTimeout(t *testing.T) {
	t.Setenv("REDIS_DB", "bad")
	if _, err := Load(); err == nil {
		t.Fatal("expected REDIS_DB error")
	}

	t.Setenv("REDIS_DB", "0")
	t.Setenv("REDIS_TIMEOUT_MS", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected positive timeout error")
	}
}
