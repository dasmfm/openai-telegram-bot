package telegram

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Client struct {
	bot          *tgbotapi.BotAPI
	allowedIDs   map[int64]bool
	maxFileBytes int64
}

var newBotAPIFn = tgbotapi.NewBotAPI

func New(token string, allowedIDs map[int64]bool, maxFileMB int64) (*Client, error) {
	bot, err := newBotAPIFn(token)
	if err != nil {
		return nil, err
	}

	maxFileBytes := maxFileMB * 1024 * 1024
	if maxFileBytes <= 0 {
		maxFileBytes = 20 * 1024 * 1024
	}

	return &Client{bot: bot, allowedIDs: allowedIDs, maxFileBytes: maxFileBytes}, nil
}

func (c *Client) Bot() *tgbotapi.BotAPI {
	return c.bot
}

func LongPollingConfig() tgbotapi.UpdateConfig {
	updateConfig := tgbotapi.NewUpdate(0)
	updateConfig.Timeout = 60
	return updateConfig
}

func (c *Client) IsAllowed(userID int64) bool {
	if len(c.allowedIDs) == 0 {
		return false
	}
	return c.allowedIDs[userID]
}

func (c *Client) DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	start := time.Now()
	log.Printf("telegram download start: file_id=%s", fileID)
	file, err := c.bot.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil {
		log.Printf("telegram download error: get file err=%v", err)
		return nil, "", err
	}

	url := file.Link(c.bot.Token)
	if url == "" {
		return nil, "", fmt.Errorf("empty file url")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		log.Printf("telegram download error: request err=%v", err)
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		log.Printf("telegram download error: status=%d", resp.StatusCode)
		return nil, "", fmt.Errorf("download status %d", resp.StatusCode)
	}

	var buf bytes.Buffer
	if c.maxFileBytes > 0 {
		_, err = io.CopyN(&buf, resp.Body, c.maxFileBytes+1)
		if err != nil && err != io.EOF {
			return nil, "", err
		}
		if int64(buf.Len()) > c.maxFileBytes {
			return nil, "", fmt.Errorf("file too large")
		}
	} else {
		_, err = io.Copy(&buf, resp.Body)
		if err != nil {
			return nil, "", err
		}
	}

	data := buf.Bytes()
	name := filepath.Base(file.FilePath)
	log.Printf("telegram download done: ms=%d bytes=%d name=%s", time.Since(start).Milliseconds(), len(data), name)
	return data, name, nil
}

func (c *Client) EditMessage(chatID int64, messageID int, text string) error {
	text = sanitizeHTML(text)
	edit := tgbotapi.NewEditMessageText(chatID, messageID, text)
	edit.DisableWebPagePreview = true
	edit.ParseMode = "HTML"
	_, err := c.bot.Send(edit)
	if err != nil {
		if strings.Contains(err.Error(), "can't parse entities") {
			fallback := tgbotapi.NewEditMessageText(chatID, messageID, text)
			fallback.DisableWebPagePreview = true
			fallback.ParseMode = ""
			if _, retryErr := c.bot.Send(fallback); retryErr != nil {
				log.Printf("telegram edit fallback error: chat=%d msg_id=%d err=%v", chatID, messageID, retryErr)
				return err
			}
			return nil
		}
		log.Printf("telegram edit error: chat=%d msg_id=%d err=%v", chatID, messageID, err)
	}
	return err
}

func (c *Client) DeleteMessage(chatID int64, messageID int) error {
	deleteMsg := tgbotapi.NewDeleteMessage(chatID, messageID)
	_, err := c.bot.Request(deleteMsg)
	if err != nil {
		log.Printf("telegram delete error: chat=%d msg_id=%d err=%v", chatID, messageID, err)
	}
	return err
}

func (c *Client) SendText(chatID int64, text string) error {
	text = sanitizeHTML(text)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "HTML"
	_, err := c.bot.Send(msg)
	if err != nil {
		if strings.Contains(err.Error(), "can't parse entities") {
			fallback := tgbotapi.NewMessage(chatID, text)
			fallback.DisableWebPagePreview = true
			fallback.ParseMode = ""
			if _, retryErr := c.bot.Send(fallback); retryErr != nil {
				log.Printf("telegram send fallback error: chat=%d err=%v", chatID, retryErr)
				return err
			}
			return nil
		}
		log.Printf("telegram send error: chat=%d err=%v", chatID, err)
	}
	return err
}

