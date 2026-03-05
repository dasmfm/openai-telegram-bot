package handlers

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/openai/openai-go/v3/responses"

	"github.com/dasmfm/openai-telegram-bot/internal/memory"
	openaiwrap "github.com/dasmfm/openai-telegram-bot/internal/openai"
	"github.com/dasmfm/openai-telegram-bot/internal/telegram"
)

type Handler struct {
	TG             tgClient
	OA             oaClient
	Store          *memory.Store
	MaxHistory     int
	RequestTimeout time.Duration
}

type tgClient interface {
	IsAllowed(userID int64) bool
	SendText(chatID int64, text string) error
	SendMessage(chatID int64, text string) (int, error)
	DeleteMessage(chatID int64, messageID int) error
	SendPhotoBytes(chatID int64, data []byte) error
	Typing(chatID int64)
	DownloadFile(ctx context.Context, fileID string) ([]byte, string, error)
}

type oaClient interface {
	BuildInput(messages []openaiwrap.MessageInput) []responses.ResponseInputItemUnionParam
	ClassifyImageRequest(ctx context.Context, text string) (bool, error)
	TextResponse(ctx context.Context, input []responses.ResponseInputItemUnionParam) (string, error)
	GenerateImage(ctx context.Context, prompt string) ([]byte, error)
	EditImage(ctx context.Context, prompt string, imageDataURL string) ([]byte, error)
	UploadFile(ctx context.Context, data []byte, filename string, contentType string) (string, error)
	Transcribe(ctx context.Context, data []byte, filename string, contentType string) (string, error)
	DeleteFile(ctx context.Context, fileID string) error
	GuardImageAction(ctx context.Context, prompt string, isEdit bool) (bool, string, error)
}

var supportedContextFileExt = map[string]bool{
	".art": true, ".bat": true, ".brf": true, ".c": true, ".cls": true,
	".css": true, ".diff": true, ".eml": true, ".es": true, ".h": true,
	".hs": true, ".htm": true, ".html": true, ".ics": true, ".ifb": true,
	".java": true, ".js": true, ".json": true, ".ksh": true, ".ltx": true,
	".mail": true, ".markdown": true, ".md": true, ".mht": true, ".mhtml": true,
	".mjs": true, ".nws": true, ".patch": true, ".pdf": true, ".pl": true,
	".pm": true, ".pot": true, ".py": true, ".rst": true, ".scala": true,
	".sh": true, ".shtml": true, ".srt": true, ".sty": true, ".tex": true,
	".text": true, ".txt": true, ".vcf": true, ".vtt": true, ".xml": true,
	".yaml": true, ".yml": true,
}

