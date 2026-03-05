package openaiwrap

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
	"github.com/openai/openai-go/v3/responses"
)

type Client struct {
	inner           *openai.Client
	model           string
	imageModel      string
	routerModel     string
	roles           []responses.ResponseInputItemUnionParam
	transcribeModel string
}

type MessageInput struct {
	Role         string
	Text         string
	ImageDataURL string
	FileData     string
	FileID       string
	FileName     string
}

func New(apiKey string, model string, imageModel string, routerModel string, transcribeModel string, systemPrompt string) *Client {
	return NewWithOptions(apiKey, model, imageModel, routerModel, transcribeModel, systemPrompt)
}

func NewWithOptions(apiKey string, model string, imageModel string, routerModel string, transcribeModel string, systemPrompt string, requestOptions ...option.RequestOption) *Client {
	opts := make([]option.RequestOption, 0, len(requestOptions)+1)
	opts = append(opts, option.WithAPIKey(apiKey))
	opts = append(opts, requestOptions...)
	client := openai.NewClient(opts...)

	var roles []responses.ResponseInputItemUnionParam
	if strings.TrimSpace(systemPrompt) != "" {
		roles = append(roles, responses.ResponseInputItemParamOfMessage(systemPrompt, responses.EasyInputMessageRoleSystem))
	}

	return &Client{inner: &client, model: model, imageModel: imageModel, routerModel: routerModel, roles: roles, transcribeModel: transcribeModel}
}

func (c *Client) BuildInput(messages []MessageInput) []responses.ResponseInputItemUnionParam {
	items := make([]responses.ResponseInputItemUnionParam, 0, len(messages)+len(c.roles))
	items = append(items, c.roles...)

	for _, msg := range messages {
		role := responses.EasyInputMessageRoleUser
		switch strings.ToLower(msg.Role) {
		case "assistant":
			role = responses.EasyInputMessageRoleAssistant
		case "system":
			role = responses.EasyInputMessageRoleSystem
		case "developer":
			role = responses.EasyInputMessageRoleDeveloper
		case "user":
			role = responses.EasyInputMessageRoleUser
		}

		if msg.ImageDataURL != "" || msg.FileData != "" || msg.FileID != "" {
			content := responses.ResponseInputMessageContentListParam{}
			if strings.TrimSpace(msg.Text) != "" {
				content = append(content, responses.ResponseInputContentUnionParam{
					OfInputText: &responses.ResponseInputTextParam{Text: msg.Text},
				})
			}
			if msg.ImageDataURL != "" {
				content = append(content, responses.ResponseInputContentUnionParam{
					OfInputImage: &responses.ResponseInputImageParam{
						ImageURL: openai.String(msg.ImageDataURL),
						Detail:   responses.ResponseInputImageDetailAuto,
					},
				})
			}
			if msg.FileID != "" {
				content = append(content, responses.ResponseInputContentUnionParam{
					OfInputFile: &responses.ResponseInputFileParam{
						FileID: openai.String(msg.FileID),
					},
				})
			} else if msg.FileData != "" {
				content = append(content, responses.ResponseInputContentUnionParam{
					OfInputFile: &responses.ResponseInputFileParam{
						FileData: openai.String(msg.FileData),
						Filename: openai.String(msg.FileName),
					},
				})
			}
			items = append(items, responses.ResponseInputItemParamOfMessage(content, role))
			continue
		}

		items = append(items, responses.ResponseInputItemParamOfMessage(msg.Text, role))
	}

	return items
}

func (c *Client) TextResponse(ctx context.Context, input []responses.ResponseInputItemUnionParam) (string, error) {
	start := time.Now()
	log.Printf("openai text start: model=%s items=%d", c.model, len(input))
	params := responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{OfInputItemList: input},
		Model: openai.ChatModel(c.model),
	}
	var resp *responses.Response
	var err error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err = c.inner.Responses.New(ctx, params)
		if err == nil {
			break
		}
		if !shouldRetryOpenAI(err) || attempt == 2 {
			log.Printf("openai text error: model=%s ms=%d err=%v", c.model, time.Since(start).Milliseconds(), err)
			return "", err
		}
		wait := time.Duration(attempt+1) * 500 * time.Millisecond
		log.Printf("openai text retry: model=%s attempt=%d wait_ms=%d err=%v", c.model, attempt+1, wait.Milliseconds(), err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}
	text := strings.TrimSpace(extractResponseText(resp))
	if text == "" {
		log.Printf("openai text empty: model=%s ms=%d", c.model, time.Since(start).Milliseconds())
		return "", errors.New("empty response from model")
	}
	log.Printf("openai text done: model=%s ms=%d chars=%d", c.model, time.Since(start).Milliseconds(), len(text))
	return text, nil
}

