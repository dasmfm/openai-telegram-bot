package telegram

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestSanitizeHTML_ClosesOpenTags(t *testing.T) {
	in := "<b>Привет, <i>мир"
	out := sanitizeHTML(in)
	if out != "<b>Привет, <i>мир</i></b>" {
		t.Fatalf("unexpected sanitized html: %q", out)
	}
}

func TestSanitizeHTML_DropsMismatchedClosingTags(t *testing.T) {
	in := "<b>hello</i>"
	out := sanitizeHTML(in)
	if out != "<b>hello</b>" {
		t.Fatalf("unexpected sanitized html for mismatched close: %q", out)
	}
}

func TestSanitizeHTML_PreservesUnknownTagsAsText(t *testing.T) {
	in := "<span>hello</span> <code>x</code>"
	out := sanitizeHTML(in)
	if out != "<span>hello</span> <code>x</code>" {
		t.Fatalf("unexpected sanitized html with unknown tags: %q", out)
	}
}

func TestSplitTelegramMessage(t *testing.T) {
	long := strings.Repeat("a", 3900) + " " + strings.Repeat("b", 3900)
	parts := SplitTelegramMessage(long)
	if len(parts) < 2 {
		t.Fatalf("expected split into 2+ parts, got %d", len(parts))
	}
	for i, part := range parts {
		if len(part) > 4000 {
			t.Fatalf("part %d too long: %d", i, len(part))
		}
	}
}

func TestSplitTelegramMessage_Empty(t *testing.T) {
	parts := SplitTelegramMessage("   ")
	if len(parts) != 1 || parts[0] != "" {
		t.Fatalf("unexpected split result for empty input: %#v", parts)
	}
}

func TestImageDataURL(t *testing.T) {
	data := []byte("img")
	got := ImageDataURL("image/png", data)
	want := "data:image/png;base64," + base64.StdEncoding.EncodeToString(data)
	if got != want {
		t.Fatalf("ImageDataURL mismatch: got %q want %q", got, want)
	}
}

func TestImageDataURL_DefaultContentType(t *testing.T) {
	got := ImageDataURL("", []byte("x"))
	if !strings.HasPrefix(got, "data:image/jpeg;base64,") {
		t.Fatalf("expected jpeg default prefix, got %q", got)
	}
}

func TestGuessImageContentType(t *testing.T) {
	cases := map[string]string{
		"a.png":  "image/png",
		"a.WEBP": "image/webp",
		"a.gif":  "image/gif",
		"a.jpg":  "image/jpeg",
		"a.jpeg": "image/jpeg",
		"a.bin":  "image/jpeg",
	}
	for name, want := range cases {
		if got := GuessImageContentType(name); got != want {
			t.Fatalf("GuessImageContentType(%q)=%q want %q", name, got, want)
		}
	}
}

func TestIsAllowed(t *testing.T) {
	c := &Client{allowedIDs: map[int64]bool{42: true}}
	if !c.IsAllowed(42) {
		t.Fatal("expected user 42 to be allowed")
	}
	if c.IsAllowed(7) {
		t.Fatal("expected user 7 to be blocked")
	}

	c = &Client{allowedIDs: map[int64]bool{}}
	if c.IsAllowed(42) {
		t.Fatal("expected empty allowlist to block users")
	}
}

func TestLongPollingConfig(t *testing.T) {
	cfg := LongPollingConfig()
	if cfg.Timeout != 60 {
		t.Fatalf("unexpected timeout: %d", cfg.Timeout)
	}
}

func TestNew(t *testing.T) {
	orig := newBotAPIFn
	defer func() { newBotAPIFn = orig }()

	newBotAPIFn = func(token string) (*tgbotapi.BotAPI, error) {
		if token != "token" {
			t.Fatalf("unexpected token: %q", token)
		}
		return &tgbotapi.BotAPI{}, nil
	}

	c, err := New("token", map[int64]bool{1: true}, 0)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	if c.maxFileBytes != 20*1024*1024 {
		t.Fatalf("expected default maxFileBytes, got %d", c.maxFileBytes)
	}

	newBotAPIFn = func(token string) (*tgbotapi.BotAPI, error) {
		return nil, errors.New("init error")
	}
	if _, err := New("token", nil, 10); err == nil {
		t.Fatal("expected error from New when bot init fails")
	}
}
