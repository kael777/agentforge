package config

import (
	"errors"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config holds the realtime service settings. The zero value is not valid:
// Load always returns a configuration that has passed validate.
type Config struct {
	ServiceName string
	Environment string
	LogLevel    string

	// Address is the public listener. In production it normally binds loopback
	// and Nginx terminates TLS in front of it.
	Address string
	// ManagementAddress serves /readyz, /version and /metrics. It must stay off
	// the public interface.
	ManagementAddress          string
	AllowNonLoopbackManagement bool

	// AllowedOrigins is an exact allowlist. WebSocket handshakes are not
	// protected by the browser's same-origin policy, so the server has to
	// enforce it. "*" is rejected everywhere, not just in production.
	AllowedOrigins []string
	// AllowEmptyOrigin exists for server-to-server clients that send no Origin
	// header. Leave it false when browsers are the only clients.
	AllowEmptyOrigin bool

	// TicketSecret must match the value the API signs tickets with.
	TicketSecret   string
	TicketIssuer   string
	TicketAudience string

	// TrustedProxies lists addresses allowed to supply X-Forwarded-For. Empty
	// means the peer address is always the client address, which is the safe
	// default: otherwise any client could pick its own identity and bypass the
	// per-address connection limit.
	TrustedProxies []string

	Limits Limits

	ShutdownTimeout time.Duration
}

// Limits bounds what one connection, one user or one address may consume.
type Limits struct {
	MaxConnections        int
	MaxConnectionsPerUser int
	MaxConnectionsPerIP   int

	MaxMessageBytes int64
	// SendQueueSize is the per-connection outbound buffer. When it fills up the
	// connection is closed instead of letting the queue grow without bound.
	SendQueueSize int

	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	PingInterval time.Duration

	// MessageRate and MessageBurst configure a token bucket per connection.
	MessageRate  float64
	MessageBurst int
}

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

	cfg.ServiceName = envOrDefault("SERVICE_NAME", "agentforge-ws")
	cfg.Environment = envOrDefault("APP_ENV", "development")
	cfg.LogLevel = envOrDefault("LOG_LEVEL", "info")

	cfg.Address = envOrDefault("WS_ADDR", ":8090")
	cfg.ManagementAddress = envOrDefault("WS_MANAGEMENT_ADDR", "127.0.0.1:8091")
	cfg.AllowNonLoopbackManagement = envBool("WS_MANAGEMENT_ALLOW_NON_LOOPBACK")
	cfg.AllowedOrigins = splitCSV(os.Getenv("WS_ALLOWED_ORIGINS"))
	cfg.AllowEmptyOrigin = envBool("WS_ALLOW_EMPTY_ORIGIN")

	cfg.TicketSecret = strings.TrimSpace(os.Getenv("WS_TICKET_SECRET"))
	cfg.TicketIssuer = envOrDefault("WS_TICKET_ISSUER", "agentforge-api")
	cfg.TicketAudience = envOrDefault("WS_TICKET_AUDIENCE", "agentforge-ws")
	cfg.TrustedProxies = splitCSV(os.Getenv("WS_TRUSTED_PROXIES"))

	if cfg.Limits.MaxConnections, err = intEnv("WS_MAX_CONNECTIONS", 1000); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MaxConnectionsPerUser, err = intEnv("WS_MAX_CONNECTIONS_PER_USER", 5); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MaxConnectionsPerIP, err = intEnv("WS_MAX_CONNECTIONS_PER_IP", 20); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MaxMessageBytes, err = int64Env("WS_MAX_MESSAGE_BYTES", 32*1024); err != nil {
		return Config{}, err
	}
	if cfg.Limits.SendQueueSize, err = intEnv("WS_SEND_QUEUE_SIZE", 256); err != nil {
		return Config{}, err
	}
	if cfg.Limits.ReadTimeout, err = durationEnv("WS_READ_TIMEOUT", 90*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Limits.WriteTimeout, err = durationEnv("WS_WRITE_TIMEOUT", 10*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Limits.PingInterval, err = durationEnv("WS_PING_INTERVAL", 30*time.Second); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MessageRate, err = floatEnv("WS_MESSAGE_RATE", 20); err != nil {
		return Config{}, err
	}
	if cfg.Limits.MessageBurst, err = intEnv("WS_MESSAGE_BURST", 40); err != nil {
		return Config{}, err
	}
	if cfg.ShutdownTimeout, err = durationEnv("SHUTDOWN_TIMEOUT", 15*time.Second); err != nil {
		return Config{}, err
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validate(cfg Config) error {
	if len(cfg.TicketSecret) < 32 {
		return errors.New("WS_TICKET_SECRET must be at least 32 characters and match the API value")
	}
	if err := validateManagementAddress(cfg.ManagementAddress, cfg.AllowNonLoopbackManagement); err != nil {
		return err
	}
	if cfg.ManagementAddress == cfg.Address {
		return errors.New("WS_MANAGEMENT_ADDR must differ from WS_ADDR")
	}
	for _, origin := range cfg.AllowedOrigins {
		if origin == "*" {
			return errors.New("WS_ALLOWED_ORIGINS must not contain a wildcard")
		}
	}
	if len(cfg.AllowedOrigins) == 0 && !cfg.AllowEmptyOrigin {
		return errors.New("WS_ALLOWED_ORIGINS is required unless WS_ALLOW_EMPTY_ORIGIN=true")
	}

	if !isProduction(cfg.Environment) {
		return nil
	}
	if len(cfg.AllowedOrigins) == 0 {
		return errors.New("WS_ALLOWED_ORIGINS is required in production")
	}
	for _, placeholder := range placeholderSecrets {
		if strings.EqualFold(cfg.TicketSecret, placeholder) {
			return errors.New("WS_TICKET_SECRET still holds a repository placeholder value")
		}
	}
	return nil
}

func validateManagementAddress(address string, allowNonLoopback bool) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("WS_MANAGEMENT_ADDR must be host:port: %w", err)
	}
	if host == "" {
		if allowNonLoopback {
			return nil
		}
		return errors.New("WS_MANAGEMENT_ADDR must name a loopback address")
	}
	if strings.EqualFold(host, "localhost") {
		return nil
	}
	if ip := net.ParseIP(host); ip != nil && ip.IsLoopback() {
		return nil
	}
	if allowNonLoopback {
		return nil
	}
	return errors.New("WS_MANAGEMENT_ADDR must be a loopback address; set WS_MANAGEMENT_ALLOW_NON_LOOPBACK=true only when the port is not published")
}

func isProduction(environment string) bool {
	return strings.EqualFold(strings.TrimSpace(environment), "production")
}

func loadDevelopmentEnv(filename string) error {
	if isProduction(os.Getenv("APP_ENV")) {
		return nil
	}
	if _, err := os.Stat(filename); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	// The service reads only variables it knows about; loading a file here
	// keeps local development consistent with the API service.
	content, err := os.ReadFile(filename)
	if err != nil {
		return fmt.Errorf("read %s: %w", filename, err)
	}
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		if _, exists := os.LookupEnv(key); exists {
			continue
		}
		_ = os.Setenv(key, strings.TrimSpace(value))
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
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return parsed, nil
}

func int64Env(key string, fallback int64) (int64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive integer", key)
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

func floatEnv(key string, fallback float64) (float64, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseFloat(value, 64)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s must be a positive number", key)
	}
	return parsed, nil
}
