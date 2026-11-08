package config

import (
	"bufio"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Env              string
	HTTPAddr         string
	DatabaseURL      string
	AllowedOrigins   []string
	UploadStorageDir string
	StaticFilesDir   string
	SessionSecret    string
	SessionTTL       time.Duration
	SecureCookies    bool
	CookieSameSite   string
	SeedDevUsers     bool
}

func Load() (*Config, error) {
	loadDotEnvIfPresent(".env")
	env := getEnv("APP_ENV", "development")

	// Default listen address is strictly 127.0.0.1:8080 (never 0.0.0.0 in dev/test).
	defaultAddr := "127.0.0.1:8080"
	if env == "production" {
		if port := strings.TrimSpace(os.Getenv("PORT")); port != "" {
			defaultAddr = "0.0.0.0:" + port
		}
	}
	httpAddr := getEnv("HTTP_ADDR", defaultAddr)
	if strings.HasPrefix(httpAddr, "http://") || strings.HasPrefix(httpAddr, "https://") {
		return nil, errors.New("HTTP_ADDR must be host:port (e.g. 127.0.0.1:8080), not an http/https URL")
	}
	dbURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if dbURL == "" {
		if env == "production" {
			return nil, errors.New("DATABASE_URL must be configured in production")
		}
		// TODO(security): Enforce mTLS (sslmode=verify-full with client certs) for production database connections.
		dbURL = "postgres://chat_app:chat_dev_password@127.0.0.1:5432/chat_online?sslmode=disable"
	}

	var originsRaw string
	if env == "production" {
		originsRaw = strings.TrimSpace(os.Getenv("ALLOWED_ORIGINS"))
		if originsRaw == "" {
			return nil, errors.New("ALLOWED_ORIGINS must be explicitly configured in production")
		}
	} else {
		originsRaw = getEnv("ALLOWED_ORIGINS", "http://127.0.0.1:5173,http://localhost:5173,http://127.0.0.1:8080")
	}

	var origins []string
	for _, o := range strings.Split(originsRaw, ",") {
		trimmed := strings.TrimSpace(o)
		if trimmed == "*" {
			return nil, errors.New("wildcard (*) origin is not allowed in ALLOWED_ORIGINS")
		}
		if trimmed != "" {
			origins = append(origins, trimmed)
		}
	}

	uploadDir := getEnv("UPLOAD_STORAGE_DIR", "./.data/uploads")
	staticDir := strings.TrimSpace(os.Getenv("STATIC_FILES_DIR"))

	sessionSecret, err := resolveSessionSecret(env)
	if err != nil {
		return nil, err
	}

	ttlHours := 24
	if rawTTL := os.Getenv("SESSION_TTL_HOURS"); rawTTL != "" {
		if parsed, parseErr := strconv.Atoi(rawTTL); parseErr == nil && parsed > 0 {
			ttlHours = parsed
		}
	}

	secureCookies := env == "production"
	if rawSecure := os.Getenv("SECURE_COOKIES"); rawSecure != "" {
		secureCookies = rawSecure == "true" || rawSecure == "1"
	}

	seedDev := env != "production"
	if rawSeed := os.Getenv("SEED_DEV_USERS"); rawSeed != "" {
		seedDev = rawSeed == "true" || rawSeed == "1"
	}

	cookieSameSite := strings.ToLower(getEnv("COOKIE_SAME_SITE", "lax"))
	if cookieSameSite != "lax" && cookieSameSite != "none" && cookieSameSite != "strict" {
		return nil, errors.New("COOKIE_SAME_SITE must be 'lax', 'none', or 'strict'")
	}
	if cookieSameSite == "none" && !secureCookies {
		return nil, errors.New("COOKIE_SAME_SITE=none requires SECURE_COOKIES=true")
	}

	return &Config{
		Env:              env,
		HTTPAddr:         httpAddr,
		DatabaseURL:      dbURL,
		AllowedOrigins:   origins,
		UploadStorageDir: uploadDir,
		StaticFilesDir:   staticDir,
		SessionSecret:    sessionSecret,
		SessionTTL:       time.Duration(ttlHours) * time.Hour,
		SecureCookies:    secureCookies,
		CookieSameSite:   cookieSameSite,
		SeedDevUsers:     seedDev,
	}, nil
}

func resolveSessionSecret(env string) (string, error) {
	if secret := strings.TrimSpace(os.Getenv("SESSION_SECRET")); secret != "" {
		if env == "production" && len(secret) < 32 {
			return "", errors.New("SESSION_SECRET must be at least 32 characters in production")
		}
		return secret, nil
	}
	if env == "production" {
		// TODO(security): Integrate with KMS / secret manager for production deployment.
		return "", errors.New("SESSION_SECRET environment variable is required in production")
	}
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	slog.Warn("SESSION_SECRET not set; generated ephemeral secret for non-production instance")
	return hex.EncodeToString(buf), nil
}

func getEnv(key, fallback string) string {
	if val := strings.TrimSpace(os.Getenv(key)); val != "" {
		return val
	}
	return fallback
}

func loadDotEnvIfPresent(filename string) {
	f, err := os.Open(filename)
	if err != nil {
		return
	}
	defer func() {
		_ = f.Close()
	}()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, val)
		}
	}
}
