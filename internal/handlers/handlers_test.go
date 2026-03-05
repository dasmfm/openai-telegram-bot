package handlers

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	"github.com/openai/openai-go/v3/responses"

	"openai_telegram_bot/internal/memory"
	openaiwrap "openai_telegram_bot/internal/openai"
)

type downloadedFile struct {
	data     []byte
	filename string
}

type fakeTG struct {
	allowed      bool
	downloads    map[string]downloadedFile
	downloadErr  error
	sentTexts    []string
	sendTextErr  error
	sentMessages []string
	sendMsgErr   error
	deleted      []int
	deleteErr    error
	photos       [][]byte
	sendPhotoErr error
	typingCount  int
	nextMessage  int
}

func (f *fakeTG) IsAllowed(userID int64) bool {
	return f.allowed
}

func (f *fakeTG) SendText(chatID int64, text string) error {
	f.sentTexts = append(f.sentTexts, text)
	if f.sendTextErr != nil {
		return f.sendTextErr
	}
	return nil
}

func (f *fakeTG) SendMessage(chatID int64, text string) (int, error) {
	f.sentMessages = append(f.sentMessages, text)
	if f.sendMsgErr != nil {
		return 0, f.sendMsgErr
	}
	f.nextMessage++
	return f.nextMessage, nil
}

func (f *fakeTG) DeleteMessage(chatID int64, messageID int) error {
	f.deleted = append(f.deleted, messageID)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	return nil
}

func (f *fakeTG) SendPhotoBytes(chatID int64, data []byte) error {
	f.photos = append(f.photos, data)
	if f.sendPhotoErr != nil {
		return f.sendPhotoErr
	}
	return nil
}

func (f *fakeTG) Typing(chatID int64) {
	f.typingCount++
}

func (f *fakeTG) DownloadFile(ctx context.Context, fileID string) ([]byte, string, error) {
	if f.downloadErr != nil {
		return nil, "", f.downloadErr
	}
	file := f.downloads[fileID]
	return file.data, file.filename, nil
}

type fakeOA struct {
	classifyFn       func(string) bool
	classifyInputs   []string
	classifyErr      error
	textResponses    []string
	textCalls        int
	textErr          error
	builtMessages    [][]openaiwrap.MessageInput
	generateCalls    []string
	generateResp     []byte
	generateErr      error
	editCalls        []struct{ prompt, image string }
	editResp         []byte
	editErr          error
	uploadedFileIDs  []string
	uploadResp       string
	uploadErr        error
	deletedFileIDs   []string
	transcribeResp   string
	transcribeCalled int
	transcribeErr    error
	guardAllow       bool
	guardReply       string
	guardErr         error
}

func (f *fakeOA) BuildInput(messages []openaiwrap.MessageInput) []responses.ResponseInputItemUnionParam {
	copyMsgs := make([]openaiwrap.MessageInput, len(messages))
	copy(copyMsgs, messages)
	f.builtMessages = append(f.builtMessages, copyMsgs)
	return nil
}

func (f *fakeOA) ClassifyImageRequest(ctx context.Context, text string) (bool, error) {
	f.classifyInputs = append(f.classifyInputs, text)
	if f.classifyErr != nil {
		return false, f.classifyErr
	}
	if f.classifyFn == nil {
		return false, nil
	}
	return f.classifyFn(text), nil
}

func (f *fakeOA) TextResponse(ctx context.Context, input []responses.ResponseInputItemUnionParam) (string, error) {
	f.textCalls++
	if f.textErr != nil {
		return "", f.textErr
	}
	if len(f.textResponses) == 0 {
		return "", nil
	}
	resp := f.textResponses[0]
	f.textResponses = f.textResponses[1:]
	return resp, nil
}

func (f *fakeOA) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	f.generateCalls = append(f.generateCalls, prompt)
	if f.generateErr != nil {
		return nil, f.generateErr
	}
	return f.generateResp, nil
}

func (f *fakeOA) EditImage(ctx context.Context, prompt string, imageDataURL string) ([]byte, error) {
	f.editCalls = append(f.editCalls, struct{ prompt, image string }{prompt: prompt, image: imageDataURL})
	if f.editErr != nil {
		return nil, f.editErr
	}
	return f.editResp, nil
}

func (f *fakeOA) UploadFile(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	if f.uploadErr != nil {
		return "", f.uploadErr
	}
	id := f.uploadResp
	if id == "" {
		id = "file-1"
	}
	f.uploadedFileIDs = append(f.uploadedFileIDs, id)
	return id, nil
}

func (f *fakeOA) Transcribe(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	f.transcribeCalled++
	if f.transcribeErr != nil {
		return "", f.transcribeErr
	}
	return f.transcribeResp, nil
}

