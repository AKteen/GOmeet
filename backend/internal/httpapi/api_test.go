package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"gomeet/internal/agora"
	"gomeet/internal/auth"
	"gomeet/internal/config"
	"gomeet/internal/store"
)

func testSettings() config.Settings {
	return config.Settings{
		JWTSecret:           "test-jwt-secret-phase2",
		JWTExpireMinutes:    10080,
		CORSOrigins:         "http://localhost:3000",
		AgoraAppID:          "a1b2c3d4e5f6478980a1b2c3d4e5f647",
		AgoraAppCertificate: "f647e5d4c3b2a1808976f5e4d3c2b1a0",
		AgoraChatRestHost:   "https://chat.example.com",
		RedisURL:            "redis://localhost:6379/15",
	}
}

type recorder struct {
	mu   sync.Mutex
	urls []string
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.mu.Lock()
	r.urls = append(r.urls, req.URL.String())
	r.mu.Unlock()
	body := `{}`
	path := req.URL.Path
	if strings.Contains(path, "/chatrooms") {
		body = `{"data":{"id":"mock-chat-room-id"}}`
	}
	if strings.Contains(path, "/speech-to-text/") && strings.HasSuffix(path, "/join") {
		body = `{"agent_id":"agent-1","status":"RUNNING"}`
	}
	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(strings.NewReader(body)),
		Header:     make(http.Header),
		Request:    req,
	}, nil
}

func (r *recorder) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.urls))
	copy(out, r.urls)
	return out
}

func countSuffix(urls []string, suffix string) int {
	n := 0
	for _, u := range urls {
		if strings.HasSuffix(u, suffix) {
			n++
		}
	}
	return n
}

