package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

// Settings is the process configuration. Secrets come from the environment or backend/.env.
type Settings struct {
	DatabaseURL         string
	RedisURL            string
	JWTSecret           string
	JWTExpireMinutes    int
	CORSOrigins         string
	AgoraAppID          string
	AgoraAppCertificate string
	AgoraChatAppKey     string
	AgoraChatRestHost   string
	AgoraCustomerID     string
	AgoraCustomerSecret string
}

// CORSOriginList splits the comma-separated CORS_ORIGINS value.
func (s Settings) CORSOriginList() []string {
	parts := strings.Split(s.CORSOrigins, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(strings.TrimRight(p, "/"))
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// Load reads backend/.env or .env without overriding variables already set, then the environment.
func Load() (Settings, error) {
	loadEnvFile("backend/.env")
	loadEnvFile(".env")

	minutes, err := envInt("JWT_EXPIRE_MINUTES", 10080)
	if err != nil {
		return Settings{}, err
	}
	s := Settings{
		DatabaseURL:         env("DATABASE_URL", "postgresql://gomeet:gomeet@localhost:5432/gomeet"),
		RedisURL:            env("REDIS_URL", "redis://localhost:6379/0"),
		JWTSecret:           env("JWT_SECRET", "change-me"),
		JWTExpireMinutes:    minutes,
		CORSOrigins:         env("CORS_ORIGINS", "http://localhost:3000"),
		AgoraAppID:          env("AGORA_APP_ID", ""),
		AgoraAppCertificate: env("AGORA_APP_CERTIFICATE", ""),
		AgoraChatAppKey:     env("AGORA_CHAT_APP_KEY", ""),
		AgoraChatRestHost:   env("AGORA_CHAT_REST_HOST", ""),
		AgoraCustomerID:     env("AGORA_CUSTOMER_ID", ""),
		AgoraCustomerSecret: env("AGORA_CUSTOMER_SECRET", ""),
	}
	if err := RejectLocalhostOnRender(s.DatabaseURL, s.RedisURL); err != nil {
		return Settings{}, err
	}
	if err := s.ValidateProduction(); err != nil {
		return Settings{}, err
	}
	return s, nil
}

func loadEnvFile(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	_ = godotenv.Load(path)
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok {
		return v
	}
	return fallback
}

func envInt(key string, fallback int) (int, error) {
	v, ok := os.LookupEnv(key)
	if !ok || strings.TrimSpace(v) == "" {
		return fallback, nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer", key)
	}
	return n, nil
}

// NormalizeDatabaseURL rewrites postgres:// and SQLAlchemy asyncpg URLs into a pgx DSN.
// ssl is true when the source URL asked for TLS (sslmode=require or ssl=true).
func NormalizeDatabaseURL(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case strings.HasPrefix(raw, "postgresql+asyncpg://"):
		raw = "postgresql://" + strings.TrimPrefix(raw, "postgresql+asyncpg://")
	case strings.HasPrefix(raw, "postgres://"):
		raw = "postgresql://" + strings.TrimPrefix(raw, "postgres://")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return raw, false
	}
	q := u.Query()
	sslmode := q.Get("sslmode")
	sslFlag := strings.ToLower(q.Get("ssl"))
	ssl := sslmode == "require" || sslmode == "verify-ca" || sslmode == "verify-full" ||
		sslFlag == "1" || sslFlag == "true" || sslFlag == "require"
	if ssl && sslmode == "" {
		q.Set("sslmode", "require")
	}
	q.Del("ssl")
	u.RawQuery = q.Encode()
	return u.String(), ssl
}

var localHosts = map[string]struct{}{
	"localhost": {},
	"127.0.0.1": {},
	"::1":       {},
}

// RejectLocalhostOnRender fails when Render is given a localhost database or Redis URL.
func RejectLocalhostOnRender(databaseURL, redisURL string) error {
	if os.Getenv("RENDER") == "" {
		return nil
	}
	dsn, _ := NormalizeDatabaseURL(databaseURL)
	if _, ok := localHosts[hostname(dsn)]; ok {
		return fmt.Errorf("DATABASE_URL points at localhost. On the Render web service, set DATABASE_URL to the Postgres Internal Database URL. Do not paste backend/.env.")
	}
	if _, ok := localHosts[hostname(redisURL)]; ok {
		return fmt.Errorf("REDIS_URL points at localhost. On the Render web service, set REDIS_URL to the Key Value internal connection string.")
	}
	return nil
}

// ValidateProduction fails closed on Render when secrets or the public origin are still placeholders.
func (s Settings) ValidateProduction() error {
	if os.Getenv("RENDER") == "" {
		return nil
	}
	secret := strings.TrimSpace(s.JWTSecret)
	if len(secret) < 32 || secret == "change-me" || secret == "change-me-to-a-long-random-string" {
		return fmt.Errorf("JWT_SECRET must be a random string of at least 32 characters")
	}
	origins := s.CORSOriginList()
	if len(origins) == 0 {
		return fmt.Errorf("CORS_ORIGINS must be the Vercel site origin, with no trailing slash")
	}
	for _, origin := range origins {
		if strings.Contains(origin, "localhost") || strings.Contains(origin, "127.0.0.1") {
			return fmt.Errorf("CORS_ORIGINS points at localhost. Set it to the Vercel origin, for example https://your-app.vercel.app")
		}
		if !strings.HasPrefix(origin, "https://") {
			return fmt.Errorf("CORS_ORIGINS must use https on Render")
		}
	}
	if strings.TrimSpace(s.AgoraAppID) == "" || strings.TrimSpace(s.AgoraAppCertificate) == "" {
		return fmt.Errorf("AGORA_APP_ID and AGORA_APP_CERTIFICATE are required")
	}
	if strings.TrimSpace(s.AgoraChatRestHost) == "" {
		return fmt.Errorf("AGORA_CHAT_REST_HOST is required")
	}
	return nil
}

func hostname(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Hostname()
}
