package config

import (
	"net/url"
	"strings"
	"testing"
)

func TestRenderPostgresURLUsesTLS(t *testing.T) {
	dsn, ssl := NormalizeDatabaseURL("postgres://user:pass@dpg.render.com:5432/gomeet?sslmode=require")
	if !ssl {
		t.Fatal("expected ssl")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "postgresql" || u.Host != "dpg.render.com:5432" || u.Path != "/gomeet" {
		t.Fatalf("dsn %s", dsn)
	}
	if u.Query().Get("sslmode") != "require" {
		t.Fatalf("sslmode %q", u.Query().Get("sslmode"))
	}
	if strings.Contains(dsn, "asyncpg") {
		t.Fatalf("driver prefix left in %s", dsn)
	}
}

func TestRenderRejectsLocalhostDatabase(t *testing.T) {
	t.Setenv("RENDER", "true")
	err := RejectLocalhostOnRender(
		"postgresql+asyncpg://gomeet:gomeet@localhost:5432/gomeet",
		"redis://example.internal:6379/0",
	)
	if err == nil || !strings.Contains(err.Error(), "Internal Database URL") {
		t.Fatalf("err %v", err)
	}
}

func TestAsyncpgURLDropsDriverPrefix(t *testing.T) {
	original := "postgresql+asyncpg://gomeet:gomeet@localhost:5432/gomeet"
	dsn, ssl := NormalizeDatabaseURL(original)
	if ssl {
		t.Fatal("ssl should be off")
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	if u.Scheme != "postgresql" || u.Host != "localhost:5432" || u.Path != "/gomeet" || u.RawQuery != "" {
		t.Fatalf("dsn %s", dsn)
	}
}

func TestRenderRequiresProductionSecrets(t *testing.T) {
	t.Setenv("RENDER", "true")
	err := (Settings{
		JWTSecret:   "change-me",
		CORSOrigins: "http://localhost:3000",
	}).ValidateProduction()
	if err == nil || !strings.Contains(err.Error(), "JWT_SECRET") {
		t.Fatalf("err %v", err)
	}
}
