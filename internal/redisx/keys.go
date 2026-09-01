package redisx

import (
	"fmt"
	"time"
)

func ShopDetailKey(id uint64) string {
	return fmt.Sprintf("gorush:shop:detail:%d", id)
}

func ShopVouchersKey(shopID uint64) string {
	return fmt.Sprintf("gorush:shop:vouchers:%d", shopID)
}

func GeoAllKey() string {
	return "gorush:geo:shops:all"
}

func GeoTypeKey(typeID uint64) string {
	return fmt.Sprintf("gorush:geo:shops:type:%d", typeID)
}

func HotKey(day time.Time) string {
	return fmt.Sprintf("gorush:shop:hot:%s", day.Format("20060102"))
}
