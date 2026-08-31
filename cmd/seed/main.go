// Command seed 灌入演示数据，让平台立即像个"大众点评后端"。
//
// 行为：每次执行都先 truncate 演示数据相关的表，再灌入固定数据，幂等。
//
// 用法：
//
//	go run ./cmd/seed
package main

import (
	"log"
	"time"

	"gorm.io/gorm"

	"gorush/internal/config"
	"gorush/internal/database"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}
	db, err := database.Open(database.Config{DSN: cfg.DSN()})
	if err != nil {
		log.Fatalf("db: %v", err)
	}

	// 注意顺序：从最外键引用方开始 truncate，否则 FK 报错。
	tables := []string{
		"favorites",
		"reviews",
		"orders",
		"seckill_vouchers",
		"vouchers",
		"shops",
		"shop_types",
		"users",
	}
	for _, t := range tables {
		if err := db.Exec("TRUNCATE TABLE " + t).Error; err != nil {
			log.Fatalf("truncate %s: %v", t, err)
		}
	}

	now := time.Now()
	future := now.Add(30 * 24 * time.Hour)

	// ---------- users ----------
	for _, u := range []struct{ nick, phone string }{
		{"Alice", "13800000001"},
		{"Bob", "13800000002"},
		{"Carol", "13800000003"},
	} {
		if err := db.Exec(
			"INSERT INTO users(nickname, phone) VALUES (?, ?)", u.nick, u.phone,
		).Error; err != nil {
			log.Fatalf("seed user: %v", err)
		}
	}

	// ---------- shop_types ----------
	for _, t := range []struct {
		name string
		sort int
	}{
		{"美食", 1}, {"电影", 2}, {"酒店", 3}, {"休闲娱乐", 4},
	} {
		if err := db.Exec(
			"INSERT INTO shop_types(name, sort) VALUES (?, ?)", t.name, t.sort,
		).Error; err != nil {
			log.Fatalf("seed shop_type: %v", err)
		}
	}

	// ---------- shops ----------
	for _, s := range []struct {
		name     string
		typeName string
		address  string
		lng, lat float64
		avg      int
		desc     string
	}{
		{"海底捞(望京店)", "美食", "北京市朝阳区望京街 9 号", 116.480, 39.997, 12000, "火锅连锁"},
		{"星巴克(三里屯店)", "美食", "北京市朝阳区三里屯路 19 号", 116.453, 39.937, 5000, "咖啡"},
		{"万达影城(国贸店)", "电影", "北京市朝阳区建国路 93 号", 116.461, 39.910, 8000, "IMAX 影院"},
		{"全季酒店(中关村店)", "酒店", "北京市海淀区中关村大街 27 号", 116.314, 39.984, 30000, "商务酒店"},
	} {
		if err := db.Exec(`
			INSERT INTO shops(type_id, name, address, longitude, latitude, avg_price, description)
			VALUES ((SELECT id FROM shop_types WHERE name = ?), ?, ?, ?, ?, ?, ?)
		`, s.typeName, s.name, s.address, s.lng, s.lat, s.avg, s.desc,
		).Error; err != nil {
			log.Fatalf("seed shop: %v", err)
		}
	}

	// ---------- vouchers + seckill_vouchers ----------
	insertVoucher(db, "海底捞(望京店)", "满 200 减 30", 1, 3000, now, future)
	insertVoucher(db, "星巴克(三里屯店)", "第二杯半价", 1, 2500, now, future)
	insertVoucher(db, "万达影城(国贸店)", "IMAX 电影票 9 折", 1, 9000, now, future)
	insertVoucher(db, "全季酒店(中关村店)", "标准大床房 8 折", 1, 8000, now, future)
	insertSeckill(db, "全季酒店(中关村店)", "99 元秒杀豪华大床房", 100, 9900, now, future)

	log.Printf("seed done.")
}

// insertVoucher 给指定商家插入一条普通券。
func insertVoucher(db *gorm.DB, shopName, title string, vtype, discount int, begin, end time.Time) {
	if err := db.Exec(`
		INSERT INTO vouchers(shop_id, title, voucher_type, discount_value, begin_time, end_time, status)
		VALUES (
		  (SELECT id FROM shops WHERE name = ?),
		  ?, ?, ?, ?, ?, 1
		)`, shopName, title, vtype, discount, begin, end,
	).Error; err != nil {
		log.Fatalf("seed voucher: %v", err)
	}
}

// insertSeckill 给指定商家插入一张秒杀券：先写 vouchers，再用 LAST_INSERT_ID 反查 voucher_id。
func insertSeckill(db *gorm.DB, shopName, title string, stock, price int, begin, end time.Time) {
	if err := db.Exec(`
		INSERT INTO vouchers(shop_id, title, voucher_type, discount_value, begin_time, end_time, status)
		VALUES (
		  (SELECT id FROM shops WHERE name = ?),
		  ?, 2, ?, ?, ?, 1
		)`, shopName, title, price, begin, end,
	).Error; err != nil {
		log.Fatalf("seed seckill voucher: %v", err)
	}
	var voucherID uint64
	if err := db.Raw(`
		SELECT id FROM vouchers
		WHERE shop_id = (SELECT id FROM shops WHERE name = ?) AND title = ?
		ORDER BY id DESC LIMIT 1
	`, shopName, title).Scan(&voucherID).Error; err != nil {
		log.Fatalf("lookup seckill voucher id: %v", err)
	}
	if err := db.Exec(`
		INSERT INTO seckill_vouchers(voucher_id, stock, begin_time, end_time)
		VALUES (?, ?, ?, ?)
	`, voucherID, stock, begin, end,
	).Error; err != nil {
		log.Fatalf("seed seckill stock: %v", err)
	}
}
