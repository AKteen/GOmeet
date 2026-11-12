package agora

import (
	"strings"
	"testing"

	"gomeet/internal/config"
)

func TestRTCAndChatTokensAreNonempty(t *testing.T) {
	c := New(config.Settings{
		AgoraAppID:          "a1b2c3d4e5f6478980a1b2c3d4e5f647",
		AgoraAppCertificate: "f647e5d4c3b2a1808976f5e4d3c2b1a0",
	}, nil)
	rtc, err := c.BuildRTCToken("demo-channel", 42)
	if err != nil {
		t.Fatal(err)
	}
	chatUser, err := c.BuildChatUserToken("abc123")
	if err != nil {
		t.Fatal(err)
	}
	app, err := c.buildChatAppToken()
	if err != nil {
		t.Fatal(err)
	}
	for _, token := range []string{rtc, chatUser, app} {
		if !strings.HasPrefix(token, "007") {
			t.Fatalf("token %s", token)
		}
	}
}
