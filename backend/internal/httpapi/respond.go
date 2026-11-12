package httpapi

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"

	"gomeet/internal/agora"
	"gomeet/internal/presence"
	"gomeet/internal/ratelimit"
	"gomeet/internal/store"
)

type httpError struct {
	status int
	detail string
}

func (e *httpError) Error() string { return e.detail }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	var he *httpError
	if errors.As(err, &he) {
		writeJSON(w, he.status, map[string]string{"detail": he.detail})
		return
	}
	var ce *agora.ConfigError
	if errors.As(err, &ce) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"detail": ce.Error()})
		return
	}
	var ae *agora.APIError
	if errors.As(err, &ae) {
		writeJSON(w, http.StatusBadGateway, map[string]string{"detail": ae.Error()})
		return
	}
	if errors.Is(err, presence.ErrFull) {
		writeJSON(w, http.StatusConflict, map[string]string{"detail": "Room is full"})
		return
	}
	if errors.Is(err, ratelimit.ErrLimited) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"detail": "Rate limit exceeded"})
		return
	}
	if errors.Is(err, store.ErrNotFound) {
		writeJSON(w, http.StatusNotFound, map[string]string{"detail": "Not found"})
		return
	}
	log.Printf("request failed: %v", err)
	writeJSON(w, http.StatusInternalServerError, map[string]string{"detail": "Internal server error"})
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		return &httpError{status: http.StatusUnprocessableEntity, detail: "Invalid request body"}
	}
	return nil
}
