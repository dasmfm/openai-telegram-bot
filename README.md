# OpenAI Telegram Bot

A practical Telegram bot powered by OpenAI: chat naturally, understand photos, generate or edit images, and transcribe voice notes, all in one conversation.

## Use Cases

- Family assistant chat with data safety.
- Quick image generation or edits directly from Telegram prompts.
- Voice notes to text, then continue the conversation from transcription.

## Features

- Conversational chat with per-chat memory.
- Photo understanding and follow-up questions.
- Image generation and image editing flows.
- Voice message transcription.
- Document context support for common text-like formats.
- Access control via Telegram user allowlist.

## Requirements

- Telegram bot token (create a bot via `@BotFather`).
- OpenAI API key.
- Docker + Docker Compose (recommended), or Go `1.22+` for local runs.

## Quick Start (Docker Compose)

1. Copy the template:

```bash
cp .env.example .env
```

2. Fill required values in `.env`.

3. Start:

```bash
docker compose up -d
```

Use `--build` when you want compose to rebuild the image from local source:

```bash
docker compose up -d --build
```

4. Logs:

```bash
docker compose logs -f
```

## Run Prebuilt Image (no clone)

```bash
docker run --rm \
  -e TELEGRAM_TOKEN=... \
  -e OPENAI_API_KEY=... \
  -e ALLOWED_TG_IDS=123456789 \
  -e SYSTEM_PROMPT="You are a helpful assistant" \
  ghcr.io/dasmfm/openai-telegram-bot:latest
```

## Local Run

```bash
go run ./cmd/bot
```

## Environment Variables

| Variable | Required | Default | Description | Example |
|---|---|---|---|---|
| `TELEGRAM_TOKEN` | Yes | - | Telegram Bot API token from BotFather. | `123456:ABC...` |
| `OPENAI_API_KEY` | Yes | - | OpenAI API key used for all model calls. | `sk-proj-...` |
| `ALLOWED_TG_IDS` | Yes | - | Comma-separated Telegram user IDs allowed to use the bot. | `123456789,987654321` |
| `OPENAI_MODEL` | No | `gpt-5.2` | Main text model for chat responses. | `gpt-5.2` |
| `IMAGE_MODEL` | No | `gpt-image-1-mini` | Model used for image generation/editing. | `gpt-image-1-mini` |
| `ROUTER_MODEL` | No | `gpt-5-mini` | Model used to route text vs image actions. | `gpt-5-mini` |
| `SYSTEM_PROMPT` | No | empty | Custom behavior prompt appended to base Telegram HTML prompt. | `You are a helpful assistant.` |
| `MAX_HISTORY_MSGS` | No | `20` | Max number of messages kept in memory per chat. | `20` |
| `MAX_FILE_MB` | No | `20` | Max downloaded file size from Telegram. | `20` |
| `REQUEST_TIMEOUT_SEC` | No | `120` | Timeout for OpenAI/Telegram request handling. | `120` |
| `TRANSCRIBE_MODEL` | No | `whisper-1` | Model used for voice transcription. | `whisper-1` |

## Commands

- `/help` — shows a short help message with supported input types.
- `/reset` — clears conversation memory for the current chat.

## Testing

```bash
go test -count=1 ./...
```
