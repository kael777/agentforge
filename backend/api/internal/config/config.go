package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv          string
	ServiceName     string
	HTTP            HTTPConfig
	Database        DatabaseConfig
	Auth            AuthConfig
	LogLevel        string
	ShutdownTimeout time.Duration
}

type HTTPConfig struct {
	Address        string
	AllowedOrigins []string
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	IdleTimeout    time.Duration
	BodyLimit      int
}

type DatabaseConfig struct {
	DSN             string
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

type AuthConfig struct {
	JWTSecret string
	TokenTTL  time.Duration
}

func Load() (Config, error) {
	var cfg Config
	var err error
	if err := loadDevelopmentEnv(".env"); err != nil {
		return Config{}, err
	}

	cfg.AppEnv = envOrDefault("APP_ENV", "development")
	cfg.ServiceName = envOrDefault("SERVICE_NAME", "agentforge-api")
	cfg.LogLevel = envOrDefault("LOG_LEVEL", "info")
	cfg.HTTP.Address = envOrDefault("HTTP_ADDR", ":55555")
	cfg.HTTP.AllowedOrigins = splitCSV(envOrDefault("HTTP_ALLOWED_ORIGINS", "http://localhost:5173"))
	cfg.HTTP.ReadTimeout = 10 * time.Second
	cfg.HTTP.WriteTimeout = 30 * time.Second
	cfg.HTTP.IdleTimeout = 60 * time.Second
	cfg.HTTP.BodyLimit = 2 * 1024 * 1024

	cfg.Database.DSN = strings.TrimSpace(os.Getenv("DATABASE_DSN"))
	if cfg.Database.DSN == "" {
		return Config{}, errors.New("DATABASE_DSN is required")
	}

	if cfg.Database.MaxIdleConns, err = intEnv("DATABASE_MAX_IDLE_CONNS", 5); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxOpenConns, err = intEnv("DATABASE_MAX_OPEN_CONNS", 20); err != nil {
		return Config{}, err
	}
	if cfg.Database.MaxIdleConns > cfg.Database.MaxOpenConns {
		return Config{}, errors.New("DATABASE_MAX_IDLE_CONNS cannot exceed DATABASE_MAX_OPEN_CONNS")
	}
	if cfg.Database.ConnMaxLifetime, err = durationEnv("DATABASE_CONN_MAX_LIFETIME", time.Hour); err != nil {
		return Config{}, err
	}

	cfg.Auth.JWTSecret = os.Getenv("JWT_SECRET")
	if len(cfg.Auth.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must be at least 32 characters")
	}
	if cfg.Auth.TokenTTL, err = durationEnv("JWT_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func loadDevelopmentEnv(filename string) error {
	if strings.EqualFold(strings.TrimSpace(os.Getenv("APP_ENV")), "production") {
		return nil
	}
	if err := godotenv.Load(filename); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("load %s: %w", filename, err)
	}
	return nil
}

func envOrDefault(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func splitCSV(value string) []string {
	parts := strings.Split(value, ",")
	result := make([]string, 0, len(parts))
	for _, part := range parts {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil || parsed < 0 {
		return 0, fmt.Errorf("%s must be a non-negative integer", key)
	}
	return parsed, nil
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive duration", key)
	}
	return parsed, nil
}
