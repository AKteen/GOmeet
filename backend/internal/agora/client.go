// Package agora calls Agora Chat and speech-to-text, and mints RTC and Chat tokens
// with the official Dynamic Key builders.
package agora

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"math"
	"net/http"
	"strings"
	"time"

	chattoken "github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src/chatTokenBuilder"
	rtctokenbuilder2 "github.com/AgoraIO/Tools/DynamicKey/AgoraDynamicKey/go/src/rtctokenbuilder2"

	"gomeet/internal/config"
)

const (
	tokenTTLSeconds        = 3600
	screenUIDOffset        = 1_000_000_000
	STTBotUID       uint32 = 2_100_000_002
)

// ConfigError means required Agora credentials are missing.
type ConfigError struct{ Msg string }

func (e *ConfigError) Error() string { return e.Msg }

// APIError means an Agora REST call failed.
type APIError struct{ Msg string }

func (e *APIError) Error() string { return e.Msg }

// Client mints tokens and calls Agora REST APIs.
type Client struct {
	cfg  config.Settings
	http *http.Client
}

func New(cfg config.Settings, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 15 * time.Second}
	} else if httpClient.Timeout == 0 {
		httpClient.Timeout = 15 * time.Second
	}
	return &Client{cfg: cfg, http: httpClient}
}

func (c *Client) AppID() string { return c.cfg.AgoraAppID }

func (c *Client) requireRTC() (string, string, error) {
	if c.cfg.AgoraAppID == "" || c.cfg.AgoraAppCertificate == "" {
		return "", "", &ConfigError{Msg: "AGORA_APP_ID and AGORA_APP_CERTIFICATE are required"}
	}
	return c.cfg.AgoraAppID, c.cfg.AgoraAppCertificate, nil
}

func (c *Client) requireChatREST() (appID, cert, host string, err error) {
	appID, cert, err = c.requireRTC()
	if err != nil {
		return "", "", "", err
	}
	host = strings.TrimRight(c.cfg.AgoraChatRestHost, "/")
	if host == "" {
		return "", "", "", &ConfigError{Msg: "AGORA_CHAT_REST_HOST is required for Chat REST"}
	}
	return appID, cert, host, nil
}

// ScreenUID is the RTC uid used for a user's screen-share track.
func (c *Client) ScreenUID(agoraUID int) (uint32, error) {
	if agoraUID <= 0 {
		return 0, &ConfigError{Msg: "screen uid is outside Agora's 32-bit range"}
	}
	sum := int64(agoraUID) + screenUIDOffset
	if sum > int64(math.MaxUint32) {
		return 0, &ConfigError{Msg: "screen uid is outside Agora's 32-bit range"}
	}
	return uint32(sum), nil
}

// BuildRTCToken builds a publisher token. Expire values are seconds from now.
func (c *Client) BuildRTCToken(channel string, uid uint32) (string, error) {
	appID, cert, err := c.requireRTC()
	if err != nil {
		return "", err
	}
	return rtctokenbuilder2.BuildTokenWithUid(
		appID,
		cert,
		channel,
		uid,
		rtctokenbuilder2.RolePublisher,
		tokenTTLSeconds,
		tokenTTLSeconds,
	)
}

// BuildChatUserToken builds a Chat user token.
func (c *Client) BuildChatUserToken(chatUserID string) (string, error) {
	appID, cert, err := c.requireRTC()
	if err != nil {
		return "", err
	}
	return chattoken.BuildChatUserToken(appID, cert, chatUserID, tokenTTLSeconds)
}

func (c *Client) buildChatAppToken() (string, error) {
	appID, cert, err := c.requireRTC()
	if err != nil {
		return "", err
	}
	return chattoken.BuildChatAppToken(appID, cert, tokenTTLSeconds)
}

func restBase(host, appID string) string {
	return host + "/app-id/" + appID
}

// RegisterChatUser creates the Agora Chat user. An already-existing user is not an error.
func (c *Client) RegisterChatUser(ctx context.Context, username string) error {
	appID, _, host, err := c.requireChatREST()
	if err != nil {
		return err
	}
	token, err := c.buildChatAppToken()
	if err != nil {
		return err
	}
	status, body, err := c.postJSON(ctx, restBase(host, appID)+"/users", token, "", "", map[string]string{
		"username": username,
	})
	if err != nil {
		return err
	}
	if status == http.StatusOK {
		return nil
	}
	low := strings.ToLower(body)
	if (status == http.StatusBadRequest || status == http.StatusConflict) && strings.Contains(low, "already") {
		log.Printf("Agora chat user already exists: %s", username)
		return nil
	}
	if status == http.StatusBadRequest && (strings.Contains(low, "duplicate") || strings.Contains(low, "exist")) {
		return nil
	}
	return &APIError{Msg: fmt.Sprintf("register_chat_user failed (%d): %s", status, body)}
}

