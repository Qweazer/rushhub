package redisx

import (
	"testing"
	"time"
)

func TestKeysAreNamespaced(t *testing.T) {
	day := time.Date(2026, 8, 31, 10, 0, 0, 0, time.FixedZone("CST", 8*3600))
	cases := map[string]string{
		ShopDetailKey(7):   "gorush:shop:detail:7",
		ShopVouchersKey(7): "gorush:shop:vouchers:7",
		GeoAllKey():        "gorush:geo:shops:all",
		GeoTypeKey(3):      "gorush:geo:shops:type:3",
		HotKey(day):        "gorush:shop:hot:20260831",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %q want %q", got, want)
		}
	}
}
