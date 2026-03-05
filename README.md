# OpenAI Telegram Bot Proxy

Telegram bot with long polling that proxies user messages to OpenAI (gpt-5.2) and streams responses by editing a single message.

## Requirements

- Go 1.22+ (for local builds)
- Docker (for containerized run)

## Environment

Required:

- `TELEGRAM_TOKEN`
- `OPENAI_API_KEY`
- `ALLOWED_TG_IDS` (CSV of telegram user IDs)

Optional:

- `OPENAI_MODEL` (default `gpt-5.2`)
- `IMAGE_MODEL` (default `gpt-image-1-mini`)
- `ROUTER_MODEL` (default `gpt-5-mini`)
- `SYSTEM_PROMPT` (default empty)
- `MAX_HISTORY_MSGS` (default `20`)
- `STREAM_THROTTLE_MS` (default `600`)
- `MAX_FILE_MB` (default `20`)
- `REQUEST_TIMEOUT_SEC` (default `120`)
- `TRANSCRIBE_MODEL` (default `whisper-1`)

## Local run

Copy `.env.example` to `.env` and fill in your values.

```bash
go run ./cmd/bot
```

## Docker

Build:

```bash
docker build -t openai-telegram-bot .
```

Run:

```bash
docker run --rm \
  -e TELEGRAM_TOKEN=... \
  -e OPENAI_API_KEY=... \
  -e ALLOWED_TG_IDS=123456,987654 \
  -e SYSTEM_PROMPT="You are a helpful assistant" \
  openai-telegram-bot
```

## Commands

- `/help`
- `/reset`
