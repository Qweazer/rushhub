// Package migrate 是 GoRush 的最简 migration runner。
//
// 设计取舍：
//   - 没有用 golang-migrate 之类现成库，避免 Day 1 引入更多依赖。
//   - 通过一张 schema_migrations 表记录已应用的 migration 文件名（lexical 序）。
//   - 每次启动只跑"还没跑过"的文件，幂等。
//   - 每个文件整段 Exec（依赖 multiStatements=true）。
package migrate

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// Run 扫描 dir 下所有 *.sql，按文件名顺序执行未跑过的 migration。
func Run(db *gorm.DB, dir string) error {
	// 1. 保证 schema_migrations 表存在。
	if err := db.Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			filename   VARCHAR(255) NOT NULL,
			applied_at DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (filename)
		) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4
	`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	// 2. 读出"已应用"集合。
	applied := map[string]bool{}
	rows, err := db.Raw("SELECT filename FROM schema_migrations").Rows()
	if err != nil {
		return fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var f string
		if err := rows.Scan(&f); err != nil {
			return fmt.Errorf("scan schema_migrations: %w", err)
		}
		applied[f] = true
	}

	// 3. 扫描目录里所有 .sql。
	entries, err := os.ReadDir(dir)
	if err != nil {
		return fmt.Errorf("read dir %s: %w", dir, err)
	}
	var files []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if strings.HasSuffix(e.Name(), ".sql") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	// 4. 逐个执行未应用的。
	for _, name := range files {
		if applied[name] {
			fmt.Printf("[migrate] skip %s\n", name)
			continue
		}
		path := filepath.Join(dir, name)
		buf, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read %s: %w", path, err)
		}
		sql := string(buf)

		fmt.Printf("[migrate] apply %s\n", name)
		if err := db.Exec(sql).Error; err != nil {
			return fmt.Errorf("apply %s: %w", name, err)
		}
		if err := db.Exec(
			"INSERT INTO schema_migrations(filename) VALUES (?)", name,
		).Error; err != nil {
			return fmt.Errorf("record %s: %w", name, err)
		}
	}
	return nil
}