func newAPI(t *testing.T, mutate func(*config.Settings)) (*httptest.Server, *recorder) {
	t.Helper()
	cfg := testSettings()
	if mutate != nil {
		mutate(&cfg)
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	rec := &recorder{}
	ag := agora.New(cfg, &http.Client{Transport: rec})
	srv := httptest.NewServer(New(cfg, store.NewMemory(), rdb, ag))
	t.Cleanup(srv.Close)
	return srv, rec
}

func doJSON(t *testing.T, method, url, token string, body any) *http.Response {
	t.Helper()
	var buf io.Reader
	if body != nil {
		raw, err := json.Marshal(body)
		if err != nil {
			t.Fatal(err)
		}
		buf = bytes.NewReader(raw)
	}
	req, err := http.NewRequest(method, url, buf)
	if err != nil {
		t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func readJSON(t *testing.T, res *http.Response) map[string]any {
	t.Helper()
	defer res.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func readArray(t *testing.T, res *http.Response) []map[string]any {
	t.Helper()
	defer res.Body.Close()
	var out []map[string]any
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	return out
}

func signup(t *testing.T, base, email, name string) string {
	t.Helper()
	res := doJSON(t, http.MethodPost, base+"/auth/signup", "", map[string]string{
		"email": email, "password": "password123", "name": name,
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("signup %s: %d", email, res.StatusCode)
	}
	body := readJSON(t, res)
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("missing access token")
	}
	return token
}

func TestClientIPPrefersProxyHeaders(t *testing.T) {
	req, err := http.NewRequest(http.MethodGet, "/", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.RemoteAddr = "10.0.0.1:4000"
	req.Header.Set("X-Forwarded-For", "1.2.3.4, 10.0.0.8")
	if clientIP(req) != "10.0.0.8" {
		t.Fatalf("forwarded %s", clientIP(req))
	}
	req.Header.Set("X-Real-IP", "203.0.113.9")
	if clientIP(req) != "203.0.113.9" {
		t.Fatalf("real %s", clientIP(req))
	}
}

func TestSignupReturnsToken(t *testing.T) {
	srv, _ := newAPI(t, nil)
	res := doJSON(t, http.MethodPost, srv.URL+"/auth/signup", "", map[string]string{
		"email": "alice@example.com", "password": "password123", "name": "Alice",
	})
	if res.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if body["token_type"] != "bearer" {
		t.Fatalf("token_type %v", body["token_type"])
	}
	token, _ := body["access_token"].(string)
	if token == "" {
		t.Fatal("empty access token")
	}
}

func TestLoginSuccess(t *testing.T) {
	srv, _ := newAPI(t, nil)
	signup(t, srv.URL, "bob@example.com", "Bob")
	res := doJSON(t, http.MethodPost, srv.URL+"/auth/login", "", map[string]string{
		"email": "bob@example.com", "password": "password123",
	})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	if readJSON(t, res)["access_token"] == "" {
		t.Fatal("empty access token")
	}
}

func TestLoginWrongPassword(t *testing.T) {
	srv, _ := newAPI(t, nil)
	signup(t, srv.URL, "carol@example.com", "Carol")
	res := doJSON(t, http.MethodPost, srv.URL+"/auth/login", "", map[string]string{
		"email": "carol@example.com", "password": "wrong-password",
	})
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestMeWithValidToken(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "dave@example.com", "Dave")
	res := doJSON(t, http.MethodGet, srv.URL+"/auth/me", token, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d", res.StatusCode)
	}
	body := readJSON(t, res)
	if body["email"] != "dave@example.com" || body["name"] != "Dave" {
		t.Fatalf("user %+v", body)
	}
	if _, ok := body["agora_uid"].(float64); !ok {
		t.Fatalf("agora_uid %T", body["agora_uid"])
	}
}

func TestExpiredToken(t *testing.T) {
	srv, _ := newAPI(t, nil)
	delta := -10 * time.Second
	token, err := auth.CreateAccessToken(testSettings().JWTSecret, uuid.NewString(), 10080, &delta)
	if err != nil {
		t.Fatal(err)
	}
	res := doJSON(t, http.MethodGet, srv.URL+"/auth/me", token, nil)
	defer res.Body.Close()
	if res.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status %d", res.StatusCode)
	}
}

func TestCreateAndListRooms(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "host@example.com", "Host")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", token, map[string]any{
		"title": "Standup", "max_participants": 5,
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("status %d", created.StatusCode)
	}
	body := readJSON(t, created)
	if body["title"] != "Standup" || body["chat_room_id"] != "mock-chat-room-id" {
		t.Fatalf("room %+v", body)
	}
	share, _ := body["share_path"].(string)
	if !strings.HasPrefix(share, "/room/") {
		t.Fatalf("share_path %s", share)
	}
	mine := doJSON(t, http.MethodGet, srv.URL+"/rooms/mine", token, nil)
	if mine.StatusCode != http.StatusOK {
		t.Fatalf("mine status %d", mine.StatusCode)
	}
	if rooms := readArray(t, mine); len(rooms) != 1 {
		t.Fatalf("rooms %d", len(rooms))
	}
}

func TestJoinReturnsAgoraTokens(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "joiner@example.com", "Host")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", token, map[string]string{"title": "Call"})
	room := readJSON(t, created)
	slug, _ := room["slug"].(string)
	joined := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", token, map[string]any{})
	if joined.StatusCode != http.StatusOK {
		t.Fatalf("status %d", joined.StatusCode)
	}
	payload := readJSON(t, joined)
	if payload["channel"] != slug || payload["app_id"] == "" || payload["rtc_token"] == "" || payload["chat_token"] == "" || payload["chat_user"] == "" {
		t.Fatalf("payload %+v", payload)
	}
	if payload["chat_room_id"] != "mock-chat-room-id" {
		t.Fatalf("chat room %v", payload["chat_room_id"])
	}
	uid, ok := payload["uid"].(float64)
	if !ok {
		t.Fatalf("uid %T", payload["uid"])
	}
	screen, ok := payload["screen_uid"].(float64)
	if !ok || int(screen) != int(uid)+1_000_000_000 {
		t.Fatalf("screen uid %v uid %v", payload["screen_uid"], payload["uid"])
	}
	if payload["screen_rtc_token"] == "" {
		t.Fatal("empty screen token")
	}
	if payload["stt_bot_uid"] != nil {
		t.Fatalf("stt %v", payload["stt_bot_uid"])
	}
}

func TestSTTStartsOnceAndStopsWhenEmpty(t *testing.T) {
	srv, rec := newAPI(t, func(cfg *config.Settings) {
		cfg.AgoraCustomerID = "customer"
		cfg.AgoraCustomerSecret = "secret"
	})
	host := signup(t, srv.URL, "stt-host@example.com", "Host")
	guest := signup(t, srv.URL, "stt-guest@example.com", "Guest")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", host, map[string]string{"title": "Transcribe"})
	slug, _ := readJSON(t, created)["slug"].(string)

	first := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", host, map[string]any{})
	if first.StatusCode != http.StatusOK {
		t.Fatalf("first join %d", first.StatusCode)
	}
	if readJSON(t, first)["stt_bot_uid"] != float64(2_100_000_002) {
		t.Fatal("missing stt bot")
	}
	if countSuffix(rec.snapshot(), "/join") != 1 {
		t.Fatalf("joins after first: %v", rec.snapshot())
	}

	second := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", guest, map[string]any{})
	if second.StatusCode != http.StatusOK {
		t.Fatalf("second join %d", second.StatusCode)
	}
	if readJSON(t, second)["stt_bot_uid"] != float64(2_100_000_002) {
		t.Fatal("guest missing stt bot")
	}
	if countSuffix(rec.snapshot(), "/join") != 1 {
		t.Fatalf("joins after second: %v", rec.snapshot())
	}

	left := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/leave", host, map[string]any{})
	defer left.Body.Close()
	if left.StatusCode != http.StatusOK {
		t.Fatalf("host leave %d", left.StatusCode)
	}
	if countSuffix(rec.snapshot(), "/leave") != 0 {
		t.Fatalf("leave too early: %v", rec.snapshot())
	}

	last := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/leave", guest, map[string]any{})
	defer last.Body.Close()
	if last.StatusCode != http.StatusOK {
		t.Fatalf("guest leave %d", last.StatusCode)
	}
	if countSuffix(rec.snapshot(), "/leave") != 1 {
		t.Fatalf("leave calls: %v", rec.snapshot())
	}
}

func TestTranscriptLineIsSavedOnce(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "scribe@example.com", "Host")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", token, map[string]string{"title": "Notes"})
	slug, _ := readJSON(t, created)["slug"].(string)
	joined := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", token, map[string]any{})
	uid := readJSON(t, joined)["uid"]
	body := map[string]any{"sentence_id": 7, "speaker_uid": uid, "text": "Hello there"}
	first := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/transcript", token, body)
	second := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/transcript", token, body)
	defer first.Body.Close()
	defer second.Body.Close()
	if first.StatusCode != http.StatusNoContent || second.StatusCode != http.StatusNoContent {
		t.Fatalf("transcript %d %d", first.StatusCode, second.StatusCode)
	}
	sessions := doJSON(t, http.MethodGet, srv.URL+"/rooms/"+slug+"/sessions", token, nil)
	if sessions.StatusCode != http.StatusOK {
		t.Fatalf("sessions %d", sessions.StatusCode)
	}
	rows := readArray(t, sessions)
	if len(rows) != 1 || rows[0]["transcript"] != "Host: Hello there" {
		t.Fatalf("sessions %+v", rows)
	}
}

