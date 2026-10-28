package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

// Config holds every runtime setting the process needs. Loading fails fast:
// a configuration that cannot be proven safe never reaches a listener.
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
	// Address is the public listener. Only business routes are reachable here.
	Address string
	// ManagementAddress serves dependency probes, build metadata and metrics.
	// It must stay off the public interface.
	ManagementAddress string
	// AllowNonLoopbackManagement records the explicit decision to bind the
	// management listener to a non-loopback address, which is only acceptable
	// when the port is not published outside the host or container network.
	AllowNonLoopbackManagement bool
	AllowedOrigins             []string
	ReadTimeout                time.Duration
	WriteTimeout               time.Duration
	IdleTimeout                time.Duration
	BodyLimit                  int
	// TrustedProxies lists the addresses allowed to supply X-Forwarded-For.
	// When it is empty no proxy header is trusted, so a client cannot choose
	// its own source IP and bypass per-IP limits.
	TrustedProxies []string
	AuthRateLimit  RateLimitConfig
}

// RateLimitConfig bounds how often an unauthenticated caller may hit one
// endpoint from a single source address.
type RateLimitConfig struct {
	Max    int
	Window time.Duration
}

type DatabaseConfig struct {
	DSN             string
	MaxIdleConns    int
	MaxOpenConns    int
	ConnMaxLifetime time.Duration
}

type AuthConfig struct {
	JWTSecret string
	// Issuer and Audience are validated on every request so a token minted for
	// another service cannot be replayed here.
	Issuer   string
	Audience string
	TokenTTL time.Duration
}

// Placeholder secrets are committed to the repository, so a production
// deployment that still carries one of them is misconfigured by definition.
var placeholderSecrets = []string{
	"replace-this-with-at-least-32-characters",
	"local-development-secret-at-least-32-characters",
	"changeme",
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
	cfg.HTTP.ManagementAddress = envOrDefault("MANAGEMENT_ADDR", "127.0.0.1:55556")
	cfg.HTTP.AllowNonLoopbackManagement = envBool("MANAGEMENT_ALLOW_NON_LOOPBACK")
	cfg.HTTP.AllowedOrigins = splitCSV(envOrDefault("HTTP_ALLOWED_ORIGINS", "http://localhost:5173"))
	cfg.HTTP.ReadTimeout = 10 * time.Second
	cfg.HTTP.WriteTimeout = 30 * time.Second
	cfg.HTTP.IdleTimeout = 60 * time.Second
	cfg.HTTP.BodyLimit = 2 * 1024 * 1024
	cfg.HTTP.TrustedProxies = splitCSV(os.Getenv("HTTP_TRUSTED_PROXIES"))
	if cfg.HTTP.AuthRateLimit.Max, err = intEnv("AUTH_RATE_LIMIT_MAX", 10); err != nil {
		return Config{}, err
	}
	if cfg.HTTP.AuthRateLimit.Window, err = durationEnv("AUTH_RATE_LIMIT_WINDOW", time.Minute); err != nil {
		return Config{}, err
	}

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
	cfg.Auth.Issuer = envOrDefault("JWT_ISSUER", "agentforge-api")
	cfg.Auth.Audience = envOrDefault("JWT_AUDIENCE", "agentforge-web")
	if cfg.Auth.TokenTTL, err = durationEnv("JWT_TTL", 24*time.Hour); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// validate enforces the invariants that keep the deployment topology honest.
// Most checks only apply to production, because local development should stay
// frictionless.
func validate(cfg Config) error {
	if err := validateManagementAddress(cfg.HTTP.ManagementAddress, cfg.HTTP.AllowNonLoopbackManagement); err != nil {
		return err
	}
	if cfg.HTTP.ManagementAddress == cfg.HTTP.Address {
		return errors.New("MANAGEMENT_ADDR must differ from HTTP_ADDR")
	}
	if !isProduction(cfg.AppEnv) {
		return nil
	}

	for _, origin := range cfg.HTTP.AllowedOrigins {
		if origin == "*" {
			return errors.New("HTTP_ALLOWED_ORIGINS must not contain a wildcard in production")
		}
	}
	for _, placeholder := range placeholderSecrets {
		if strings.EqualFold(strings.TrimSpace(cfg.Auth.JWTSecret), placeholder) {
			return errors.New("JWT_SECRET still holds a repository placeholder value")
		}
	}
	return nil
}

func validateManagementAddress(address string, allowNonLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("MANAGEMENT_ADDR must be host:port: %w", err)
	}
	if host == "" || strings.EqualFold(host, "localhost") {
		if allowNonLoopback {
			return nil
		}
		if host == "" {
			return errors.New("MANAGEMENT_ADDR must name a loopback address")
		}
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if allowNonLoopback {
		return nil
	}
	return errors.New("MANAGEMENT_ADDR must be a loopback address; set MANAGEMENT_ALLOW_NON_LOOPBACK=true only when the port is not published")
}

func isProduction(environment string) bool {
	return strings.EqualFold(strings.TrimSpace(environment), "production")
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

func envBool(key string) bool {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return false
	}
	parsed, err := strconv.ParseBool(value)
	return err == nil && parsed
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
