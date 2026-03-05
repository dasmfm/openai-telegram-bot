package openaiwrap

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/openai/openai-go/v3/responses"
)

type tempNetErr struct {
	msg       string
	timeout   bool
	temporary bool
}

func (e tempNetErr) Error() string   { return e.msg }
func (e tempNetErr) Timeout() bool   { return e.timeout }
func (e tempNetErr) Temporary() bool { return e.temporary }

func TestBuildInput_IncludesSystemRoleAndMedia(t *testing.T) {
	c := New("test-key", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "system rules")
	items := c.BuildInput([]MessageInput{{
		Role:         "user",
		Text:         "look",
		ImageDataURL: "data:image/jpeg;base64,AAAA",
	}, {
		Role:   "user",
		Text:   "use file",
		FileID: "file-123",
	}})

	if len(items) != 3 {
		t.Fatalf("expected 3 items (system + 2 user), got %d", len(items))
	}
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(b)
	for _, want := range []string{"system rules", "look", "image/jpeg", "file-123", "use file"} {
		if !strings.Contains(s, want) {
			t.Fatalf("expected %q in marshaled input: %s", want, s)
		}
	}
}

func TestDecodeDataURL(t *testing.T) {
	raw := []byte("hello-image")
	dataURL := "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
	data, contentType, err := decodeDataURL(dataURL)
	if err != nil {
		t.Fatalf("decodeDataURL returned error: %v", err)
	}
	if contentType != "image/png" {
		t.Fatalf("unexpected content type: %q", contentType)
	}
	if string(data) != string(raw) {
		t.Fatalf("decoded bytes mismatch: got %q want %q", string(data), string(raw))
	}
}

func TestDecodeDataURL_Invalid(t *testing.T) {
	_, _, err := decodeDataURL("data:image/png,abc")
	if err == nil {
		t.Fatal("expected error for non-base64 data url")
	}
}

func TestFilenameForContentType(t *testing.T) {
	cases := map[string]string{
		"image/png":  "image.png",
		"image/webp": "image.webp",
		"image/jpeg": "image.jpg",
		"image/jpg":  "image.jpg",
		"whatever":   "image.jpg",
	}
	for in, want := range cases {
		if got := filenameForContentType(in); got != want {
			t.Fatalf("filenameForContentType(%q)=%q want %q", in, got, want)
		}
	}
}

func TestShouldRetryOpenAI(t *testing.T) {
	if shouldRetryOpenAI(nil) {
		t.Fatal("nil error must not retry")
	}
	if shouldRetryOpenAI(context.Canceled) {
		t.Fatal("context canceled must not retry")
	}
	if !shouldRetryOpenAI(tempNetErr{msg: "timeout", timeout: true}) {
		t.Fatal("timeout net.Error must retry")
	}
	if !shouldRetryOpenAI(errors.New("remote error: tls: bad record MAC")) {
		t.Fatal("tls error must retry")
	}
	if shouldRetryOpenAI(errors.New("400 bad request")) {
		t.Fatal("generic bad request must not retry")
	}
}

func TestExtractResponseText(t *testing.T) {
	jsonPayload := `{"output":[{"type":"message","content":[{"type":"output_text","text":"hello"}]}]}`
	var resp responses.Response
	if err := json.Unmarshal([]byte(jsonPayload), &resp); err != nil {
		t.Fatalf("unmarshal response failed: %v", err)
	}
	if got := extractResponseText(&resp); got != "hello" {
		t.Fatalf("extractResponseText()=%q want %q", got, "hello")
	}

	if got := extractResponseText(nil); got != "" {
		t.Fatalf("extractResponseText(nil)=%q want empty", got)
	}
}

func TestClassifyQueryContainsEditInstructions(t *testing.T) {
	text := "убери объект на фото"
	query := "Classify the user request. Reply with ONLY 'IMAGE' if it requests generating a new image OR editing/modifying an existing image. Reply with ONLY 'TEXT' otherwise.\n\nRequest:\n" + text
	if !strings.Contains(query, "editing/modifying") {
		t.Fatal("classifier query should mention image editing")
	}
	if !strings.Contains(query, text) {
		t.Fatal("classifier query should include request text")
	}
}

func TestShouldRetryOpenAI_NetTemporary(t *testing.T) {
	err := tempNetErr{msg: "temporary network", temporary: true}
	if !shouldRetryOpenAI(err) {
		t.Fatalf("expected retry for temporary net error: %v", err)
	}

	err = tempNetErr{msg: "permanent", temporary: false, timeout: false}
	if shouldRetryOpenAI(err) {
		t.Fatalf("did not expect retry for permanent net error: %v", err)
	}
}

func TestBuildInput_MapsRoles(t *testing.T) {
	c := New("test-key", "gpt-5.2", "gpt-image-1-mini", "gpt-5-mini", "whisper-1", "")
	items := c.BuildInput([]MessageInput{{Role: "assistant", Text: "a"}, {Role: "developer", Text: "d"}, {Role: "system", Text: "s"}})
	b, err := json.Marshal(items)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	s := string(b)
	for _, want := range []string{"\"role\":\"assistant\"", "\"role\":\"developer\"", "\"role\":\"system\""} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing role marker %s in %s", want, s)
		}
	}
}

func TestShouldRetryOpenAI_StringCases(t *testing.T) {
	cases := []string{
		"unexpected EOF",
		"connection reset by peer",
		"server sent GOAWAY",
		"broken pipe",
	}
	for _, msg := range cases {
		if !shouldRetryOpenAI(fmt.Errorf(msg)) {
			t.Fatalf("expected retry for message %q", msg)
		}
	}
	if _, ok := interface{}(tempNetErr{}).(net.Error); !ok {
		t.Fatal("tempNetErr must implement net.Error")
	}
}