func (c *Client) SendMessage(chatID int64, text string) (int, error) {
	text = sanitizeHTML(text)
	msg := tgbotapi.NewMessage(chatID, text)
	msg.DisableWebPagePreview = true
	msg.ParseMode = "HTML"
	res, err := c.bot.Send(msg)
	if err != nil {
		if strings.Contains(err.Error(), "can't parse entities") {
			fallback := tgbotapi.NewMessage(chatID, text)
			fallback.DisableWebPagePreview = true
			fallback.ParseMode = ""
			res, retryErr := c.bot.Send(fallback)
			if retryErr != nil {
				log.Printf("telegram send fallback error: chat=%d err=%v", chatID, retryErr)
				return 0, err
			}
			log.Printf("telegram send message: chat=%d msg_id=%d", chatID, res.MessageID)
			return res.MessageID, nil
		}
		log.Printf("telegram send error: chat=%d err=%v", chatID, err)
		return 0, err
	}
	log.Printf("telegram send message: chat=%d msg_id=%d", chatID, res.MessageID)
	return res.MessageID, nil
}

func (c *Client) SendPhotoBytes(chatID int64, data []byte) error {
	photo := tgbotapi.NewPhoto(chatID, tgbotapi.FileBytes{
		Name:  "image.png",
		Bytes: data,
	})
	_, err := c.bot.Send(photo)
	if err != nil {
		log.Printf("telegram send photo error: chat=%d bytes=%d err=%v", chatID, len(data), err)
	}
	return err
}

func (c *Client) Typing(chatID int64) {
	if _, err := c.bot.Request(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)); err != nil {
		log.Printf("telegram typing error: chat=%d err=%v", chatID, err)
	}
}

func SplitTelegramMessage(text string) []string {
	const maxLen = 4000
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}

	var parts []string
	for len(text) > maxLen {
		cut := strings.LastIndexAny(text[:maxLen], "\n ")
		if cut < maxLen/2 {
			cut = maxLen
		}
		parts = append(parts, strings.TrimSpace(text[:cut]))
		text = strings.TrimSpace(text[cut:])
	}
	parts = append(parts, text)
	return parts
}

func sanitizeHTML(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return text
	}
	allowed := map[string]bool{
		"b":    true,
		"i":    true,
		"u":    true,
		"code": true,
	}
	var stack []string
	var out strings.Builder
	for i := 0; i < len(text); i++ {
		ch := text[i]
		if ch != '<' {
			out.WriteByte(ch)
			continue
		}
		end := strings.IndexByte(text[i:], '>')
		if end < 0 {
			out.WriteByte(ch)
			continue
		}
		tag := text[i+1 : i+end]
		lower := strings.ToLower(strings.TrimSpace(tag))
		if lower == "" {
			out.WriteString(text[i : i+end+1])
			i += end
			continue
		}
		isClose := strings.HasPrefix(lower, "/")
		name := strings.TrimPrefix(lower, "/")
		if !allowed[name] {
			out.WriteString(text[i : i+end+1])
			i += end
			continue
		}
		if isClose {
			if len(stack) == 0 || stack[len(stack)-1] != name {
				i += end
				continue
			}
			stack = stack[:len(stack)-1]
			out.WriteString("</" + name + ">")
			i += end
			continue
		}
		stack = append(stack, name)
		out.WriteString("<" + name + ">")
		i += end
	}
	for i := len(stack) - 1; i >= 0; i-- {
		out.WriteString("</" + stack[i] + ">")
	}
	return out.String()
}

func ImageDataURL(contentType string, data []byte) string {
	if contentType == "" {
		contentType = "image/jpeg"
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("data:%s;base64,%s", contentType, encoded)
}

func GuessImageContentType(filename string) string {
	lower := strings.ToLower(filename)
	switch {
	case strings.HasSuffix(lower, ".png"):
		return "image/png"
	case strings.HasSuffix(lower, ".webp"):
		return "image/webp"
	case strings.HasSuffix(lower, ".gif"):
		return "image/gif"
	default:
		return "image/jpeg"
	}
}
