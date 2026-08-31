package main

import (
	"fmt"
	"log"

	"gorush/internal/config"
	"gorush/internal/database"
	"gorush/internal/httpx"
	"gorush/internal/router"
)

func main() {
	// 1) 结构化日志（Step 9）
	httpx.Init()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db, err := database.Open(database.Config{DSN: cfg.DSN()})
	if err != nil {
		log.Fatalf("connect mysql: %v", err)
	}

	addr := fmt.Sprintf(":%s", cfg.ServerPort)
	log.Printf("GoRush listening on %s (db=%s:%d/%s)",
		addr, cfg.DBHost, cfg.DBPort, cfg.DBName)

	r := router.New(db)
	if err := r.Run(addr); err != nil {
		log.Fatalf("server failed: %v", err)
	}
}
