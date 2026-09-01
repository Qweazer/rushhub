package redisx

import (
	"context"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

type GeoShop struct {
	ID                  uint64
	TypeID              uint64
	Longitude, Latitude float64
}

type GeoResult struct {
	ShopID         uint64
	DistanceMeters float64
}

func (s *Store) GeoSearch(ctx context.Context, typeID uint64, lng, lat, radiusMeters float64, count int) ([]GeoResult, error) {
	if count <= 0 {
		return nil, fmt.Errorf("GEO search count must be positive")
	}

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	key := GeoAllKey()
	if typeID != 0 {
		key = GeoTypeKey(typeID)
	}
	locations, err := s.client.GeoSearchLocation(ctx, key, &redis.GeoSearchLocationQuery{
		GeoSearchQuery: redis.GeoSearchQuery{
			Longitude:  lng,
			Latitude:   lat,
			Radius:     radiusMeters,
			RadiusUnit: "m",
			Sort:       "ASC",
			Count:      count,
		},
		WithDist: true,
	}).Result()
	if err != nil {
		return nil, err
	}

	results := make([]GeoResult, len(locations))
	for i, location := range locations {
		shopID, err := strconv.ParseUint(location.Name, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse GEO shop member %q: %w", location.Name, err)
		}
		results[i] = GeoResult{ShopID: shopID, DistanceMeters: location.Dist}
	}
	return results, nil
}

func (s *Store) RebuildGeo(ctx context.Context, shops []GeoShop) error {
	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	keys, err := s.geoTypeKeys(ctx)
	if err != nil {
		return err
	}
	keys = append(keys, GeoAllKey())
	if err := s.client.Del(ctx, keys...).Err(); err != nil {
		return err
	}

	locationsByKey := make(map[string][]*redis.GeoLocation)
	for _, shop := range shops {
		location := &redis.GeoLocation{
			Name:      strconv.FormatUint(shop.ID, 10),
			Longitude: shop.Longitude,
			Latitude:  shop.Latitude,
		}
		locationsByKey[GeoAllKey()] = append(locationsByKey[GeoAllKey()], location)
		locationsByKey[GeoTypeKey(shop.TypeID)] = append(locationsByKey[GeoTypeKey(shop.TypeID)], location)
	}

	pipe := s.client.Pipeline()
	for key, locations := range locationsByKey {
		pipe.GeoAdd(ctx, key, locations...)
	}
	_, err = pipe.Exec(ctx)
	return err
}

func (s *Store) geoTypeKeys(ctx context.Context) ([]string, error) {
	var (
		cursor uint64
		keys   []string
	)
	for {
		found, nextCursor, err := s.client.Scan(ctx, cursor, "gorush:geo:shops:type:*", 100).Result()
		if err != nil {
			return nil, err
		}
		keys = append(keys, found...)
		cursor = nextCursor
		if cursor == 0 {
			return keys, nil
		}
	}
}
