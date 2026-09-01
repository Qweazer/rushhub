// Command reindex rebuilds Redis GEO indexes from the current online shops.
package main

import (
	"context"
	"log"

	"gorush/internal/config"
	"gorush/internal/database"
	"gorush/internal/redisx"
	"gorush/internal/repository"
)

type locationRepository interface {
	ListOnlineLocations(context.Context) ([]repository.ShopLocation, error)
}

type geoRebuilder interface {
	RebuildGeo(context.Context, []redisx.GeoShop) error
}

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := database.Open(database.Config{DSN: cfg.DSN()})
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	client := redisx.NewClient(redisx.ClientOptions{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
		Timeout:  cfg.RedisTimeout,
	})
	defer client.Close()

	if err := run(context.Background(), repository.NewShopRepository(db), redisx.NewStore(client, cfg.RedisTimeout)); err != nil {
		log.Fatalf("reindex: %v", err)
	}
	log.Printf("reindex done.")
}

func run(ctx context.Context, repo locationRepository, geo geoRebuilder) error {
	locations, err := repo.ListOnlineLocations(ctx)
	if err != nil {
		return err
	}

	shops := make([]redisx.GeoShop, len(locations))
	for i, location := range locations {
		shops[i] = redisx.GeoShop{
			ID:        location.ID,
			TypeID:    location.TypeID,
			Longitude: location.Longitude,
			Latitude:  location.Latitude,
		}
	}
	return geo.RebuildGeo(ctx, shops)
}