func (f *fakeOA) DeleteFile(ctx context.Context, fileID string) error {
	f.deletedFileIDs = append(f.deletedFileIDs, fileID)
	return nil
}

func (f *fakeOA) GuardImageAction(ctx context.Context, prompt string, isEdit bool) (bool, string, error) {
	if f.guardErr != nil {
		return false, "", f.guardErr
	}
	if !f.guardAllow && f.guardReply == "" {
		return true, "", nil
	}
	return f.guardAllow, f.guardReply, nil
}

func newTestHandler(tg *fakeTG, oa *fakeOA) *Handler {
	return &Handler{
		TG:             tg,
		OA:             oa,
		Store:          memory.NewStore(),
		MaxHistory:     50,
		RequestTimeout: 5 * time.Second,
	}
}

func makeTextUpdate(chatID, userID int64, text string) tgbotapi.Update {
	msg := &tgbotapi.Message{
		Text: text,
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: chatID},
	}
	return tgbotapi.Update{Message: msg}
}

func makeCommandUpdate(chatID, userID int64, cmd string) tgbotapi.Update {
	text := "/" + cmd
	msg := &tgbotapi.Message{
		Text: text,
		Entities: []tgbotapi.MessageEntity{{
			Type:   "bot_command",
			Offset: 0,
			Length: len(text),
		}},
		From: &tgbotapi.User{ID: userID},
		Chat: &tgbotapi.Chat{ID: chatID},
	}
	return tgbotapi.Update{Message: msg}
}

func TestHandleUpdate_IgnoresDisallowedUser(t *testing.T) {
	tg := &fakeTG{allowed: false}
	oa := &fakeOA{}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "привет"))

	if len(tg.sentTexts) != 0 {
		t.Fatalf("expected no messages for disallowed user, got %d", len(tg.sentTexts))
	}
	if oa.textCalls != 0 {
		t.Fatalf("expected no model calls, got %d", oa.textCalls)
	}
}

func TestHandleUpdate_ShortTextUsesShortReplyPath(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{
		textResponses: []string{"Привет! Как дела? Еще что-то.\nЛишняя строка"},
	}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "привет"))

	if got := len(tg.sentTexts); got != 1 {
		t.Fatalf("expected 1 sent text, got %d", got)
	}
	if tg.sentTexts[0] != "Привет! Как дела?" {
		t.Fatalf("unexpected short reply: %q", tg.sentTexts[0])
	}
	if tg.typingCount != 2 {
		t.Fatalf("expected typing twice for short path, got %d", tg.typingCount)
	}
}

func TestHandleUpdate_ImageCaptionUsesEditFlow(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"photo-1": {data: []byte("jpg-bytes"), filename: "photo.jpg"},
		},
	}
	oa := &fakeOA{
		classifyFn:    func(text string) bool { return true },
		textResponses: []string{"Что тебе сгенерировать?"},
		editResp:      []byte("edited-image"),
	}
	h := newTestHandler(tg, oa)

	update := tgbotapi.Update{Message: &tgbotapi.Message{
		From:    &tgbotapi.User{ID: 2},
		Chat:    &tgbotapi.Chat{ID: 1},
		Caption: "убрать йогурт с фото",
		Photo: []tgbotapi.PhotoSize{{
			FileID: "photo-1",
		}},
	}}

	h.HandleUpdate(update)

	if len(oa.editCalls) != 1 {
		t.Fatalf("expected edit image call, got %d", len(oa.editCalls))
	}
	if len(oa.generateCalls) != 0 {
		t.Fatalf("expected no generate image call, got %d", len(oa.generateCalls))
	}
	if len(tg.sentMessages) != 1 || tg.sentMessages[0] != "Редактирую изображение..." {
		t.Fatalf("unexpected status message: %#v", tg.sentMessages)
	}
	if len(tg.deleted) != 1 {
		t.Fatalf("expected status delete, got %d", len(tg.deleted))
	}
	if len(tg.photos) != 1 {
		t.Fatalf("expected edited photo to be sent, got %d", len(tg.photos))
	}
	if len(tg.sentTexts) == 0 || tg.sentTexts[0] != "Редактирую изображение по твоему запросу." {
		t.Fatalf("unexpected pre-text: %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_TextImageRequestUsesGenerateFlow(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{
		classifyFn:    func(text string) bool { return true },
		textResponses: []string{"Сейчас сделаю."},
		generateResp:  []byte("generated"),
	}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "нарисуй кота"))

	if len(oa.generateCalls) != 1 {
		t.Fatalf("expected generate image call, got %d", len(oa.generateCalls))
	}
	if len(oa.editCalls) != 0 {
		t.Fatalf("expected no edit call, got %d", len(oa.editCalls))
	}
	if len(tg.sentMessages) != 1 || tg.sentMessages[0] != "Генерирую изображение..." {
		t.Fatalf("unexpected status message: %#v", tg.sentMessages)
	}
}