func (c *Client) Transcribe(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	start := time.Now()
	log.Printf("openai transcribe start: model=%s bytes=%d", c.transcribeModel, len(data))
	reader := bytes.NewReader(data)
	file := openai.File(reader, filename, contentType)
	var resp *openai.AudioTranscriptionNewResponseUnion
	var err error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err = c.inner.Audio.Transcriptions.New(ctx, openai.AudioTranscriptionNewParams{
			Model: openai.AudioModel(c.transcribeModel),
			File:  file,
		})
		if err == nil {
			break
		}
		if !shouldRetryOpenAI(err) || attempt == 2 {
			log.Printf("openai transcribe error: model=%s ms=%d err=%v", c.transcribeModel, time.Since(start).Milliseconds(), err)
			return "", err
		}
		wait := time.Duration(attempt+1) * 500 * time.Millisecond
		log.Printf("openai transcribe retry: model=%s attempt=%d wait_ms=%d err=%v", c.transcribeModel, attempt+1, wait.Milliseconds(), err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}

	text := strings.TrimSpace(resp.AsTranscription().Text)
	if text == "" {
		text = strings.TrimSpace(resp.AsTranscriptionVerbose().Text)
	}
	if text == "" {
		log.Printf("openai transcribe empty: model=%s ms=%d", c.transcribeModel, time.Since(start).Milliseconds())
		return "", fmt.Errorf("empty transcription")
	}
	log.Printf("openai transcribe done: model=%s ms=%d chars=%d", c.transcribeModel, time.Since(start).Milliseconds(), len(text))
	return text, nil
}

func (c *Client) UploadFile(ctx context.Context, data []byte, filename string, contentType string) (string, error) {
	start := time.Now()
	log.Printf("openai file upload start: bytes=%d name=%s", len(data), filename)
	reader := bytes.NewReader(data)
	file := openai.File(reader, filename, contentType)
	var res *openai.FileObject
	var err error
	for attempt := 0; attempt <= 2; attempt++ {
		res, err = c.inner.Files.New(ctx, openai.FileNewParams{
			File:    file,
			Purpose: openai.FilePurposeUserData,
		})
		if err == nil {
			break
		}
		if !shouldRetryOpenAI(err) || attempt == 2 {
			log.Printf("openai file upload error: ms=%d err=%v", time.Since(start).Milliseconds(), err)
			return "", err
		}
		wait := time.Duration(attempt+1) * 500 * time.Millisecond
		log.Printf("openai file upload retry: attempt=%d wait_ms=%d err=%v", attempt+1, wait.Milliseconds(), err)
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(wait):
		}
	}
	if res.ID == "" {
		log.Printf("openai file upload empty id: ms=%d", time.Since(start).Milliseconds())
		return "", fmt.Errorf("empty file id")
	}
	log.Printf("openai file upload done: ms=%d id=%s", time.Since(start).Milliseconds(), res.ID)
	return res.ID, nil
}

func (c *Client) DeleteFile(ctx context.Context, fileID string) error {
	if strings.TrimSpace(fileID) == "" {
		return nil
	}
	start := time.Now()
	log.Printf("openai file delete start: id=%s", fileID)
	_, err := c.inner.Files.Delete(ctx, fileID)
	if err != nil {
		log.Printf("openai file delete error: ms=%d err=%v", time.Since(start).Milliseconds(), err)
		return err
	}
	log.Printf("openai file delete done: ms=%d", time.Since(start).Milliseconds())
	return nil
}

