package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/kael777/agentforge/backend/api/internal/config"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Connection struct {
	GORM *gorm.DB
	SQL  *sql.DB
}

func Open(cfg config.DatabaseConfig) (*Connection, error) {
	db, err := gorm.Open(mysql.Open(cfg.DSN), &gorm.Config{TranslateError: true})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("get database pool: %w", err)
	}
	sqlDB.SetMaxIdleConns(cfg.MaxIdleConns)
	sqlDB.SetMaxOpenConns(cfg.MaxOpenConns)
	sqlDB.SetConnMaxLifetime(cfg.ConnMaxLifetime)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		_ = sqlDB.Close()
		return nil, fmt.Errorf("ping database: %w", err)
	}

	return &Connection{GORM: db, SQL: sqlDB}, nil
}

func (c *Connection) Close() error {
	if c == nil || c.SQL == nil {
		return nil
	}
	return c.SQL.Close()
}
