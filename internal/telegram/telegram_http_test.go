package telegram

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type rewriteTelegramTransport struct {
	base   http.RoundTripper
	target *url.URL
}

func (r *rewriteTelegramTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	if cloned.URL.Host == "api.telegram.org" {
		cloned.URL.Scheme = r.target.Scheme
		cloned.URL.Host = r.target.Host
	}
	return r.base.RoundTrip(cloned)
}

func TestSendText_FallbackWithoutParseMode(t *testing.T) {
	var sendCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTESTTOKEN/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case "/botTESTTOKEN/sendMessage":
			sendCalls++
			body, _ := io.ReadAll(r.Body)
			vals, _ := url.ParseQuery(string(body))
			if vals.Get("parse_mode") == "HTML" {
				_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":10,"date":0,"chat":{"id":1,"type":"private"}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}
	c := &Client{bot: bot}

	if err := c.SendText(1, "<b>broken"); err != nil {
		t.Fatalf("SendText returned error: %v", err)
	}
	if sendCalls != 2 {
		t.Fatalf("expected fallback second send, got %d calls", sendCalls)
	}
}

func TestDownloadFile_SuccessAndTooLarge(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTESTTOKEN/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case "/botTESTTOKEN/getFile":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"abc","file_path":"photos/pic.jpg"}}`))
		case "/file/botTESTTOKEN/photos/pic.jpg":
			_, _ = w.Write([]byte("0123456789"))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	origDefaultTransport := http.DefaultTransport
	targetURL, _ := url.Parse(server.URL)
	http.DefaultTransport = &rewriteTelegramTransport{base: origDefaultTransport, target: targetURL}
	defer func() { http.DefaultTransport = origDefaultTransport }()

	bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}

	c := &Client{bot: bot, maxFileBytes: 20}
	data, name, err := c.DownloadFile(context.Background(), "abc")
	if err != nil {
		t.Fatalf("DownloadFile failed: %v", err)
	}
	if string(data) != "0123456789" || name != "pic.jpg" {
		t.Fatalf("unexpected download result: data=%q name=%q", string(data), name)
	}

	c.maxFileBytes = 5
	_, _, err = c.DownloadFile(context.Background(), "abc")
	if err == nil || !strings.Contains(err.Error(), "file too large") {
		t.Fatalf("expected too large error, got: %v", err)
	}
}

func TestDownloadFile_ErrorBranches(t *testing.T) {
	t.Run("getFile error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/botTESTTOKEN/getMe":
				_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
			case "/botTESTTOKEN/getFile":
				_, _ = w.Write([]byte(`{"ok":false,"description":"bad file id"}`))
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer server.Close()

		bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
		if err != nil {
			t.Fatalf("failed to init bot: %v", err)
		}
		c := &Client{bot: bot, maxFileBytes: 20}
		if _, _, err := c.DownloadFile(context.Background(), "bad"); err == nil {
			t.Fatal("expected getFile error")
		}
	})

	t.Run("download status error", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/botTESTTOKEN/getMe":
				_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
			case "/botTESTTOKEN/getFile":
				_, _ = w.Write([]byte(`{"ok":true,"result":{"file_id":"abc","file_path":"photos/pic.jpg"}}`))
			case "/file/botTESTTOKEN/photos/pic.jpg":
				w.WriteHeader(http.StatusBadGateway)
			default:
				t.Fatalf("unexpected path: %s", r.URL.Path)
			}
		}))
		defer server.Close()

		origDefaultTransport := http.DefaultTransport
		targetURL, _ := url.Parse(server.URL)
		http.DefaultTransport = &rewriteTelegramTransport{base: origDefaultTransport, target: targetURL}
		defer func() { http.DefaultTransport = origDefaultTransport }()

		bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
		if err != nil {
			t.Fatalf("failed to init bot: %v", err)
		}
		c := &Client{bot: bot, maxFileBytes: 20}
		if _, _, err := c.DownloadFile(context.Background(), "abc"); err == nil {
			t.Fatal("expected download status error")
		}
	})
}

func TestDeleteAndTyping(t *testing.T) {
	var deleteCalls, typingCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTESTTOKEN/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case "/botTESTTOKEN/deleteMessage":
			deleteCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		case "/botTESTTOKEN/sendChatAction":
			typingCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}
	c := &Client{bot: bot}

	if err := c.DeleteMessage(1, 10); err != nil {
		t.Fatalf("DeleteMessage returned error: %v", err)
	}
	c.Typing(1)

	if deleteCalls != 1 || typingCalls != 1 {
		t.Fatalf("unexpected call counts: delete=%d typing=%d", deleteCalls, typingCalls)
	}
}

func TestTyping_ErrorPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTESTTOKEN/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case "/botTESTTOKEN/sendChatAction":
			_, _ = w.Write([]byte(`{"ok":false,"description":"chat action failed"}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}
	c := &Client{bot: bot}

	// No panic and no return value; this just exercises error branch.
	c.Typing(1)
}

func TestBotGetter(t *testing.T) {
	raw := &tgbotapi.BotAPI{}
	c := &Client{bot: raw}
	if c.Bot() != raw {
		t.Fatal("Bot() should return underlying bot pointer")
	}
}

func TestSendMessageEditAndPhoto(t *testing.T) {
	var sendCalls, editCalls, photoCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/botTESTTOKEN/getMe":
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"bot","username":"bot"}}`))
		case "/botTESTTOKEN/sendMessage":
			sendCalls++
			body, _ := io.ReadAll(r.Body)
			vals, _ := url.ParseQuery(string(body))
			if sendCalls == 1 && vals.Get("parse_mode") == "HTML" {
				_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":1,"type":"private"}}}`))
		case "/botTESTTOKEN/editMessageText":
			editCalls++
			body, _ := io.ReadAll(r.Body)
			vals, _ := url.ParseQuery(string(body))
			if editCalls == 1 && vals.Get("parse_mode") == "HTML" {
				_, _ = w.Write([]byte(`{"ok":false,"description":"Bad Request: can't parse entities"}`))
				return
			}
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":77,"date":0,"chat":{"id":1,"type":"private"},"text":"ok"}}`))
		case "/botTESTTOKEN/sendPhoto":
			photoCalls++
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":88,"date":0,"chat":{"id":1,"type":"private"}}}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	bot, err := tgbotapi.NewBotAPIWithClient("TESTTOKEN", server.URL+"/bot%s/%s", server.Client())
	if err != nil {
		t.Fatalf("failed to init bot: %v", err)
	}
	c := &Client{bot: bot}

	msgID, err := c.SendMessage(1, "<b>hi")
	if err != nil {
		t.Fatalf("SendMessage returned error: %v", err)
	}
	if msgID != 77 {
		t.Fatalf("unexpected message id: %d", msgID)
	}
	if err := c.EditMessage(1, 77, "<i>broken"); err != nil {
		t.Fatalf("EditMessage returned error: %v", err)
	}
	if err := c.SendPhotoBytes(1, []byte("png")); err != nil {
		t.Fatalf("SendPhotoBytes returned error: %v", err)
	}
	if sendCalls != 2 {
		t.Fatalf("expected send fallback calls = 2, got %d", sendCalls)
	}
	if editCalls != 2 {
		t.Fatalf("expected edit fallback calls = 2, got %d", editCalls)
	}
	if photoCalls != 1 {
		t.Fatalf("expected one photo call, got %d", photoCalls)
	}
}
