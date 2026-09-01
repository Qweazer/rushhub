package redisx

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"
)

const hotTTL = 72 * time.Hour

type RankedShop struct {
	ShopID uint64
	Views  int64
}

func (s *Store) IncrementHot(ctx context.Context, shopID uint64, day time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	key := HotKey(day)
	pipe := s.client.TxPipeline()
	pipe.ZIncrBy(ctx, key, 1, strconv.FormatUint(shopID, 10))
	pipe.Expire(ctx, key, hotTTL)
	_, err := pipe.Exec(ctx)
	return err
}

func (s *Store) TopHot(ctx context.Context, day time.Time, limit int) ([]RankedShop, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("hot ranking limit must be positive")
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	values, err := s.client.ZRevRangeWithScores(ctx, HotKey(day), 0, int64(limit-1)).Result()
	if err != nil {
		return nil, err
	}

	ranked := make([]RankedShop, len(values))
	for i, value := range values {
		member, ok := value.Member.(string)
		if !ok {
			return nil, fmt.Errorf("unexpected hot shop member type %T", value.Member)
		}
		shopID, err := strconv.ParseUint(member, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse hot shop member %q: %w", member, err)
		}
		if math.Trunc(value.Score) != value.Score {
			return nil, fmt.Errorf("hot shop score %f is not integral", value.Score)
		}
		ranked[i] = RankedShop{ShopID: shopID, Views: int64(value.Score)}
	}
	return ranked, nil
}
