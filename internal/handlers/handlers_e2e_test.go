package handlers

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestE2E_PhotoThenSeparateEditRequest(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"photo-1": {data: []byte("jpg-bytes"), filename: "photo.jpg"},
		},
	}
	oa := &fakeOA{
		classifyFn: func(text string) bool {
			return strings.Contains(text, "убери йогурт с фото")
		},
		textResponses: []string{
			"Вижу фото, готова помочь.",
			"Сейчас отредактирую.",
		},
		editResp: []byte("edited-image"),
	}
	h := newTestHandler(tg, oa)

	photoUpdate := tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: 2},
		Chat: &tgbotapi.Chat{ID: 1},
		Photo: []tgbotapi.PhotoSize{{
			FileID: "photo-1",
		}},
	}}
	h.HandleUpdate(photoUpdate)

	h.HandleUpdate(makeTextUpdate(1, 2, "убери йогурт с фото"))

	if len(oa.editCalls) != 1 {
		t.Fatalf("expected one edit call, got %d", len(oa.editCalls))
	}
	if len(oa.generateCalls) != 0 {
		t.Fatalf("expected no generate call, got %d", len(oa.generateCalls))
	}
	if len(tg.photos) != 1 {
		t.Fatalf("expected edited photo to be sent, got %d", len(tg.photos))
	}
	if len(tg.sentMessages) == 0 || tg.sentMessages[len(tg.sentMessages)-1] != "Редактирую изображение..." {
		t.Fatalf("expected edit status message, got %#v", tg.sentMessages)
	}
}

func TestE2E_VoiceThenTextFollowup(t *testing.T) {
	tg := &fakeTG{
		allowed: true,
		downloads: map[string]downloadedFile{
			"voice-1": {data: []byte("ogg-bytes"), filename: "voice.ogg"},
		},
	}
	oa := &fakeOA{
		classifyFn:     func(text string) bool { return false },
		transcribeResp: "привет",
		textResponses: []string{
			"Привет!",
			"И тебе привет!",
		},
	}
	h := newTestHandler(tg, oa)

	voiceUpdate := tgbotapi.Update{Message: &tgbotapi.Message{
		From:  &tgbotapi.User{ID: 2},
		Chat:  &tgbotapi.Chat{ID: 1},
		Voice: &tgbotapi.Voice{FileID: "voice-1"},
	}}
	h.HandleUpdate(voiceUpdate)
	h.HandleUpdate(makeTextUpdate(1, 2, "как дела"))

	if oa.transcribeCalled != 1 {
		t.Fatalf("expected one transcribe call, got %d", oa.transcribeCalled)
	}
	if len(tg.sentTexts) < 2 {
		t.Fatalf("expected at least two replies, got %#v", tg.sentTexts)
	}
	if tg.sentTexts[0] == "" || tg.sentTexts[1] == "" {
		t.Fatalf("unexpected empty replies: %#v", tg.sentTexts)
	}
	if len(h.Store.Get(1)) < 4 {
		t.Fatalf("expected conversation history to include both turns, got %#v", h.Store.Get(1))
	}
}