// CreateChatRoom creates an Agora Chat room and returns its id.
func (c *Client) CreateChatRoom(ctx context.Context, name, description, owner string, maxUsers int) (string, error) {
	appID, _, host, err := c.requireChatREST()
	if err != nil {
		return "", err
	}
	token, err := c.buildChatAppToken()
	if err != nil {
		return "", err
	}
	status, body, err := c.postJSON(ctx, restBase(host, appID)+"/chatrooms", token, "", "", map[string]any{
		"name":        clip(name, 128),
		"description": clip(description, 512),
		"maxusers":    maxUsers,
		"owner":       owner,
	})
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", &APIError{Msg: fmt.Sprintf("create_chat_room failed (%d): %s", status, body)}
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", &APIError{Msg: fmt.Sprintf("create_chat_room missing id in response: %s", body)}
	}
	data, _ := payload["data"].(map[string]any)
	id := ""
	if data != nil && data["id"] != nil {
		id = strings.TrimSpace(fmt.Sprint(data["id"]))
	}
	if id == "" || id == "<nil>" {
		return "", &APIError{Msg: fmt.Sprintf("create_chat_room missing id in response: %s", body)}
	}
	return id, nil
}

// StartSTTAgent starts Real-Time STT. An empty id means STT is not configured or Agora rejected it.
func (c *Client) StartSTTAgent(ctx context.Context, channel, taskName string) (string, error) {
	if c.cfg.AgoraAppID == "" || c.cfg.AgoraCustomerID == "" || c.cfg.AgoraCustomerSecret == "" {
		return "", nil
	}
	token, err := c.BuildRTCToken(channel, STTBotUID)
	if err != nil {
		log.Printf("start_stt_agent failed: %v", err)
		return "", nil
	}
	url := fmt.Sprintf("https://api.agora.io/api/speech-to-text/v1/projects/%s/join", c.cfg.AgoraAppID)
	status, body, err := c.postJSON(ctx, url, "", c.cfg.AgoraCustomerID, c.cfg.AgoraCustomerSecret, map[string]any{
		"languages":   []string{"en-US"},
		"name":        clip(taskName, 64),
		"maxIdleTime": 60,
		"rtcConfig": map[string]any{
			"channelName":        channel,
			"pubBotUid":          fmt.Sprint(STTBotUID),
			"pubBotToken":        token,
			"enableJsonProtocol": true,
		},
	})
	if err != nil {
		log.Printf("start_stt_agent failed: %v", err)
		return "", nil
	}
	if status >= 400 {
		log.Printf("start_stt_agent failed (%d): %s", status, body)
		return "", nil
	}
	var payload map[string]any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		log.Printf("start_stt_agent failed: %v", err)
		return "", nil
	}
	if payload["agent_id"] == nil {
		return "", nil
	}
	id := strings.TrimSpace(fmt.Sprint(payload["agent_id"]))
	if id == "" || id == "<nil>" {
		return "", nil
	}
	return id, nil
}

// StopSTTAgent asks the speech-to-text agent to leave. Failures are logged.
func (c *Client) StopSTTAgent(ctx context.Context, agentID string) {
	if agentID == "" || c.cfg.AgoraAppID == "" || c.cfg.AgoraCustomerID == "" || c.cfg.AgoraCustomerSecret == "" {
		return
	}
	url := fmt.Sprintf("https://api.agora.io/api/speech-to-text/v1/projects/%s/agents/%s/leave", c.cfg.AgoraAppID, agentID)
	status, body, err := c.postJSON(ctx, url, "", c.cfg.AgoraCustomerID, c.cfg.AgoraCustomerSecret, nil)
	if err != nil {
		log.Printf("stop_stt_agent failed: %v", err)
		return
	}
	if status >= 400 {
		log.Printf("stop_stt_agent failed (%d): %s", status, body)
	}
}

func (c *Client) postJSON(ctx context.Context, url, bearer, basicUser, basicPass string, payload any) (int, string, error) {
	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return 0, "", err
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, body)
	if err != nil {
		return 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	if basicUser != "" {
		req.SetBasicAuth(basicUser, basicPass)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return resp.StatusCode, "", err
	}
	return resp.StatusCode, string(b), nil
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