func TestHandleUpdate_ImageRefusalDoesNotGenerate(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{
		classifyFn:   func(text string) bool { return true },
		guardAllow:   false,
		guardReply:   "Прости, но я не могу помогать удалять вотермарки.",
		generateResp: []byte("generated"),
	}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "удали вотермарки"))

	if len(oa.generateCalls) != 0 {
		t.Fatalf("expected no generate call after refusal, got %d", len(oa.generateCalls))
	}
	if len(oa.editCalls) != 0 {
		t.Fatalf("expected no edit call after refusal, got %d", len(oa.editCalls))
	}
	if len(tg.sentMessages) != 0 {
		t.Fatalf("expected no status message after refusal, got %#v", tg.sentMessages)
	}
	if len(tg.sentTexts) == 0 || !strings.Contains(strings.ToLower(tg.sentTexts[0]), "не могу") {
		t.Fatalf("expected refusal text to be sent, got %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_DocumentCleansUpUploadedFile(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"doc-1": {data: []byte("doc-bytes"), filename: "note.txt"},
		},
	}
	oa := &fakeOA{
		uploadResp:    "file-42",
		textResponses: []string{"Готово."},
	}
	h := newTestHandler(tg, oa)

	update := tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: 2},
		Chat: &tgbotapi.Chat{ID: 1},
		Document: &tgbotapi.Document{
			FileID:   "doc-1",
			MimeType: "text/plain",
		},
		Caption: "прочитай",
	}}

	h.HandleUpdate(update)

	if len(oa.uploadedFileIDs) != 1 || oa.uploadedFileIDs[0] != "file-42" {
		t.Fatalf("unexpected upload calls: %#v", oa.uploadedFileIDs)
	}
	if len(oa.deletedFileIDs) != 1 || oa.deletedFileIDs[0] != "file-42" {
		t.Fatalf("expected cleanup delete for file-42, got %#v", oa.deletedFileIDs)
	}
}

func TestBuildInput_VoiceTranscribe(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"voice-1": {data: []byte("ogg"), filename: "voice.ogg"},
		},
	}
	oa := &fakeOA{transcribeResp: "расшифровка"}
	h := newTestHandler(tg, oa)

	msg := &tgbotapi.Message{
		From:  &tgbotapi.User{ID: 2},
		Chat:  &tgbotapi.Chat{ID: 1},
		Voice: &tgbotapi.Voice{FileID: "voice-1"},
	}

	input, userText, imagePrompt, err := h.buildInput(context.Background(), msg)
	if err != nil {
		t.Fatalf("buildInput returned error: %v", err)
	}
	if len(input) != 0 {
		t.Fatalf("expected no structured input for voice, got %d", len(input))
	}
	if userText != "расшифровка" {
		t.Fatalf("unexpected transcription: %q", userText)
	}
	if imagePrompt != "" {
		t.Fatalf("unexpected image prompt: %q", imagePrompt)
	}
}

func TestBuildInput_DocumentImageHandledAsImage(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"doc-img": {data: []byte("jpeg-bytes"), filename: "scan.jpeg"},
		},
	}
	oa := &fakeOA{}
	h := newTestHandler(tg, oa)

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 2},
		Chat: &tgbotapi.Chat{ID: 1},
		Document: &tgbotapi.Document{
			FileID:   "doc-img",
			MimeType: "image/jpeg",
		},
		Caption: "что на картинке?",
	}

	input, userText, imagePrompt, err := h.buildInput(context.Background(), msg)
	if err != nil {
		t.Fatalf("buildInput returned error: %v", err)
	}
	if userText != "" || imagePrompt != "" {
		t.Fatalf("expected image input path, got userText=%q imagePrompt=%q", userText, imagePrompt)
	}
	if len(input) != 1 || input[0].ImageDataURL == "" {
		t.Fatalf("expected single image input, got %#v", input)
	}
	if len(oa.uploadedFileIDs) != 0 {
		t.Fatalf("image document must not be uploaded as file, got uploads %#v", oa.uploadedFileIDs)
	}
}

