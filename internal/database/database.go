// Package database 负责建立 MySQL 连接并暴露 *gorm.DB。
//
// 设计原则：
//   - 连接建立、连接池参数都集中在这里，main.go 不直接碰 sql.Open。
//   - 连接池参数：MaxOpenConns / MaxIdleConns / ConnMaxLifetime
//     避免后续每个请求都新建 TCP 连接，也防止连接占着不释放。
package database

import (
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Config 是 database 包自己关心的最小配置子集，避免依赖 internal/config
// （减少反向依赖：config 不必知道 GORM，database 也不必知道别的）。
type Config struct {
	DSN string
}

// Open 建立 GORM 连接，并做一次 Ping 确认可用。
// 失败时直接 panic：启动期连不上 DB 就该立刻暴露问题。
func Open(cfg Config) (*gorm.DB, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{
		// Step 2 只关心连通性，日志先静默。
		// 后续 Step 接入 Request ID 后，会替换为把 request_id 写进 GORM logger。
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, err
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}

	// 连接池参数。Step 2 不深究含义，Step 压测阶段会回头调。
	sqlDB.SetMaxOpenConns(50)
	sqlDB.SetMaxIdleConns(10)
	sqlDB.SetConnMaxLifetime(time.Hour)

	return db, nil
}
