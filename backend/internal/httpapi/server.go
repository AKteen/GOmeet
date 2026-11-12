package httpapi

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/redis/go-redis/v9"

	"gomeet/internal/agora"
	"gomeet/internal/config"
	"gomeet/internal/presence"
	"gomeet/internal/store"
)

type app struct {
	cfg      config.Settings
	store    store.Store
	redis    *redis.Client
	agora    *agora.Client
	presence *presence.Service
}

// New returns the HTTP handler for the GOmeet API.
func New(cfg config.Settings, st store.Store, rdb *redis.Client, ag *agora.Client) http.Handler {
	a := &app{
		cfg:      cfg,
		store:    st,
		redis:    rdb,
		agora:    ag,
		presence: presence.New(rdb),
	}
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(securityHeaders)
	r.Use(limitBody)
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   cfg.CORSOriginList(),
		AllowedMethods:   []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "X-Requested-With"},
		AllowCredentials: true,
		MaxAge:           int((12 * time.Hour).Seconds()),
	}))

	r.Get("/health", a.health)
	r.Post("/auth/signup", a.signup)
	r.Post("/auth/login", a.login)
	r.Get("/auth/me", a.me)

	r.Post("/rooms", a.createRoom)
	r.Get("/rooms/mine", a.myRooms)
	r.Get("/rooms/{slug}", a.getRoom)
	r.Post("/rooms/{slug}/join", a.joinRoom)
	r.Post("/rooms/{slug}/heartbeat", a.heartbeat)
	r.Post("/rooms/{slug}/leave", a.leaveRoom)
	r.Post("/rooms/{slug}/transcript", a.transcript)
	r.Get("/rooms/{slug}/sessions", a.sessions)
	return r
}

func (a *app) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		next.ServeHTTP(w, r)
	})
}
