package openaiwrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/option"
)

func TestClientHTTPEndpoints(t *testing.T) {
	type state struct {
		responsesCount   int
		genCount         int
		editCount        int
		uploadCount      int
		deleteCount      int
		transcribeCount  int
		sawEditPrompt    bool
		sawEditImagePNG  bool
		sawUploadPurpose bool
	}
	s := &state{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON := func(v any) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(v)
		}

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/responses":
			s.responsesCount++
			body, _ := io.ReadAll(r.Body)
			text := "hello from model"
			if strings.Contains(string(body), "Classify the user request") {
				text = "IMAGE"
			}
			if strings.Contains(string(body), "allow_image_action") {
				text = `{"allow_image_action":true,"reply":"ok"}`
			}
			writeJSON(map[string]any{
				"output": []any{map[string]any{
					"type": "message",
					"content": []any{map[string]any{
						"type": "output_text",
						"text": text,
					}},
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/images/generations":
			s.genCount++
			writeJSON(map[string]any{
				"data": []any{map[string]any{
					"b64_json": base64.StdEncoding.EncodeToString([]byte("generated-image")),
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/images/edits":
			s.editCount++
			if err := r.ParseMultipartForm(2 << 20); err == nil && r.MultipartForm != nil {
				if vals := r.MultipartForm.Value["prompt"]; len(vals) > 0 && vals[0] == "edit prompt" {
					s.sawEditPrompt = true
				}
				if files := r.MultipartForm.File["image"]; len(files) > 0 {
					if strings.Contains(strings.ToLower(files[0].Header.Get("Content-Type")), "image/png") {
						s.sawEditImagePNG = true
					}
				}
			}
			writeJSON(map[string]any{
				"data": []any{map[string]any{
					"b64_json": base64.StdEncoding.EncodeToString([]byte("edited-image")),
				}},
			})
		case r.Method == http.MethodPost && r.URL.Path == "/audio/transcriptions":
			s.transcribeCount++
			writeJSON(map[string]any{"text": "voice text"})
		case r.Method == http.MethodPost && r.URL.Path == "/files":
			s.uploadCount++
			if err := r.ParseMultipartForm(2 << 20); err == nil && r.MultipartForm != nil {
				if vals := r.MultipartForm.Value["purpose"]; len(vals) > 0 && vals[0] != "" {
					s.sawUploadPurpose = true
				}
			}
			writeJSON(map[string]any{"id": "file-123"})
		case r.Method == http.MethodDelete && r.URL.Path == "/files/file-123":
			s.deleteCount++
			writeJSON(map[string]any{"id": "file-123", "deleted": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.Path)
		}
	}))
	defer server.Close()

	c := NewWithOptions(
		"test-key",
		"gpt-5.2",
		"gpt-image-1-mini",
		"gpt-5-mini",
		"whisper-1",
		"system",
		option.WithBaseURL(server.URL+"/"),
		option.WithHTTPClient(server.Client()),
	)
	ctx := context.Background()

	text, err := c.TextResponse(ctx, c.BuildInput([]MessageInput{{Role: "user", Text: "hello"}}))
	if err != nil || text != "hello from model" {
		t.Fatalf("TextResponse failed: text=%q err=%v", text, err)
	}

	wantImage, err := c.ClassifyImageRequest(ctx, "нарисуй кота")
	if err != nil || !wantImage {
		t.Fatalf("ClassifyImageRequest failed: wantImage=%v err=%v", wantImage, err)
	}

	allowed, reply, err := c.GuardImageAction(ctx, "нарисуй кота", false)
	if err != nil || !allowed || reply != "ok" {
		t.Fatalf("GuardImageAction failed: allowed=%v reply=%q err=%v", allowed, reply, err)
	}

	gen, err := c.GenerateImage(ctx, "generate prompt")
	if err != nil || string(gen) != "generated-image" {
		t.Fatalf("GenerateImage failed: bytes=%q err=%v", string(gen), err)
	}

	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("orig-image"))
	edited, err := c.EditImage(ctx, "edit prompt", dataURL)
	if err != nil || string(edited) != "edited-image" {
		t.Fatalf("EditImage failed: bytes=%q err=%v", string(edited), err)
	}

	tr, err := c.Transcribe(ctx, []byte("ogg"), "voice.ogg", "audio/ogg")
	if err != nil || tr != "voice text" {
		t.Fatalf("Transcribe failed: text=%q err=%v", tr, err)
	}

	id, err := c.UploadFile(ctx, []byte("file"), "note.txt", "text/plain")
	if err != nil || id != "file-123" {
		t.Fatalf("UploadFile failed: id=%q err=%v", id, err)
	}
	if err := c.DeleteFile(ctx, id); err != nil {
		t.Fatalf("DeleteFile failed: %v", err)
	}

	if s.responsesCount != 3 {
		t.Fatalf("expected 3 /responses calls, got %d", s.responsesCount)
	}
	if s.genCount != 1 || s.editCount != 1 || s.transcribeCount != 1 || s.uploadCount != 1 || s.deleteCount != 1 {
		t.Fatalf("unexpected endpoint counters: %+v", s)
	}
	if !s.sawEditPrompt || !s.sawEditImagePNG {
		t.Fatalf("expected edit multipart fields to be present, got %+v", s)
	}
	if !s.sawUploadPurpose {
		t.Fatalf("expected upload purpose field, got %+v", s)
	}
}

func TestGuardImageAction_ParseError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/responses" {
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"output": []any{map[string]any{
				"type":    "message",
				"content": []any{map[string]any{"type": "output_text", "text": "not-json"}},
			}},
		})
	}))
	defer server.Close()

	c := NewWithOptions(
		"test-key",
		"gpt-5.2",
		"gpt-image-1-mini",
		"gpt-5-mini",
		"whisper-1",
		"system",
		option.WithBaseURL(server.URL+"/"),
		option.WithHTTPClient(server.Client()),
	)

	_, _, err := c.GuardImageAction(context.Background(), "remove watermark", true)
	if err == nil {
		t.Fatal("expected parse error from GuardImageAction")
	}
}
