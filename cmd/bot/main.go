package main

import (
	"fmt"
	"log"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"openai_telegram_bot/internal/config"
	"openai_telegram_bot/internal/handlers"
	"openai_telegram_bot/internal/memory"
	openaiwrap "openai_telegram_bot/internal/openai"
	"openai_telegram_bot/internal/telegram"
)

type updateHandler interface {
	HandleUpdate(update tgbotapi.Update)
}

var (
	loadConfigFn  = config.Load
	newTelegramFn = telegram.New
	newOpenAIFn   = openaiwrap.New
	newStoreFn    = memory.NewStore

	newHandlerFn = func(bot *telegram.Client, oa *openaiwrap.Client, store *memory.Store, cfg config.Config) updateHandler {
		return &handlers.Handler{
			TG:             bot,
			OA:             oa,
			Store:          store,
			MaxHistory:     cfg.MaxHistoryMsgs,
			RequestTimeout: cfg.RequestTimeout,
		}
	}
	botUsernameFn = func(bot *telegram.Client) string {
		return bot.Bot().Self.UserName
	}
	updatesFn = func(bot *telegram.Client) tgbotapi.UpdatesChannel {
		return bot.Bot().GetUpdatesChan(telegram.LongPollingConfig())
	}
)

func main() {
	if err := run(); err != nil {
		log.Fatalf("startup error: %v", err)
	}
}

func run() error {
	cfg, err := loadConfigFn()
	if err != nil {
		return fmt.Errorf("config error: %w", err)
	}

	bot, err := newTelegramFn(cfg.TelegramToken, cfg.AllowedTGIDs, cfg.MaxFileMB)
	if err != nil {
		return fmt.Errorf("telegram error: %w", err)
	}

	oa := newOpenAIFn(cfg.OpenAIAPIKey, cfg.OpenAIModel, cfg.ImageModel, cfg.RouterModel, cfg.TranscribeModel, cfg.SystemPrompt)
	store := newStoreFn()
	h := newHandlerFn(bot, oa, store, cfg)

	log.Printf("bot started as @%s", botUsernameFn(bot))

	updates := updatesFn(bot)
	for update := range updates {
		h.HandleUpdate(update)
	}

	return nil
}
