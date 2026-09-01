package redisx

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const nullSentinel = "__gorush_null__"

type CacheState uint8

const (
	CacheMiss CacheState = iota
	CacheHit
	CacheNull
)

type CacheResult struct {
	State CacheState
	Data  []byte
}

func (s *Store) Get(ctx context.Context, key string) (CacheResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	data, err := s.client.Get(ctx, key).Bytes()
	if err == redis.Nil {
		return CacheResult{State: CacheMiss}, nil
	}
	if err != nil {
		return CacheResult{}, err
	}
	return cacheResult(data), nil
}

func (s *Store) MGet(ctx context.Context, keys []string) ([]CacheResult, error) {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	values, err := s.client.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, err
	}

	results := make([]CacheResult, len(values))
	for i, value := range values {
		if value == nil {
			results[i] = CacheResult{State: CacheMiss}
			continue
		}

		switch data := value.(type) {
		case string:
			results[i] = cacheResult([]byte(data))
		case []byte:
			results[i] = cacheResult(data)
		default:
			return nil, fmt.Errorf("unexpected Redis MGET value type %T", value)
		}
	}
	return results, nil
}

func (s *Store) Set(ctx context.Context, key string, data []byte, ttl time.Duration) error {
	if ttl <= 0 {
		return fmt.Errorf("cache TTL must be positive")
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.client.Set(ctx, key, data, ttl).Err()
}

func (s *Store) SetNull(ctx context.Context, key string, ttl time.Duration) error {
	return s.Set(ctx, key, []byte(nullSentinel), ttl)
}

func (s *Store) Delete(ctx context.Context, key string) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()
	return s.client.Del(ctx, key).Err()
}

func cacheResult(data []byte) CacheResult {
	if string(data) == nullSentinel {
		return CacheResult{State: CacheNull}
	}

	copyData := make([]byte, len(data))
	copy(copyData, data)
	return CacheResult{State: CacheHit, Data: copyData}
}
