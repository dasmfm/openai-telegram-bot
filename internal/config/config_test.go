package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsAndRequiredEnv(t *testing.T) {
	t.Setenv("TELEGRAM_TOKEN", "tg-token")
	t.Setenv("OPENAI_API_KEY", "oa-key")
	t.Setenv("ALLOWED_TG_IDS", "1, 2")
	t.Setenv("OPENAI_MODEL", "")
	t.Setenv("IMAGE_MODEL", "")
	t.Setenv("ROUTER_MODEL", "")
	t.Setenv("SYSTEM_PROMPT", "")
	t.Setenv("MAX_HISTORY_MSGS", "")
	t.Setenv("MAX_FILE_MB", "")
	t.Setenv("REQUEST_TIMEOUT_SEC", "")
	t.Setenv("TRANSCRIBE_MODEL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}

	if cfg.OpenAIModel != "gpt-5.2" || cfg.ImageModel != "gpt-image-1-mini" || cfg.RouterModel != "gpt-5-mini" {
		t.Fatalf("unexpected default models: %+v", cfg)
	}
	if cfg.TranscribeModel != "whisper-1" {
		t.Fatalf("unexpected default transcribe model: %q", cfg.TranscribeModel)
	}
	if cfg.MaxHistoryMsgs != 20 || cfg.MaxFileMB != 20 {
		t.Fatalf("unexpected numeric defaults: history=%d fileMB=%d", cfg.MaxHistoryMsgs, cfg.MaxFileMB)
	}
	if cfg.RequestTimeout != 120*time.Second {
		t.Fatalf("unexpected request timeout: %v", cfg.RequestTimeout)
	}
	if !cfg.AllowedTGIDs[1] || !cfg.AllowedTGIDs[2] {
		t.Fatalf("expected allowed ids 1 and 2, got %#v", cfg.AllowedTGIDs)
	}
	if !strings.HasPrefix(cfg.SystemPrompt, "Reply in Telegram HTML only.") {
		t.Fatalf("base system prompt missing: %q", cfg.SystemPrompt)
	}
}

func TestLoadAppendsCustomSystemPrompt(t *testing.T) {
	t.Setenv("TELEGRAM_TOKEN", "tg-token")
	t.Setenv("OPENAI_API_KEY", "oa-key")
	t.Setenv("ALLOWED_TG_IDS", "42")
	t.Setenv("SYSTEM_PROMPT", "Будь краткой")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !strings.Contains(cfg.SystemPrompt, "Будь краткой") {
		t.Fatalf("custom prompt not appended: %q", cfg.SystemPrompt)
	}
}

func TestLoadMissingRequiredEnv(t *testing.T) {
	t.Setenv("TELEGRAM_TOKEN", "")
	t.Setenv("OPENAI_API_KEY", "")
	t.Setenv("ALLOWED_TG_IDS", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected missing required env error")
	}
	msg := err.Error()
	for _, key := range []string{"TELEGRAM_TOKEN", "OPENAI_API_KEY", "ALLOWED_TG_IDS"} {
		if !strings.Contains(msg, key) {
			t.Fatalf("missing key %q in error: %v", key, err)
		}
	}
}

func TestLoadInvalidAllowedIDs(t *testing.T) {
	t.Setenv("TELEGRAM_TOKEN", "tg-token")
	t.Setenv("OPENAI_API_KEY", "oa-key")
	t.Setenv("ALLOWED_TG_IDS", "abc")

	_, err := Load()
	if err == nil {
		t.Fatal("expected parse error for ALLOWED_TG_IDS")
	}
	if !strings.Contains(err.Error(), "parse ALLOWED_TG_IDS") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestParseIDSetAndGetenvInt(t *testing.T) {
	ids, err := parseIDSet("10, 20,10")
	if err != nil {
		t.Fatalf("parseIDSet returned error: %v", err)
	}
	if len(ids) != 2 || !ids[10] || !ids[20] {
		t.Fatalf("unexpected ids map: %#v", ids)
	}

	if _, err := parseIDSet("x"); err == nil {
		t.Fatal("expected parseIDSet to fail on invalid id")
	}

	t.Setenv("INT_TEST", "77")
	if got := getenvInt("INT_TEST", 3); got != 77 {
		t.Fatalf("expected getenvInt value 77, got %d", got)
	}
	t.Setenv("INT_TEST", "bad")
	if got := getenvInt("INT_TEST", 3); got != 3 {
		t.Fatalf("expected fallback 3, got %d", got)
	}
}