func TestGetRoomBySlug(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "viewer@example.com", "Host")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", token, map[string]string{"title": "Public"})
	slug, _ := readJSON(t, created)["slug"].(string)
	got := doJSON(t, http.MethodGet, srv.URL+"/rooms/"+slug, "", nil)
	if got.StatusCode != http.StatusOK {
		t.Fatalf("status %d", got.StatusCode)
	}
	if readJSON(t, got)["slug"] != slug {
		t.Fatal("slug mismatch")
	}
}

func TestJoinOverLimitReturns409(t *testing.T) {
	srv, _ := newAPI(t, nil)
	host := signup(t, srv.URL, "host-limit@example.com", "host-limit")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", host, map[string]any{
		"title": "Tiny", "max_participants": 2,
	})
	if created.StatusCode != http.StatusCreated {
		t.Fatalf("create %d", created.StatusCode)
	}
	slug, _ := readJSON(t, created)["slug"].(string)
	if res := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", host, map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("host join %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	u2 := signup(t, srv.URL, "u2-limit@example.com", "u2-limit")
	if res := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", u2, map[string]any{}); res.StatusCode != http.StatusOK {
		t.Fatalf("u2 join %d", res.StatusCode)
	} else {
		res.Body.Close()
	}
	u3 := signup(t, srv.URL, "u3-limit@example.com", "u3-limit")
	over := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", u3, map[string]any{})
	body := readJSON(t, over)
	if over.StatusCode != http.StatusConflict {
		t.Fatalf("status %d %+v", over.StatusCode, body)
	}
	detail, _ := body["detail"].(string)
	if !strings.Contains(strings.ToLower(detail), "full") {
		t.Fatalf("detail %s", detail)
	}
}

func TestJoinRateLimitReturns429(t *testing.T) {
	srv, _ := newAPI(t, nil)
	token := signup(t, srv.URL, "rate@example.com", "rate")
	created := doJSON(t, http.MethodPost, srv.URL+"/rooms", token, map[string]any{
		"title": "RL", "max_participants": 50,
	})
	slug, _ := readJSON(t, created)["slug"].(string)
	last := http.StatusOK
	for i := 0; i < 25; i++ {
		res := doJSON(t, http.MethodPost, srv.URL+"/rooms/"+slug+"/join", token, map[string]any{})
		last = res.StatusCode
		res.Body.Close()
		if last == http.StatusTooManyRequests {
			break
		}
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("last status %d", last)
	}
}
