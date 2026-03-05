package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	TelegramToken   string
	OpenAIAPIKey    string
	OpenAIModel     string
	ImageModel      string
	RouterModel     string
	SystemPrompt    string
	AllowedTGIDs    map[int64]bool
	MaxHistoryMsgs  int
	MaxFileMB       int64
	RequestTimeout  time.Duration
	TranscribeModel string
}

func Load() (Config, error) {
	var cfg Config
	var missing []string

	cfg.TelegramToken = strings.TrimSpace(os.Getenv("TELEGRAM_TOKEN"))
	if cfg.TelegramToken == "" {
		missing = append(missing, "TELEGRAM_TOKEN")
	}

	cfg.OpenAIAPIKey = strings.TrimSpace(os.Getenv("OPENAI_API_KEY"))
	if cfg.OpenAIAPIKey == "" {
		missing = append(missing, "OPENAI_API_KEY")
	}

	cfg.OpenAIModel = strings.TrimSpace(os.Getenv("OPENAI_MODEL"))
	if cfg.OpenAIModel == "" {
		cfg.OpenAIModel = "gpt-5.2"
	}

	cfg.ImageModel = strings.TrimSpace(os.Getenv("IMAGE_MODEL"))
	if cfg.ImageModel == "" {
		cfg.ImageModel = "gpt-image-1-mini"
	}

	cfg.RouterModel = strings.TrimSpace(os.Getenv("ROUTER_MODEL"))
	if cfg.RouterModel == "" {
		cfg.RouterModel = "gpt-5-mini"
	}

	basePrompt := "Reply in Telegram HTML only. Use <b>, <i>, <u>, <code>, and plain text. Do not use other tags."
	customPrompt := strings.TrimSpace(os.Getenv("SYSTEM_PROMPT"))
	if customPrompt == "" {
		cfg.SystemPrompt = basePrompt
	} else {
		cfg.SystemPrompt = basePrompt + "\n" + customPrompt
	}

	allowedStr := strings.TrimSpace(os.Getenv("ALLOWED_TG_IDS"))
	if allowedStr == "" {
		missing = append(missing, "ALLOWED_TG_IDS")
	}
	allowed, err := parseIDSet(allowedStr)
	if err != nil {
		return cfg, fmt.Errorf("parse ALLOWED_TG_IDS: %w", err)
	}
	cfg.AllowedTGIDs = allowed

	cfg.MaxHistoryMsgs = getenvInt("MAX_HISTORY_MSGS", 20)
	cfg.MaxFileMB = int64(getenvInt("MAX_FILE_MB", 20))
	cfg.RequestTimeout = time.Duration(getenvInt("REQUEST_TIMEOUT_SEC", 120)) * time.Second
	cfg.TranscribeModel = strings.TrimSpace(os.Getenv("TRANSCRIBE_MODEL"))
	if cfg.TranscribeModel == "" {
		cfg.TranscribeModel = "whisper-1"
	}

	if len(missing) > 0 {
		return cfg, errors.New("missing required env: " + strings.Join(missing, ", "))
	}

	return cfg, nil
}

func parseIDSet(csv string) (map[int64]bool, error) {
	set := make(map[int64]bool)
	if strings.TrimSpace(csv) == "" {
		return set, nil
	}
	parts := strings.Split(csv, ",")
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		id, err := strconv.ParseInt(p, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid telegram id %q", p)
		}
		set[id] = true
	}
	return set, nil
}

func getenvInt(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}
