package mail

import (
	"strings"
	"testing"
)

func TestBuildMessageEncodesBodyAndHeaders(t *testing.T) {
	msg, err := buildMessage(Settings{FromName: "Eink Library", FromAddress: "library@example.com"}, "reader@example.com", "New books", "title\r\nBcc: attacker@example.com")
	if err != nil {
		t.Fatal(err)
	}
	text := string(msg)
	if strings.Contains(text, "\r\nBcc: attacker@example.com") {
		t.Errorf("message contains an injected header: %q", text)
	}
	if !strings.Contains(text, "Content-Transfer-Encoding: base64\r\n") {
		t.Errorf("message does not encode its body: %q", text)
	}
}

func TestBuildMessageRejectsHeaderInjection(t *testing.T) {
	base := Settings{FromName: "Eink Library", FromAddress: "library@example.com"}
	for _, tt := range []struct {
		name     string
		settings Settings
		to       string
		subject  string
	}{
		{"sender name", Settings{FromName: "Eink\r\nBcc: attacker@example.com", FromAddress: base.FromAddress}, "reader@example.com", "Hello"},
		{"sender address", Settings{FromName: base.FromName, FromAddress: "library@example.com\r\nBcc: attacker@example.com"}, "reader@example.com", "Hello"},
		{"recipient", base, "reader@example.com\r\nBcc: attacker@example.com", "Hello"},
		{"subject", base, "reader@example.com", "Hello\r\nBcc: attacker@example.com"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := buildMessage(tt.settings, tt.to, tt.subject, "body"); err == nil {
				t.Fatal("expected invalid header to be rejected")
			}
		})
	}
}
