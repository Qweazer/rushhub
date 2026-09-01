package repository

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"

	"gorush/internal/model"
)

func TestShopRepository_GetByIDs(t *testing.T) {
	t.Parallel()

	emptyRepo := NewShopRepository(nil)
	emptyRows, err := emptyRepo.GetByIDs(context.Background(), nil)
	if err != nil {
		t.Fatalf("GetByIDs with empty ids returned error: %v", err)
	}
	if len(emptyRows) != 0 {
		t.Fatalf("GetByIDs with empty ids returned %d rows, want 0", len(emptyRows))
	}

	repo, tx := newShopRepositoryIntegrationTest(t)
	shops := createTestShops(t, tx, "get-by-ids", model.ShopStatusOnline, model.ShopStatusOnline, model.ShopStatusOnline)

	rows, err := repo.GetByIDs(context.Background(), []uint64{shops[2].ID, shops[0].ID, 999999})
	if err != nil {
		t.Fatalf("GetByIDs returned error: %v", err)
	}

	counts := make(map[uint64]int, len(rows))
	for _, row := range rows {
		counts[row.ID]++
	}
	for _, id := range []uint64{shops[0].ID, shops[2].ID} {
		if counts[id] != 1 {
			t.Errorf("GetByIDs row for id %d appeared %d times, want once", id, counts[id])
		}
	}
	if len(counts) != 2 {
		t.Errorf("GetByIDs returned IDs %v, want only %d and %d", sortedIDs(counts), shops[0].ID, shops[2].ID)
	}
}

func TestShopRepository_ListOnlineLocations(t *testing.T) {
	t.Parallel()

	repo, tx := newShopRepositoryIntegrationTest(t)
	onlineShops := createTestShops(t, tx, "online-locations", model.ShopStatusOnline, model.ShopStatusOnline, model.ShopStatusOnline)
	offlineShop := createTestShops(t, tx, "offline-location", model.ShopStatusOffline)[0]

	locations, err := repo.ListOnlineLocations(context.Background())
	if err != nil {
		t.Fatalf("ListOnlineLocations returned error: %v", err)
	}

	byID := make(map[uint64]ShopLocation, len(locations))
	ids := make([]uint64, 0, len(locations))
	for _, location := range locations {
		byID[location.ID] = location
		ids = append(ids, location.ID)
	}
	if !sort.SliceIsSorted(ids, func(i, j int) bool { return ids[i] < ids[j] }) {
		t.Errorf("ListOnlineLocations returned IDs %v, want ascending order", ids)
	}
	for _, shop := range onlineShops {
		location, ok := byID[shop.ID]
		if !ok {
			t.Errorf("ListOnlineLocations omitted online shop %d", shop.ID)
			continue
		}
		if location.TypeID != shop.TypeID || location.Longitude != shop.Longitude || location.Latitude != shop.Latitude {
			t.Errorf("location for shop %d = %+v, want type_id=%d longitude=%v latitude=%v", shop.ID, location, shop.TypeID, shop.Longitude, shop.Latitude)
		}
	}
	if _, ok := byID[offlineShop.ID]; ok {
		t.Errorf("ListOnlineLocations included offline shop %d", offlineShop.ID)
	}
}

func newShopRepositoryIntegrationTest(t *testing.T) (*ShopRepository, *gorm.DB) {
	t.Helper()

	dsn := os.Getenv("GORUSH_TEST_DSN")
	if dsn == "" {
		t.Skip("GORUSH_TEST_DSN is not set")
	}

	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open test database: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("get test sql database: %v", err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })

	tx := db.Begin()
	if tx.Error != nil {
		t.Fatalf("begin test transaction: %v", tx.Error)
	}
	t.Cleanup(func() {
		if err := tx.Rollback().Error; err != nil && !errors.Is(err, gorm.ErrInvalidTransaction) {
			t.Errorf("rollback test transaction: %v", err)
		}
	})

	return NewShopRepository(tx), tx
}

func createTestShops(t *testing.T, tx *gorm.DB, label string, statuses ...int) []model.Shop {
	t.Helper()

	shops := make([]model.Shop, len(statuses))
	for i, status := range statuses {
		shops[i] = model.Shop{
			TypeID:    uint64(8100 + i),
			Name:      fmt.Sprintf("task-3-%s-%d-%d", label, time.Now().UnixNano(), i),
			Address:   "integration-test-address",
			Longitude: 120.100001 + float64(i),
			Latitude:  30.200001 + float64(i),
			Status:    int8(status),
		}
	}
	if err := tx.Create(&shops).Error; err != nil {
		t.Fatalf("create test shops: %v", err)
	}
	return shops
}

func sortedIDs(rows map[uint64]int) []uint64 {
	ids := make([]uint64, 0, len(rows))
	for id := range rows {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
