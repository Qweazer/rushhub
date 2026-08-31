// Command migrate 应用所有未执行的 migration。
//
// 用法：
//
//	go run ./cmd/migrate
//
// 依赖：
//   - .env（经由 internal/config 读取）
//   - migrations/ 目录（与 main 包同级目录）
package main

import (
	"log"
	"path/filepath"
	"runtime"

	"gorush/internal/config"
	"gorush/internal/database"
	"gorush/internal/migrate"
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

	// 通过源文件位置定位 migrations/，避免依赖 CWD。
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		log.Fatalf("locate migrations dir: runtime.Caller failed")
	}
	// sourceFile = cmd/migrate/main.go -> 上两级到仓库根 -> 进 migrations/
	dir := filepath.Join(filepath.Dir(sourceFile), "..", "..", "migrations")

	if err := migrate.Run(db, dir); err != nil {
		log.Fatalf("migrate: %v", err)
	}
	log.Printf("migrate done.")
}
