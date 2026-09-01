// Package config 从环境变量（.env 或进程 env）读取配置，集中处理默认值。
//
// 设计原则：
//   - 整个进程只 Load 一次（在 main 里），得到一个 Config 值。
//   - 业务代码不直接读 os.Getenv，避免到处散落环境变量名。
//   - 缺失必填项时直接 panic，启动期就该崩，而不是请求来了再 500。
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	ServerPort string

	DBHost     string
	DBPort     int
	DBUser     string
	DBPassword string
	DBName     string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisTimeout  time.Duration
}

// Load 读取 .env（如果存在），然后把所有配置汇总成一个 Config。
// 调用方需要保证 .env 文件位于进程当前工作目录。
func Load() (*Config, error) {
	// godotenv.Load 找不到文件不会返回 error，开发期允许缺失。
	_ = godotenv.Load()

	port, err := getEnvInt("DB_PORT", 13306)
	if err != nil {
		return nil, err
	}
	redisDB, err := getEnvInt("REDIS_DB", 0)
	if err != nil {
		return nil, err
	}
	redisTimeoutMS, err := getEnvInt("REDIS_TIMEOUT_MS", 200)
	if err != nil {
		return nil, err
	}
	if redisTimeoutMS <= 0 {
		return nil, fmt.Errorf("env REDIS_TIMEOUT_MS must be > 0")
	}

	cfg := &Config{
		ServerPort: getEnv("SERVER_PORT", "18080"),

		DBHost:     getEnv("DB_HOST", "127.0.0.1"),
		DBPort:     port,
		DBUser:     getEnv("DB_USER", "gorush"),
		DBPassword: getEnv("DB_PASSWORD", "gorushpass"),
		DBName:     getEnv("DB_NAME", "gorush"),

		RedisAddr:     getEnv("REDIS_ADDR", "127.0.0.1:16379"),
		RedisPassword: getEnv("REDIS_PASSWORD", ""),
		RedisDB:       redisDB,
		RedisTimeout:  time.Duration(redisTimeoutMS) * time.Millisecond,
	}
	return cfg, nil
}

// DSN 返回 GORM/MySQL 驱动需要的 DSN 字符串。
//   - parseTime=True 让 MySQL 的 DATETIME 自动解析为 time.Time。
//   - loc=Local 让时间用本地时区。
//   - charset=utf8mb4 支持完整 Unicode（emoji 等）。
//   - multiStatements=true 允许一个 Exec 跑多条 SQL（migration 用）。
func (c *Config) DSN() string {
	return fmt.Sprintf(
		"%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&multiStatements=true",
		c.DBUser, c.DBPassword, c.DBHost, c.DBPort, c.DBName,
	)
}

func getEnv(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func getEnvInt(key string, def int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("env %s invalid int %q: %w", key, v, err)
	}
	return n, nil
}
