package config

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setProductionEnv provides the minimum a production load needs, so each test
// can override exactly the variable it is about.
func setProductionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("APP_ENV", "production")
	t.Setenv("DATABASE_DSN", "user:password@tcp(localhost:3306)/test?parseTime=True")
	t.Setenv("JWT_SECRET", "01234567890123456789012345678901")
	t.Setenv("HTTP_ALLOWED_ORIGINS", "http://localhost:5173, https://example.com")
}

func TestLoad(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("JWT_TTL", "2h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Database.DSN != "user:password@tcp(localhost:3306)/test?parseTime=True" {
		t.Fatalf("unexpected DSN: %q", cfg.Database.DSN)
	}
	if cfg.Auth.TokenTTL != 2*time.Hour {
		t.Fatalf("unexpected token TTL: %v", cfg.Auth.TokenTTL)
	}
	if len(cfg.HTTP.AllowedOrigins) != 2 {
		t.Fatalf("unexpected origins: %#v", cfg.HTTP.AllowedOrigins)
	}
	if cfg.Auth.Issuer == "" || cfg.Auth.Audience == "" {
		t.Fatalf("token scope must have defaults: %#v", cfg.Auth)
	}
}

func TestManagementAddressDefaultsToLoopback(t *testing.T) {
	setProductionEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.HTTP.ManagementAddress != "127.0.0.1:55556" {
		t.Fatalf("unexpected management address: %q", cfg.HTTP.ManagementAddress)
	}
	if cfg.HTTP.ManagementAddress == cfg.HTTP.Address {
		t.Fatal("management and public listeners must not share an address")
	}
}

func TestLoadRejectsPublicManagementAddress(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("MANAGEMENT_ADDR", "0.0.0.0:55556")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a management listener on all interfaces")
	}
}

func TestLoadAllowsNonLoopbackManagementWhenOptedIn(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("MANAGEMENT_ADDR", "0.0.0.0:55556")
	t.Setenv("MANAGEMENT_ALLOW_NON_LOOPBACK", "true")

	if _, err := Load(); err != nil {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadRejectsSharedManagementAddress(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("HTTP_ADDR", ":55555")
	t.Setenv("MANAGEMENT_ADDR", ":55555")
	t.Setenv("MANAGEMENT_ALLOW_NON_LOOPBACK", "true")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error when both listeners share an address")
	}
}

func TestLoadRejectsWildcardOriginInProduction(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("HTTP_ALLOWED_ORIGINS", "*")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for a wildcard origin in production")
	}
}

func TestLoadRejectsPlaceholderSecretInProduction(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("JWT_SECRET", "replace-this-with-at-least-32-characters")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error for the repository placeholder secret")
	}
}

func TestLoadRejectsShortJWTSecret(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("JWT_SECRET", "short")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadRejectsInvalidPoolSettings(t *testing.T) {
	setProductionEnv(t)
	t.Setenv("DATABASE_MAX_IDLE_CONNS", "10")
	t.Setenv("DATABASE_MAX_OPEN_CONNS", "5")

	if _, err := Load(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestLoadDevelopmentEnv(t *testing.T) {
	t.Setenv("APP_ENV", "development")
	t.Setenv("DOTENV_TEST_VALUE", "restore-after-test")
	if err := os.Unsetenv("DOTENV_TEST_VALUE"); err != nil {
		t.Fatalf("Unsetenv() error = %v", err)
	}

	filename := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(filename, []byte("DOTENV_TEST_VALUE=loaded\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := loadDevelopmentEnv(filename); err != nil {
		t.Fatalf("loadDevelopmentEnv() error = %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_VALUE"); got != "loaded" {
		t.Fatalf("DOTENV_TEST_VALUE = %q, want loaded", got)
	}
}

func TestProductionDoesNotLoadDotEnv(t *testing.T) {
	t.Setenv("APP_ENV", "production")
	t.Setenv("DOTENV_TEST_VALUE", "from-environment")

	filename := filepath.Join(t.TempDir(), ".env")
	if err := os.WriteFile(filename, []byte("DOTENV_TEST_VALUE=from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
	if err := loadDevelopmentEnv(filename); err != nil {
		t.Fatalf("loadDevelopmentEnv() error = %v", err)
	}
	if got := os.Getenv("DOTENV_TEST_VALUE"); got != "from-environment" {
		t.Fatalf("DOTENV_TEST_VALUE = %q, want from-environment", got)
	}
}