func (h *Handler) HandleUpdate(update tgbotapi.Update) {
	if update.Message == nil {
		return
	}

	msg := update.Message
	if msg.From == nil {
		return
	}
	userID := msg.From.ID
	chatID := msg.Chat.ID

	if !h.TG.IsAllowed(userID) {
		return
	}

	if msg.IsCommand() {
		h.handleCommand(msg)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), h.RequestTimeout)
	defer cancel()

	input, userText, imagePrompt, err := h.buildInput(ctx, msg)
	if err != nil {
		log.Printf("build input error: %v", err)
		_ = h.TG.SendText(chatID, "Failed to process message. Please try again.")
		return
	}

	fileIDs := extractFileIDs(input)
	if len(fileIDs) > 0 {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), h.RequestTimeout)
		defer cleanupCancel()
		defer h.cleanupFiles(cleanupCtx, fileIDs)
	}
	if imagePrompt == "" && hasImageInput(input) {
		captionPrompt := extractImagePrompt(input)
		if captionPrompt != "" {
			classifierText := captionPrompt
			if contextText := h.recentContext(chatID, 3); contextText != "" {
				classifierText = contextText + "\n\nCurrent message:\n" + captionPrompt
			}
			wantImage, err := h.OA.ClassifyImageRequest(ctx, classifierText)
			if err != nil {
				log.Printf("router error: %v", err)
			}
			log.Printf("router classify (caption): chat=%d user=%d want_image=%t", chatID, userID, wantImage)
			if wantImage {
				imagePrompt = captionPrompt
				input = nil
			}
		}
	}
	if imagePrompt != "" {
		log.Printf("image request detected: chat=%d user=%d", chatID, userID)
		editDataURL, hasEditImage := h.Store.ConsumeLastImage(chatID, 2*time.Minute)
		allowImageAction, guardReply, guardErr := h.OA.GuardImageAction(ctx, imagePrompt, hasEditImage)
		if guardErr != nil {
			log.Printf("image guard error: %v", guardErr)
		}
		if guardErr == nil && !allowImageAction {
			if strings.TrimSpace(guardReply) == "" {
				guardReply = "Прости, с таким запросом по изображению я не могу помочь."
			}
			_ = h.TG.SendText(chatID, guardReply)
			h.Store.Append(chatID, memory.Message{Role: "user", Text: imagePrompt}, h.MaxHistory)
			h.Store.Append(chatID, memory.Message{Role: "assistant", Text: guardReply}, h.MaxHistory)
			return
		}
		statusText := "Генерирую изображение..."
		fallbackText := "Готовлю изображение по твоему запросу."
		preSystem := "Пользователь попросил сгенерировать изображение. Картинка уже будет сгенерирована отдельно. Ответь коротко и по-человечески, без вопросов и без ASCII-арта."
		if hasEditImage {
			statusText = "Редактирую изображение..."
			fallbackText = "Редактирую изображение по твоему запросу."
			preSystem = "Пользователь попросил отредактировать изображение. Картинка уже будет отредактирована отдельно. Ответь коротко и по-человечески, без вопросов и без ASCII-арта."
		}
		history := h.Store.Get(chatID)
		openaiInput := make([]openaiwrap.MessageInput, 0, len(history)+1)
		for _, m := range history {
			openaiInput = append(openaiInput, openaiwrap.MessageInput{
				Role: m.Role,
				Text: m.Text,
			})
		}
		openaiInput = append(openaiInput, openaiwrap.MessageInput{Role: "user", Text: imagePrompt})
		preInput := append([]openaiwrap.MessageInput{{
			Role: "system",
			Text: preSystem,
		}}, openaiInput...)
		preStart := time.Now()
		preText, err := h.OA.TextResponse(ctx, h.OA.BuildInput(preInput))
		if err != nil {
			log.Printf("pre-response error: %v", err)
		}
		log.Printf("pre-response done: chat=%d user=%d ms=%d", chatID, userID, time.Since(preStart).Milliseconds())

		h.Store.Append(chatID, memory.Message{Role: "user", Text: imagePrompt}, h.MaxHistory)
		preText = strings.TrimSpace(preText)
		if preText == "" || strings.Contains(preText, "?") {
			if preText == "" {
				log.Printf("pre-response empty: chat=%d user=%d", chatID, userID)
			} else {
				log.Printf("pre-response had question: chat=%d user=%d", chatID, userID)
			}
			preText = fallbackText
		}
		if err := h.TG.SendText(chatID, preText); err != nil {
			log.Printf("send pre-text error: %v", err)
		}
		h.Store.Append(chatID, memory.Message{Role: "assistant", Text: preText}, h.MaxHistory)
		statusID, err := h.TG.SendMessage(chatID, statusText)
		if err != nil {
			log.Printf("send image status error: %v", err)
			statusID = 0
		}
		log.Printf("image status sent: chat=%d user=%d status_id=%d", chatID, userID, statusID)

		imageStart := time.Now()
		var imageBytes []byte
		if hasEditImage {
			imageBytes, err = h.OA.EditImage(ctx, imagePrompt, editDataURL)
		} else {
			imageBytes, err = h.OA.GenerateImage(ctx, imagePrompt)
		}
		if err != nil {
			log.Printf("image generation error: %v", err)
			if statusID > 0 {
				if err := h.TG.DeleteMessage(chatID, statusID); err != nil {
					log.Printf("delete image status error: %v", err)
				}
			}
			if hasEditImage {
				_ = h.TG.SendText(chatID, "Редактирование изображения не удалось.")
			} else {
				_ = h.TG.SendText(chatID, "Image generation failed.")
			}
			return
		}
		log.Printf("image generation done: chat=%d user=%d bytes=%d ms=%d", chatID, userID, len(imageBytes), time.Since(imageStart).Milliseconds())
		if err := h.TG.SendPhotoBytes(chatID, imageBytes); err != nil {
			log.Printf("send photo error: %v", err)
			if statusID > 0 {
				if err := h.TG.DeleteMessage(chatID, statusID); err != nil {
					log.Printf("delete image status error: %v", err)
				}
			}
			_ = h.TG.SendText(chatID, "Failed to send image.")
			return
		}
		if statusID > 0 {
			if err := h.TG.DeleteMessage(chatID, statusID); err != nil {
				log.Printf("delete image status error: %v", err)
			}
		}
		return
	}

	if strings.TrimSpace(userText) == "" && len(input) == 0 {
		_ = h.TG.SendText(chatID, "Send text, photo, file, or voice.")
		return
	}

	history := h.Store.Get(chatID)
	hasImage := hasImageInput(input)
	openaiInput := make([]openaiwrap.MessageInput, 0, len(history)+len(input)+2)
	if hasImage {
		openaiInput = append(openaiInput, openaiwrap.MessageInput{
			Role: "system",
			Text: "В сообщении есть изображение. Ты видишь его. Отвечай по содержанию и не говори, что не видишь/не умеешь.",
		})
	}
	for _, m := range history {
		openaiInput = append(openaiInput, openaiwrap.MessageInput{
			Role: m.Role,
			Text: m.Text,
		})
	}
	if len(input) > 0 {
		openaiInput = append(openaiInput, input...)
	} else {
		openaiInput = append(openaiInput, openaiwrap.MessageInput{Role: "user", Text: userText})
	}

	if len(input) > 0 {
		for _, m := range input {
			h.Store.Append(chatID, sanitizeForHistory(m), h.MaxHistory)
		}
	} else {
		h.Store.Append(chatID, memory.Message{Role: "user", Text: userText}, h.MaxHistory)
	}

	h.TG.Typing(chatID)
	items := h.OA.BuildInput(openaiInput)
	if isShortText(userText, input) {
		h.TG.Typing(chatID)
		text, err := h.OA.TextResponse(ctx, items)
		if err != nil {
			log.Printf("text response error: %v", err)
			_ = h.TG.SendText(chatID, "Model request failed.")
			return
		}
		finalText := shortenReply(strings.TrimSpace(text), 2, 200)
		if finalText == "" {
			finalText = "(empty response)"
		}
		_ = h.TG.SendText(chatID, finalText)
		h.Store.Append(chatID, memory.Message{Role: "assistant", Text: finalText}, h.MaxHistory)
		return
	}
	full, err := h.OA.TextResponse(ctx, items)
	if err != nil {
		log.Printf("text response error: %v", err)
		_ = h.TG.SendText(chatID, "Model request failed.")
		return
	}

	finalText := strings.TrimSpace(full)
	if finalText == "" {
		finalText = "(empty response)"
	}
	if len(finalText) <= 4000 {
		_ = h.TG.SendText(chatID, finalText)
	} else {
		parts := telegram.SplitTelegramMessage(finalText)
		if len(parts) == 0 {
			parts = []string{""}
		}
		for _, part := range parts {
			_ = h.TG.SendText(chatID, part)
		}
	}

	h.Store.Append(chatID, memory.Message{Role: "assistant", Text: finalText}, h.MaxHistory)
}

