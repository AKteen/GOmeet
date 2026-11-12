package httpapi

import (
	"crypto/rand"
	"errors"
	"math/big"
	"net"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"

	"gomeet/internal/agora"
	"gomeet/internal/auth"
	"gomeet/internal/ratelimit"
	"gomeet/internal/store"
)

type signupRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type tokenResponse struct {
	AccessToken string `json:"access_token"`
	TokenType   string `json:"token_type"`
}

type userOut struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	AgoraUID  int       `json:"agora_uid"`
	CreatedAt time.Time `json:"created_at"`
}

func (a *app) signup(w http.ResponseWriter, r *http.Request) {
	var body signupRequest
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	name := strings.TrimSpace(body.Name)
	if !validEmail(email) {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Invalid email"})
		return
	}
	if n := utf8.RuneCountInString(body.Password); n < 8 || n > 72 {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Password must be 8 to 72 characters"})
		return
	}
	if name == "" || utf8.RuneCountInString(name) > 120 {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Name must be 1 to 120 characters"})
		return
	}

	if _, err := a.store.UserByEmail(r.Context(), email); err == nil {
		writeErr(w, &httpError{status: http.StatusConflict, detail: "Email already registered"})
		return
	} else if !errors.Is(err, store.ErrNotFound) {
		writeErr(w, err)
		return
	}

	uid, err := a.nextAgoraUID(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		writeErr(w, err)
		return
	}
	user := store.User{
		ID:           uuid.New(),
		Email:        email,
		PasswordHash: hash,
		Name:         name,
		AgoraUID:     uid,
	}
	if err := a.store.CreateUser(r.Context(), &user); err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeErr(w, &httpError{status: http.StatusConflict, detail: "Email already registered"})
			return
		}
		writeErr(w, err)
		return
	}

	if err := a.agora.RegisterChatUser(r.Context(), user.ChatUsername()); err != nil {
		var ce *agora.ConfigError
		if !errors.As(err, &ce) {
			_ = a.store.DeleteUser(r.Context(), user.ID)
			writeErr(w, err)
			return
		}
		// Chat is optional at signup. Room creation still requires it.
	}

	token, err := auth.CreateAccessToken(a.cfg.JWTSecret, user.ID.String(), a.cfg.JWTExpireMinutes, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, tokenResponse{AccessToken: token, TokenType: "bearer"})
}

func (a *app) login(w http.ResponseWriter, r *http.Request) {
	var body loginRequest
	if err := decodeJSON(r, &body); err != nil {
		writeErr(w, err)
		return
	}
	email := strings.ToLower(strings.TrimSpace(body.Email))
	if !validEmail(email) {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Invalid email"})
		return
	}
	if n := utf8.RuneCountInString(body.Password); n < 1 || n > 72 {
		writeErr(w, &httpError{status: http.StatusUnprocessableEntity, detail: "Invalid email or password"})
		return
	}
	if err := ratelimit.Check(r.Context(), a.redis, "rl:login:"+clientIP(r), 10, time.Minute); err != nil {
		writeErr(w, err)
		return
	}
	user, err := a.store.UserByEmail(r.Context(), email)
	if err != nil || !auth.VerifyPassword(body.Password, user.PasswordHash) {
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			writeErr(w, err)
			return
		}
		writeErr(w, &httpError{status: http.StatusUnauthorized, detail: "Invalid email or password"})
		return
	}
	token, err := auth.CreateAccessToken(a.cfg.JWTSecret, user.ID.String(), a.cfg.JWTExpireMinutes, nil)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, tokenResponse{AccessToken: token, TokenType: "bearer"})
}

func (a *app) me(w http.ResponseWriter, r *http.Request) {
	user, ok := a.currentUser(w, r, false)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, userJSON(user))
}

func userJSON(u store.User) userOut {
	return userOut{
		ID:        u.ID,
		Email:     u.Email,
		Name:      u.Name,
		AgoraUID:  u.AgoraUID,
		CreatedAt: u.CreatedAt,
	}
}

func (a *app) nextAgoraUID(r *http.Request) (int, error) {
	max, err := a.store.MaxAgoraUID(r.Context())
	if err != nil {
		return 0, err
	}
	if max == nil {
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000_000))
		if err != nil {
			return 0, err
		}
		return int(n.Int64()) + 1, nil
	}
	return *max + 1, nil
}

func (a *app) currentUser(w http.ResponseWriter, r *http.Request, allowQuery bool) (store.User, bool) {
	user, err := a.userFromRequest(r, allowQuery)
	if err != nil {
		writeErr(w, err)
		return store.User{}, false
	}
	return user, true
}

func (a *app) userFromRequest(r *http.Request, allowQuery bool) (store.User, error) {
	token := bearerToken(r)
	if token == "" && allowQuery {
		token = r.URL.Query().Get("access_token")
	}
	if token == "" {
		return store.User{}, &httpError{status: http.StatusUnauthorized, detail: "Not authenticated"}
	}
	sub, err := auth.SubjectFromToken(a.cfg.JWTSecret, token)
	if err != nil {
		detail := "Invalid or expired token"
		if err.Error() == "invalid token" {
			detail = "Invalid token"
		}
		return store.User{}, &httpError{status: http.StatusUnauthorized, detail: detail}
	}
	id, err := uuid.Parse(sub)
	if err != nil {
		return store.User{}, &httpError{status: http.StatusUnauthorized, detail: "Invalid or expired token"}
	}
	user, err := a.store.UserByID(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		return store.User{}, &httpError{status: http.StatusUnauthorized, detail: "User not found"}
	}
	if err != nil {
		return store.User{}, err
	}
	return user, nil
}

func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}

func clientIP(r *http.Request) string {
	if real := strings.TrimSpace(r.Header.Get("X-Real-IP")); real != "" {
		return real
	}
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.Split(xff, ",")
		ip := strings.TrimSpace(parts[len(parts)-1])
		if ip != "" {
			return ip
		}
	}
	if r.RemoteAddr == "" {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func validEmail(s string) bool {
	if s == "" || strings.ContainsAny(s, " \t") {
		return false
	}
	at := strings.LastIndex(s, "@")
	if at < 1 || at >= len(s)-1 {
		return false
	}
	domain := s[at+1:]
	dot := strings.LastIndex(domain, ".")
	return dot > 0 && dot < len(domain)-1
}
