package migrate

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"sort"

	"gorm.io/gorm"
)

//go:embed sql/*.sql
var migrationFiles embed.FS

func Run(ctx context.Context, db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database is required")
	}

	if err := db.WithContext(ctx).Exec(`
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version VARCHAR(255) PRIMARY KEY,
			applied_at TIMESTAMP(6) NOT NULL DEFAULT CURRENT_TIMESTAMP(6)
		)
	`).Error; err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	entries, err := fs.ReadDir(migrationFiles, "sql")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })

	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".sql" {
			continue
		}
		version := entry.Name()

		var count int64
		if err := db.WithContext(ctx).Raw(
			"SELECT COUNT(*) FROM schema_migrations WHERE version = ?", version,
		).Scan(&count).Error; err != nil {
			return fmt.Errorf("check migration %s: %w", version, err)
		}
		if count > 0 {
			continue
		}

		contents, err := migrationFiles.ReadFile("sql/" + entry.Name())
		if err != nil {
			return fmt.Errorf("read migration %s: %w", version, err)
		}

		if err := db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec(string(contents)).Error; err != nil {
				return err
			}
			return tx.Exec("INSERT INTO schema_migrations(version) VALUES (?)", version).Error
		}); err != nil {
			return fmt.Errorf("apply migration %s: %w", version, err)
		}
	}

	return nil
}
