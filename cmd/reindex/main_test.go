package main

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"gorush/internal/redisx"
	"gorush/internal/repository"
)

func TestRunReturnsRepositoryError(t *testing.T) {
	repo := &fakeLocationRepository{err: errors.New("list locations")}
	geo := &fakeGeoRebuilder{}

	err := run(context.Background(), repo, geo)

	if !errors.Is(err, repo.err) {
		t.Fatalf("run error = %v, want %v", err, repo.err)
	}
	if repo.calls != 1 {
		t.Errorf("ListOnlineLocations calls = %d, want 1", repo.calls)
	}
	if geo.calls != 0 {
		t.Errorf("RebuildGeo calls = %d, want 0", geo.calls)
	}
}

func TestRunReturnsRebuildError(t *testing.T) {
	repo := &fakeLocationRepository{locations: []repository.ShopLocation{{
		ID: 1, TypeID: 2, Longitude: 116.4, Latitude: 39.9,
	}}}
	geo := &fakeGeoRebuilder{err: errors.New("rebuild geo")}

	err := run(context.Background(), repo, geo)

	if !errors.Is(err, geo.err) {
		t.Fatalf("run error = %v, want %v", err, geo.err)
	}
	if repo.calls != 1 {
		t.Errorf("ListOnlineLocations calls = %d, want 1", repo.calls)
	}
	if geo.calls != 1 {
		t.Errorf("RebuildGeo calls = %d, want 1", geo.calls)
	}
}

func TestRunConvertsLocationsForRebuild(t *testing.T) {
	repo := &fakeLocationRepository{locations: []repository.ShopLocation{
		{ID: 8, TypeID: 3, Longitude: 116.480, Latitude: 39.997},
		{ID: 9, TypeID: 4, Longitude: 116.314, Latitude: 39.984},
	}}
	geo := &fakeGeoRebuilder{}

	if err := run(context.Background(), repo, geo); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	want := []redisx.GeoShop{
		{ID: 8, TypeID: 3, Longitude: 116.480, Latitude: 39.997},
		{ID: 9, TypeID: 4, Longitude: 116.314, Latitude: 39.984},
	}
	if !reflect.DeepEqual(geo.shops, want) {
		t.Errorf("RebuildGeo shops = %#v, want %#v", geo.shops, want)
	}
	if repo.calls != 1 {
		t.Errorf("ListOnlineLocations calls = %d, want 1", repo.calls)
	}
	if geo.calls != 1 {
		t.Errorf("RebuildGeo calls = %d, want 1", geo.calls)
	}
}

func TestRunRebuildsEmptyLocations(t *testing.T) {
	repo := &fakeLocationRepository{locations: []repository.ShopLocation{}}
	geo := &fakeGeoRebuilder{}

	if err := run(context.Background(), repo, geo); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if geo.calls != 1 {
		t.Fatalf("RebuildGeo calls = %d, want 1", geo.calls)
	}
	if len(geo.shops) != 0 {
		t.Errorf("RebuildGeo shops = %#v, want empty", geo.shops)
	}
}

type fakeLocationRepository struct {
	locations []repository.ShopLocation
	err       error
	calls     int
}

func (f *fakeLocationRepository) ListOnlineLocations(context.Context) ([]repository.ShopLocation, error) {
	f.calls++
	return f.locations, f.err
}

type fakeGeoRebuilder struct {
	shops []redisx.GeoShop
	err   error
	calls int
}

func (f *fakeGeoRebuilder) RebuildGeo(_ context.Context, shops []redisx.GeoShop) error {
	f.calls++
	f.shops = shops
	return f.err
}