func sanitizeForHistory(m openaiwrap.MessageInput) memory.Message {
	text := strings.TrimSpace(m.Text)
	if m.FileName != "" || m.FileData != "" {
		label := "[file attached]"
		if m.FileName != "" {
			label = fmt.Sprintf("[file: %s]", m.FileName)
		}
		if text != "" {
			text = text + "\n" + label
		} else {
			text = label
		}
	}
	if m.ImageDataURL != "" {
		label := "[image attached]"
		if text != "" {
			text = text + "\n" + label
		} else {
			text = label
		}
	}
	return memory.Message{Role: "user", Text: text}
}

func extractFileIDs(inputs []openaiwrap.MessageInput) []string {
	ids := make([]string, 0, len(inputs))
	for _, input := range inputs {
		if input.FileID != "" {
			ids = append(ids, input.FileID)
		}
	}
	return ids
}

func extractImagePrompt(inputs []openaiwrap.MessageInput) string {
	for _, input := range inputs {
		if input.ImageDataURL == "" {
			continue
		}
		text := strings.TrimSpace(input.Text)
		if text != "" {
			return text
		}
	}
	return ""
}

func (h *Handler) recentContext(chatID int64, limit int) string {
	if limit <= 0 {
		return ""
	}
	history := h.Store.Get(chatID)
	if len(history) == 0 {
		return ""
	}
	if len(history) > limit {
		history = history[len(history)-limit:]
	}
	var b strings.Builder
	for _, msg := range history {
		role := strings.TrimSpace(msg.Role)
		if role == "" {
			role = "user"
		}
		text := strings.TrimSpace(msg.Text)
		if text == "" {
			continue
		}
		b.WriteString(role)
		b.WriteString(": ")
		b.WriteString(text)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func hasImageInput(inputs []openaiwrap.MessageInput) bool {
	for _, input := range inputs {
		if input.ImageDataURL != "" {
			return true
		}
	}
	return false
}

func isShortText(userText string, inputs []openaiwrap.MessageInput) bool {
	if len(inputs) > 0 {
		return false
	}
	text := strings.TrimSpace(userText)
	if text == "" {
		return false
	}
	return len([]rune(text)) <= 30
}

func shortenReply(text string, maxSentences int, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if idx := strings.IndexByte(text, '\n'); idx >= 0 {
		text = strings.TrimSpace(text[:idx])
	}
	if maxSentences > 0 {
		count := 0
		end := -1
	endLoop:
		for i, r := range text {
			switch r {
			case '.', '!', '?':
				count++
				if count >= maxSentences {
					end = i + len(string(r))
					break endLoop
				}
			}
		}
		if end > 0 && end <= len(text) {
			text = strings.TrimSpace(text[:end])
		}
	}
	if maxRunes > 0 {
		runes := []rune(text)
		if len(runes) > maxRunes {
			text = strings.TrimSpace(string(runes[:maxRunes]))
		}
	}
	return text
}

func (h *Handler) cleanupFiles(ctx context.Context, fileIDs []string) {
	for _, id := range fileIDs {
		if err := h.OA.DeleteFile(ctx, id); err != nil {
			log.Printf("delete file error: %v", err)
		}
	}
}

func (h *Handler) handleCommand(msg *tgbotapi.Message) {
	chatID := msg.Chat.ID
	switch msg.Command() {
	case "reset":
		h.Store.Reset(chatID)
		_ = h.TG.SendText(chatID, "Context cleared.")
	case "help":
		_ = h.TG.SendText(chatID, "I am a proxy bot. I support text, photos, files, and voice. Use /reset to clear context.")
	default:
		_ = h.TG.SendText(chatID, "Unknown command. /help")
	}
}

func (h *Handler) buildInput(ctx context.Context, msg *tgbotapi.Message) ([]openaiwrap.MessageInput, string, string, error) {
	if msg.Text != "" {
		classifierText := msg.Text
		if contextText := h.recentContext(msg.Chat.ID, 3); contextText != "" {
			classifierText = contextText + "\n\nCurrent message:\n" + msg.Text
		}
		wantImage, err := h.OA.ClassifyImageRequest(ctx, classifierText)
		if err != nil {
			log.Printf("router error: %v", err)
		}
		log.Printf("router classify: chat=%d user=%d want_image=%t", msg.Chat.ID, msg.From.ID, wantImage)
		if wantImage {
			return nil, "", msg.Text, nil
		}
		if dataURL, ok := h.Store.ConsumeLastImage(msg.Chat.ID, 2*time.Minute); ok {
			return []openaiwrap.MessageInput{{
				Role:         "user",
				Text:         msg.Text,
				ImageDataURL: dataURL,
			}}, "", "", nil
		}
		return nil, msg.Text, "", nil
	}

	if len(msg.Photo) > 0 {
		photo := msg.Photo[len(msg.Photo)-1]
		data, filename, err := h.TG.DownloadFile(ctx, photo.FileID)
		if err != nil {
			return nil, "", "", err
		}
		if filename == "" {
			filename = "image.jpg"
		}
		contentType := telegram.GuessImageContentType(filename)
		dataURL := telegram.ImageDataURL(contentType, data)
		h.Store.SetLastImage(msg.Chat.ID, dataURL)
		return []openaiwrap.MessageInput{{
			Role:         "user",
			Text:         msg.Caption,
			ImageDataURL: dataURL,
		}}, "", "", nil
	}

	if msg.Document != nil {
		data, filename, err := h.TG.DownloadFile(ctx, msg.Document.FileID)
		if err != nil {
			return nil, "", "", err
		}
		if filename == "" {
			filename = strings.TrimSpace(msg.Document.FileName)
		}
		if filename == "" {
			filename = "file.bin"
		}

		contentType := strings.TrimSpace(msg.Document.MimeType)
		if contentType == "" {
			contentType = strings.TrimSpace(http.DetectContentType(data))
		}
		if strings.HasPrefix(strings.ToLower(contentType), "image/") {
			dataURL := telegram.ImageDataURL(contentType, data)
			h.Store.SetLastImage(msg.Chat.ID, dataURL)
			return []openaiwrap.MessageInput{{
				Role:         "user",
				Text:         msg.Caption,
				ImageDataURL: dataURL,
			}}, "", "", nil
		}

		if !isSupportedContextFile(filename) {
			return nil, unsupportedFilePrompt(filename), "", nil
		}

		fileID, err := h.OA.UploadFile(ctx, data, filename, contentType)
		if err != nil {
			return nil, "", "", err
		}
		input := openaiwrap.MessageInput{
			Role:     "user",
			Text:     msg.Caption,
			FileID:   fileID,
			FileName: filename,
		}
		return []openaiwrap.MessageInput{input}, "", "", nil
	}

	if msg.Voice != nil {
		data, filename, err := h.TG.DownloadFile(ctx, msg.Voice.FileID)
		if err != nil {
			return nil, "", "", err
		}
		if filename == "" {
			filename = "voice.ogg"
		}
		transcription, err := h.OA.Transcribe(ctx, data, filename, "audio/ogg")
		if err != nil {
			return nil, "", "", err
		}
		return nil, transcription, "", nil
	}

	return nil, "", "", nil
}

func isSupportedContextFile(filename string) bool {
	ext := strings.ToLower(strings.TrimSpace(filepath.Ext(filename)))
	return supportedContextFileExt[ext]
}

func unsupportedFilePrompt(filename string) string {
	name := strings.TrimSpace(filename)
	if name == "" {
		name = "этот файл"
	}
	return fmt.Sprintf("Пользователь отправил файл %q. Вежливо и коротко объясни, что с таким типом файлов ты пока не умеешь работать.", name)
}
