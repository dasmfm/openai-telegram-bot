package memory

import (
	"testing"
	"time"
)

func TestStoreAppendTrimAndGetCopy(t *testing.T) {
	s := NewStore()
	chatID := int64(1)

	s.Append(chatID, Message{Role: "user", Text: "one"}, 2)
	s.Append(chatID, Message{Role: "assistant", Text: "two"}, 2)
	s.Append(chatID, Message{Role: "user", Text: "three"}, 2)

	msgs := s.Get(chatID)
	if len(msgs) != 2 {
		t.Fatalf("expected 2 messages after trim, got %d", len(msgs))
	}
	if msgs[0].Text != "two" || msgs[1].Text != "three" {
		t.Fatalf("unexpected trimmed history: %#v", msgs)
	}

	msgs[0].Text = "mutated"
	again := s.Get(chatID)
	if again[0].Text != "two" {
		t.Fatalf("Get should return a copy, got %#v", again)
	}
}

func TestStoreResetClearsHistoryAndImage(t *testing.T) {
	s := NewStore()
	chatID := int64(2)

	s.Append(chatID, Message{Role: "user", Text: "hello"}, 10)
	s.SetLastImage(chatID, "data:image/jpeg;base64,AAAA")
	s.Reset(chatID)

	if got := s.Get(chatID); len(got) != 0 {
		t.Fatalf("expected empty history after reset, got %#v", got)
	}
	if data, ok := s.ConsumeLastImage(chatID, time.Minute); ok || data != "" {
		t.Fatalf("expected no image after reset, got data=%q ok=%v", data, ok)
	}
}

func TestStoreConsumeLastImageTTL(t *testing.T) {
	s := NewStore()
	chatID := int64(3)

	s.SetLastImage(chatID, "data:image/png;base64,BBBB")
	data, ok := s.ConsumeLastImage(chatID, time.Minute)
	if !ok || data == "" {
		t.Fatalf("expected image snapshot, got data=%q ok=%v", data, ok)
	}

	s.SetLastImage(chatID, "data:image/png;base64,CCCC")
	time.Sleep(15 * time.Millisecond)
	data, ok = s.ConsumeLastImage(chatID, 1*time.Millisecond)
	if ok || data != "" {
		t.Fatalf("expected expired image snapshot, got data=%q ok=%v", data, ok)
	}
}

func TestStoreSetLastImageEmptyClearsValue(t *testing.T) {
	s := NewStore()
	chatID := int64(4)

	s.SetLastImage(chatID, "data:image/webp;base64,DDDD")
	s.SetLastImage(chatID, "")

	if data, ok := s.ConsumeLastImage(chatID, 0); ok || data != "" {
		t.Fatalf("expected image to be cleared, got data=%q ok=%v", data, ok)
	}
}