func (c *Client) GenerateImage(ctx context.Context, prompt string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("empty image prompt")
	}
	start := time.Now()
	log.Printf("openai image start: model=%s", c.imageModel)
	var resp *openai.ImagesResponse
	var err error
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err = c.inner.Images.Generate(ctx, openai.ImageGenerateParams{
			Prompt: prompt,
			Model:  openai.ImageModel(c.imageModel),
			N:      openai.Int(1),
		})
		if err == nil {
			break
		}
		if !shouldRetryOpenAI(err) || attempt == 2 {
			log.Printf("openai image error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		wait := time.Duration(attempt+1) * 500 * time.Millisecond
		log.Printf("openai image retry: model=%s attempt=%d wait_ms=%d err=%v", c.imageModel, attempt+1, wait.Milliseconds(), err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	if len(resp.Data) == 0 {
		log.Printf("openai image empty data: model=%s ms=%d", c.imageModel, time.Since(start).Milliseconds())
		return nil, fmt.Errorf("empty image response")
	}
	if strings.TrimSpace(resp.Data[0].B64JSON) != "" {
		data, err := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
		if err != nil {
			log.Printf("openai image decode error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		log.Printf("openai image done: model=%s ms=%d bytes=%d", c.imageModel, time.Since(start).Milliseconds(), len(data))
		return data, nil
	}
	if strings.TrimSpace(resp.Data[0].URL) != "" {
		data, err := downloadImage(ctx, resp.Data[0].URL)
		if err != nil {
			log.Printf("openai image download error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		log.Printf("openai image done: model=%s ms=%d bytes=%d", c.imageModel, time.Since(start).Milliseconds(), len(data))
		return data, nil
	}
	log.Printf("openai image empty response: model=%s ms=%d", c.imageModel, time.Since(start).Milliseconds())
	return nil, fmt.Errorf("empty image response")
}

func (c *Client) EditImage(ctx context.Context, prompt string, imageDataURL string) ([]byte, error) {
	if strings.TrimSpace(prompt) == "" {
		return nil, fmt.Errorf("empty image prompt")
	}
	data, contentType, err := decodeDataURL(imageDataURL)
	if err != nil {
		return nil, err
	}
	start := time.Now()
	log.Printf("openai image edit start: model=%s bytes=%d", c.imageModel, len(data))
	reader := bytes.NewReader(data)
	filename := filenameForContentType(contentType)
	file := openai.File(reader, filename, contentType)
	var resp *openai.ImagesResponse
	for attempt := 0; attempt <= 2; attempt++ {
		resp, err = c.inner.Images.Edit(ctx, openai.ImageEditParams{
			Image: openai.ImageEditParamsImageUnion{
				OfFile: file,
			},
			Prompt: prompt,
			Model:  openai.ImageModel(c.imageModel),
		})
		if err == nil {
			break
		}
		if !shouldRetryOpenAI(err) || attempt == 2 {
			log.Printf("openai image edit error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		wait := time.Duration(attempt+1) * 500 * time.Millisecond
		log.Printf("openai image edit retry: model=%s attempt=%d wait_ms=%d err=%v", c.imageModel, attempt+1, wait.Milliseconds(), err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(wait):
		}
	}
	if len(resp.Data) == 0 {
		log.Printf("openai image edit empty data: model=%s ms=%d", c.imageModel, time.Since(start).Milliseconds())
		return nil, fmt.Errorf("empty image response")
	}
	if strings.TrimSpace(resp.Data[0].B64JSON) != "" {
		data, err := base64.StdEncoding.DecodeString(resp.Data[0].B64JSON)
		if err != nil {
			log.Printf("openai image edit decode error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		log.Printf("openai image edit done: model=%s ms=%d bytes=%d", c.imageModel, time.Since(start).Milliseconds(), len(data))
		return data, nil
	}
	if strings.TrimSpace(resp.Data[0].URL) != "" {
		data, err := downloadImage(ctx, resp.Data[0].URL)
		if err != nil {
			log.Printf("openai image edit download error: model=%s ms=%d err=%v", c.imageModel, time.Since(start).Milliseconds(), err)
			return nil, err
		}
		log.Printf("openai image edit done: model=%s ms=%d bytes=%d", c.imageModel, time.Since(start).Milliseconds(), len(data))
		return data, nil
	}
	log.Printf("openai image edit empty response: model=%s ms=%d", c.imageModel, time.Since(start).Milliseconds())
	return nil, fmt.Errorf("empty image response")
}

func downloadImage(ctx context.Context, url string) ([]byte, error) {
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("image download status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	log.Printf("openai image download done: ms=%d bytes=%d", time.Since(start).Milliseconds(), len(data))
	return data, nil
}

func decodeDataURL(dataURL string) ([]byte, string, error) {
	dataURL = strings.TrimSpace(dataURL)
	if dataURL == "" {
		return nil, "", fmt.Errorf("empty data url")
	}
	if !strings.HasPrefix(dataURL, "data:") {
		return nil, "", fmt.Errorf("invalid data url")
	}
	parts := strings.SplitN(dataURL, ",", 2)
	if len(parts) != 2 {
		return nil, "", fmt.Errorf("invalid data url")
	}
	meta := parts[0]
	if !strings.Contains(meta, ";base64") {
		return nil, "", fmt.Errorf("invalid data url")
	}
	data, err := base64.StdEncoding.DecodeString(parts[1])
	if err != nil {
		return nil, "", err
	}
	contentType := strings.TrimPrefix(meta, "data:")
	contentType = strings.TrimSuffix(contentType, ";base64")
	if contentType == "" {
		contentType = "image/jpeg"
	}
	return data, contentType, nil
}

func shouldRetryOpenAI(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var netErr net.Error
	if errors.As(err, &netErr) {
		return netErr.Timeout() || netErr.Temporary()
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "tls:"):
		return true
	case strings.Contains(msg, "unexpected eof"):
		return true
	case strings.Contains(msg, "connection reset"):
		return true
	case strings.Contains(msg, "connection refused"):
		return true
	case strings.Contains(msg, "connection aborted"):
		return true
	case strings.Contains(msg, "bad record mac"):
		return true
	case strings.Contains(msg, "broken pipe"):
		return true
	case strings.Contains(msg, "server sent goaway"):
		return true
	case strings.Contains(msg, "429 too many requests"):
		return true
	case strings.Contains(msg, "500 internal server error"):
		return true
	case strings.Contains(msg, "502 bad gateway"):
		return true
	case strings.Contains(msg, "503 service unavailable"):
		return true
	case strings.Contains(msg, "504 gateway timeout"):
		return true
	default:
		return false
	}
}

func filenameForContentType(contentType string) string {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png":
		return "image.png"
	case "image/webp":
		return "image.webp"
	case "image/jpeg", "image/jpg":
		return "image.jpg"
	default:
		return "image.jpg"
	}
}

func (c *Client) ClassifyImageRequest(ctx context.Context, text string) (bool, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return false, nil
	}
	start := time.Now()
	log.Printf("openai router start: model=%s", c.routerModel)
	query := "Classify the user request. Reply with ONLY 'IMAGE' if it requests generating a new image OR editing/modifying an existing image. Reply with ONLY 'TEXT' otherwise.\n\nRequest:\n" + text
	params := responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(query, responses.EasyInputMessageRoleUser),
			},
		},
		Model: openai.ChatModel(c.routerModel),
	}

	resp, err := c.inner.Responses.New(ctx, params)
	if err != nil {
		log.Printf("openai router error: model=%s ms=%d err=%v", c.routerModel, time.Since(start).Milliseconds(), err)
		return false, err
	}
	answer := strings.TrimSpace(extractResponseText(resp))
	log.Printf("openai router done: model=%s ms=%d answer=%q", c.routerModel, time.Since(start).Milliseconds(), answer)
	return strings.EqualFold(answer, "IMAGE"), nil
}

func (c *Client) GuardImageAction(ctx context.Context, prompt string, isEdit bool) (bool, string, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return true, "", nil
	}
	action := "generate"
	if isEdit {
		action = "edit"
	}
	query := "You are a policy gate for image actions. Return ONLY valid JSON with keys allow_image_action (boolean) and reply (string). " +
		"allow_image_action=false if request should be refused. reply must be a short friendly Russian sentence for the user. " +
		"No markdown, no extra text.\n\nAction: " + action + "\nUser request:\n" + prompt
	params := responses.ResponseNewParams{
		Input: responses.ResponseNewParamsInputUnion{
			OfInputItemList: responses.ResponseInputParam{
				responses.ResponseInputItemParamOfMessage(query, responses.EasyInputMessageRoleUser),
			},
		},
		Model: openai.ChatModel(c.routerModel),
	}

	start := time.Now()
	log.Printf("openai image-guard start: model=%s action=%s", c.routerModel, action)
	resp, err := c.inner.Responses.New(ctx, params)
	if err != nil {
		log.Printf("openai image-guard error: model=%s ms=%d err=%v", c.routerModel, time.Since(start).Milliseconds(), err)
		return true, "", err
	}
	raw := strings.TrimSpace(extractResponseText(resp))
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)
	var out struct {
		Allow bool   `json:"allow_image_action"`
		Reply string `json:"reply"`
	}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		log.Printf("openai image-guard parse error: model=%s ms=%d raw=%q err=%v", c.routerModel, time.Since(start).Milliseconds(), raw, err)
		return true, "", err
	}
	log.Printf("openai image-guard done: model=%s ms=%d allow=%t", c.routerModel, time.Since(start).Milliseconds(), out.Allow)
	return out.Allow, strings.TrimSpace(out.Reply), nil
}

func extractResponseText(resp *responses.Response) string {
	if resp == nil {
		return ""
	}
	for _, output := range resp.Output {
		if output.Type != "message" {
			continue
		}
		for _, c := range output.Content {
			if c.Type == "output_text" && strings.TrimSpace(c.Text) != "" {
				return c.Text
			}
		}
	}
	return ""
}
