package openaiwrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

func TestTextResponse_NoRetryOnBadRequest(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":{"message":"bad request"}}`))
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	_, err := c.TextResponse(context.Background(), c.BuildInput([]MessageInput{{Role: "user", Text: "hello"}}))
	if err == nil {
		t.Fatal("expected bad request error")
	}
	if calls != 1 {
		t.Fatalf("expected no retries for 400, got %d calls", calls)
	}
}

func TestTextResponse_EmptyOutput(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"output": []any{}})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	_, err := c.TextResponse(context.Background(), c.BuildInput([]MessageInput{{Role: "user", Text: "hello"}}))
	if err == nil {
		t.Fatal("expected empty response error")
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

func TestGenerateImage_UsesURLBranch(t *testing.T) {
	imgServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("url-image"))
	}))
	defer imgServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": []any{map[string]any{"url": imgServer.URL}},
		})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	data, err := c.GenerateImage(context.Background(), "draw")
	if err != nil {
		t.Fatalf("GenerateImage returned error: %v", err)
	}
	if string(data) != "url-image" {
		t.Fatalf("unexpected data: %q", string(data))
	}
}

func TestGenerateImage_EdgeErrors(t *testing.T) {
	t.Run("empty prompt", func(t *testing.T) {
		c := New("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "")
		if _, err := c.GenerateImage(context.Background(), " "); err == nil {
			t.Fatal("expected empty prompt error")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		}))
		defer server.Close()
		c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
		if _, err := c.GenerateImage(context.Background(), "draw"); err == nil || !strings.Contains(err.Error(), "empty image response") {
			t.Fatalf("expected empty image response error, got %v", err)
		}
	})

	t.Run("bad b64", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{map[string]any{"b64_json": "***not-b64***"}},
			})
		}))
		defer server.Close()
		c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
		if _, err := c.GenerateImage(context.Background(), "draw"); err == nil {
			t.Fatal("expected decode error")
		}
	})

	t.Run("empty response object", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{map[string]any{}},
			})
		}))
		defer server.Close()
		c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
		if _, err := c.GenerateImage(context.Background(), "draw"); err == nil || !strings.Contains(err.Error(), "empty image response") {
			t.Fatalf("expected empty image response error, got %v", err)
		}
	})
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

func TestEditImage_EdgeErrors(t *testing.T) {
	t.Run("empty prompt", func(t *testing.T) {
		c := New("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "")
		if _, err := c.EditImage(context.Background(), "", "data:image/jpeg;base64,AAAA"); err == nil {
			t.Fatal("expected empty prompt error")
		}
	})

	t.Run("invalid data url", func(t *testing.T) {
		c := New("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "")
		if _, err := c.EditImage(context.Background(), "edit", "bad-data-url"); err == nil {
			t.Fatal("expected invalid data url error")
		}
	})

	t.Run("empty data", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{}})
		}))
		defer server.Close()
		c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
		dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("raw"))
		if _, err := c.EditImage(context.Background(), "edit", dataURL); err == nil || !strings.Contains(err.Error(), "empty image response") {
			t.Fatalf("expected empty image response error, got %v", err)
		}
	})

	t.Run("bad b64", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []any{map[string]any{"b64_json": "***not-b64***"}},
			})
		}))
		defer server.Close()
		c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
		dataURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("raw"))
		if _, err := c.EditImage(context.Background(), "edit", dataURL); err == nil {
			t.Fatal("expected decode error")
		}
	})
}

func TestTranscribe_RetriesOnServerError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/audio/transcriptions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if calls < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": "voice ok"})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	text, err := c.Transcribe(context.Background(), []byte("ogg"), "voice.ogg", "audio/ogg")
	if err != nil {
		t.Fatalf("Transcribe returned error: %v", err)
	}
	if text != "voice ok" {
		t.Fatalf("unexpected text: %q", text)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls with retries, got %d", calls)
	}
}

func TestUploadFile_RetriesOnServerError(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Path != "/files" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if calls < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "file-ok"})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	id, err := c.UploadFile(context.Background(), []byte("data"), "note.txt", "text/plain")
	if err != nil {
		t.Fatalf("UploadFile returned error: %v", err)
	}
	if id != "file-ok" {
		t.Fatalf("unexpected file id: %q", id)
	}
	if calls != 3 {
		t.Fatalf("expected 3 calls with retries, got %d", calls)
	}
}

func TestUploadFile_EmptyID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"id": ""})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	if _, err := c.UploadFile(context.Background(), []byte("data"), "note.txt", "text/plain"); err == nil || !strings.Contains(err.Error(), "empty file id") {
		t.Fatalf("expected empty file id error, got %v", err)
	}
}

func TestTranscribe_EmptyText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"text": ""})
	}))
	defer server.Close()

	c := NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	if _, err := c.Transcribe(context.Background(), []byte("ogg"), "voice.ogg", "audio/ogg"); err == nil || !strings.Contains(err.Error(), "empty transcription") {
		t.Fatalf("expected empty transcription error, got %v", err)
	}
}

func TestDeleteFile_EdgeCases(t *testing.T) {
	c := New("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "")
	if err := c.DeleteFile(context.Background(), " "); err != nil {
		t.Fatalf("empty file id should be no-op, got %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
	}))
	defer server.Close()
	c = NewWithOptions("k", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "", option.WithBaseURL(server.URL+"/"), option.WithHTTPClient(server.Client()))
	if err := c.DeleteFile(context.Background(), "file-1"); err == nil {
		t.Fatal("expected delete error")
	}
}
