package main

import (
	"errors"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"openai_telegram_bot/internal/config"
	"openai_telegram_bot/internal/memory"
	openaiwrap "openai_telegram_bot/internal/openai"
	"openai_telegram_bot/internal/telegram"
)

type fakeHandler struct {
	updates []tgbotapi.Update
}

func (f *fakeHandler) HandleUpdate(update tgbotapi.Update) {
	f.updates = append(f.updates, update)
}

func TestRun_WiresDependenciesAndProcessesUpdates(t *testing.T) {
	origLoad := loadConfigFn
	origTG := newTelegramFn
	origOA := newOpenAIFn
	origStore := newStoreFn
	origHandler := newHandlerFn
	origUser := botUsernameFn
	origUpdates := updatesFn
	defer func() {
		loadConfigFn = origLoad
		newTelegramFn = origTG
		newOpenAIFn = origOA
		newStoreFn = origStore
		newHandlerFn = origHandler
		botUsernameFn = origUser
		updatesFn = origUpdates
	}()

	cfg := config.Config{
		TelegramToken:  "token",
		OpenAIAPIKey:   "key",
		OpenAIModel:    "gpt-5.2",
		ImageModel:     "gpt-image-1-mini",
		RouterModel:    "gpt-5-mini",
		SystemPrompt:   "prompt",
		AllowedTGIDs:   map[int64]bool{1: true},
		MaxHistoryMsgs: 11,
		MaxFileMB:      22,
		RequestTimeout: 33 * time.Second,
	}

	var gotToken string
	var gotAllowed map[int64]bool
	var gotMaxMB int64

	loadConfigFn = func() (config.Config, error) { return cfg, nil }
	newTelegramFn = func(token string, allowedIDs map[int64]bool, maxFileMB int64) (*telegram.Client, error) {
		gotToken = token
		gotAllowed = allowedIDs
		gotMaxMB = maxFileMB
		return &telegram.Client{}, nil
	}
	newOpenAIFn = func(apiKey string, model string, imageModel string, routerModel string, transcribeModel string, systemPrompt string) *openaiwrap.Client {
		return &openaiwrap.Client{}
	}
	newStoreFn = func() *memory.Store { return memory.NewStore() }

	fh := &fakeHandler{}
	newHandlerFn = func(bot *telegram.Client, oa *openaiwrap.Client, store *memory.Store, gotCfg config.Config) updateHandler {
		if gotCfg.MaxHistoryMsgs != cfg.MaxHistoryMsgs || gotCfg.RequestTimeout != cfg.RequestTimeout {
			t.Fatalf("handler got unexpected cfg: %+v", gotCfg)
		}
		if bot == nil || oa == nil || store == nil {
			t.Fatal("handler dependencies must be non-nil")
		}
		return fh
	}
	botUsernameFn = func(bot *telegram.Client) string { return "test_bot" }
	updatesFn = func(bot *telegram.Client) tgbotapi.UpdatesChannel {
		ch := make(chan tgbotapi.Update, 2)
		ch <- tgbotapi.Update{UpdateID: 1}
		close(ch)
		return ch
	}

	if err := run(); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if gotToken != cfg.TelegramToken || gotMaxMB != cfg.MaxFileMB {
		t.Fatalf("telegram.New got wrong args: token=%q maxMB=%d", gotToken, gotMaxMB)
	}
	if !gotAllowed[1] {
		t.Fatalf("allowed ids not propagated: %#v", gotAllowed)
	}
	if len(fh.updates) != 1 || fh.updates[0].UpdateID != 1 {
		t.Fatalf("unexpected handled updates: %#v", fh.updates)
	}
}

func TestRun_ConfigError(t *testing.T) {
	origLoad := loadConfigFn
	defer func() { loadConfigFn = origLoad }()

	loadConfigFn = func() (config.Config, error) {
		return config.Config{}, errors.New("bad env")
	}

	err := run()
	if err == nil || err.Error() != "config error: bad env" {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRun_TelegramInitError(t *testing.T) {
	origLoad := loadConfigFn
	origTG := newTelegramFn
	defer func() {
		loadConfigFn = origLoad
		newTelegramFn = origTG
	}()

	loadConfigFn = func() (config.Config, error) {
		return config.Config{
			TelegramToken: "token",
			AllowedTGIDs:  map[int64]bool{1: true},
			MaxFileMB:     20,
		}, nil
	}
	newTelegramFn = func(token string, allowedIDs map[int64]bool, maxFileMB int64) (*telegram.Client, error) {
		return nil, errors.New("tg down")
	}

	err := run()
	if err == nil || err.Error() != "telegram error: tg down" {
		t.Fatalf("unexpected error: %v", err)
	}
}
