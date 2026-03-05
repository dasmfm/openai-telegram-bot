package memory

import (
	"sync"
	"time"
)

type Message struct {
	Role         string
	Text         string
	ImageDataURL string
	FileData     string
	FileName     string
}

type Store struct {
	mu        sync.Mutex
	data      map[int64][]Message
	lastImage map[int64]ImageSnapshot
}

type ImageSnapshot struct {
	DataURL string
	At      time.Time
}

func NewStore() *Store {
	return &Store{
		data:      make(map[int64][]Message),
		lastImage: make(map[int64]ImageSnapshot),
	}
}

func (s *Store) Get(chatID int64) []Message {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := s.data[chatID]
	if len(msgs) == 0 {
		return nil
	}

	copyMsgs := make([]Message, len(msgs))
	copy(copyMsgs, msgs)
	return copyMsgs
}

func (s *Store) Append(chatID int64, msg Message, max int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	msgs := append(s.data[chatID], msg)
	if max > 0 && len(msgs) > max {
		msgs = msgs[len(msgs)-max:]
	}
	s.data[chatID] = msgs
}

func (s *Store) Reset(chatID int64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.data, chatID)
	delete(s.lastImage, chatID)
}

func (s *Store) SetLastImage(chatID int64, dataURL string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if dataURL == "" {
		delete(s.lastImage, chatID)
		return
	}
	s.lastImage[chatID] = ImageSnapshot{DataURL: dataURL, At: time.Now()}
}

func (s *Store) ConsumeLastImage(chatID int64, maxAge time.Duration) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	snap, ok := s.lastImage[chatID]
	if !ok {
		return "", false
	}
	if maxAge > 0 && time.Since(snap.At) > maxAge {
		delete(s.lastImage, chatID)
		return "", false
	}
	delete(s.lastImage, chatID)
	return snap.DataURL, true
}
