package openaiwrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

func TestTextResponse_RetriesOnServerError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"type":    "message",
				"content": []any{map[string]any{"type": "output_text", "text": "ok"}},
			}},
		})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	text, err := c.TextResponse(context.Background(), c.BuildInput([]MessageInput{{Role: "user", Text: "hello"}}))
	if err != nil {
		t.Fatalf("TextResponse returned error: %v", err)
	}
	if text != "ok" {
		t.Fatalf("unexpected text: %q", text)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls with retries, got %d", calls)
	}
}

func TestGenerateImage_NoRetryOnBadRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	_, err := c.GenerateImage(context.Background(), "draw")
	if err == nil {
		t.Fatal("expected error for bad request")
	}
	if calls != 1 {
		t.Fatalf("expected no retries for 400, got %d calls", calls)
	}
}

func TestEditImage_RetriesOnTooManyRequests(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limit"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString([]byte("ok-edit"))}},
		})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("raw"))
	img, err := c.EditImage(context.Background(), "edit", dataURL)
	if err != nil {
		t.Fatalf("EditImage returned error: %v", err)
	}
	if string(img) != "ok-edit" {
		t.Fatalf("unexpected edited image: %q", string(img))
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls with retries, got %d", calls)
	}
}