func TestBuildInput_UnsupportedDocumentReturnsModelPrompt(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"doc-bin": {data: []byte("bin"), filename: "archive.zip"},
		},
	}
	oa := &fakeOA{}
	h := newTestHandler(tg, oa)

	msg := &tgbotapi.Message{
		From: &tgbotapi.User{ID: 2},
		Chat: &tgbotapi.Chat{ID: 1},
		Document: &tgbotapi.Document{
			FileID:   "doc-bin",
			MimeType: "application/zip",
		},
	}

	input, userText, imagePrompt, err := h.buildInput(context.Background(), msg)
	if err != nil {
		t.Fatalf("buildInput returned error: %v", err)
	}
	if len(input) != 0 || imagePrompt != "" {
		t.Fatalf("expected no file/image input, got input=%#v imagePrompt=%q", input, imagePrompt)
	}
	if userText == "" || !strings.Contains(userText, "не умеешь") {
		t.Fatalf("expected unsupported-file prompt, got %q", userText)
	}
	if len(oa.uploadedFileIDs) != 0 {
		t.Fatalf("unsupported doc must not upload, got uploads %#v", oa.uploadedFileIDs)
	}
}

func TestHandleUpdate_BuildInputErrorFromDownload(t *testing.T) {
	tg := &fakeTG{allowed: true, downloadErr: errors.New("download failed")}
	oa := &fakeOA{}
	h := newTestHandler(tg, oa)

	update := tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: 2},
		Chat: &tgbotapi.Chat{ID: 1},
		Photo: []tgbotapi.PhotoSize{{
			FileID: "photo-err",
		}},
	}}

	h.HandleUpdate(update)

	if len(tg.sentTexts) != 1 || tg.sentTexts[0] != "Failed to process message. Please try again." {
		t.Fatalf("unexpected response on build input error: %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_TextResponseError(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{textErr: errors.New("openai down")}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "привет"))

	if len(tg.sentTexts) != 1 || tg.sentTexts[0] != "Model request failed." {
		t.Fatalf("expected model failure message, got %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_ImageGenerateError(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{
		classifyFn:    func(text string) bool { return true },
		textResponses: []string{"Ок"},
		generateErr:   errors.New("boom"),
	}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "нарисуй дом"))

	if len(tg.sentMessages) != 1 || tg.sentMessages[0] != "Генерирую изображение..." {
		t.Fatalf("unexpected status message: %#v", tg.sentMessages)
	}
	if len(tg.deleted) != 1 {
		t.Fatalf("expected status delete on image error, got %d", len(tg.deleted))
	}
	if tg.sentTexts[len(tg.sentTexts)-1] != "Image generation failed." {
		t.Fatalf("expected image generation failure text, got %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_ImageEditError(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{
		classifyFn:    func(text string) bool { return true },
		textResponses: []string{"Ок"},
		editErr:       errors.New("edit failed"),
	}
	h := newTestHandler(tg, oa)
	h.Store.SetLastImage(1, "data:image/jpeg;base64,AAAA")

	h.HandleUpdate(makeTextUpdate(1, 2, "убери объект"))

	if len(oa.editCalls) != 1 {
		t.Fatalf("expected edit call, got %d", len(oa.editCalls))
	}
	if tg.sentTexts[len(tg.sentTexts)-1] != "Редактирование изображения не удалось." {
		t.Fatalf("expected edit failure text, got %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_SendPhotoError(t *testing.T) {
	tg := &fakeTG{allowed: true, sendPhotoErr: errors.New("telegram photo err")}
	oa := &fakeOA{
		classifyFn:    func(text string) bool { return true },
		textResponses: []string{"Ок"},
		generateResp:  []byte("img"),
	}
	h := newTestHandler(tg, oa)

	h.HandleUpdate(makeTextUpdate(1, 2, "нарисуй дом"))

	if tg.sentTexts[len(tg.sentTexts)-1] != "Failed to send image." {
		t.Fatalf("expected send photo failure text, got %#v", tg.sentTexts)
	}
}

func TestHandleUpdate_Commands(t *testing.T) {
	tg := &fakeTG{allowed: true}
	oa := &fakeOA{}
	h := newTestHandler(tg, oa)

	h.Store.Append(1, memory.Message{Role: "user", Text: "keep"}, 10)
	h.HandleUpdate(makeCommandUpdate(1, 2, "reset"))
	if len(h.Store.Get(1)) != 0 {
		t.Fatal("/reset should clear history")
	}

	h.HandleUpdate(makeCommandUpdate(1, 2, "help"))
	h.HandleUpdate(makeCommandUpdate(1, 2, "unknown"))

	if len(tg.sentTexts) < 3 {
		t.Fatalf("expected command replies, got %#v", tg.sentTexts)
	}
	if tg.sentTexts[0] != "Context cleared." {
		t.Fatalf("unexpected /reset reply: %q", tg.sentTexts[0])
	}
	if tg.sentTexts[1] == "" || tg.sentTexts[2] != "Unknown command. /help" {
		t.Fatalf("unexpected help/unknown replies: %#v", tg.sentTexts)
	}
}
