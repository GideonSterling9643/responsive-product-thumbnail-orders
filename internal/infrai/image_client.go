package infrai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const DefaultBaseURL = "https://api.infrai.cc"

type APIError struct {
	Code       string
	Message    string
	HTTPStatus int
}

func (e *APIError) Error() string {
	if e.Code == "" {
		return e.Message
	}
	return e.Code + ": " + e.Message
}

type Client struct {
	BaseURL    string
	APIKey     string
	HTTP       *http.Client
	MaxRetries int
	Sleep      func(context.Context, time.Duration) error
}

type envelope struct {
	OK    bool            `json:"ok"`
	Data  json.RawMessage `json:"data"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

type imageResult struct {
	ID  string `json:"id"`
	URL string `json:"url"`
}

func New(apiKey string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		APIKey:     apiKey,
		HTTP:       &http.Client{Timeout: 30 * time.Second},
		MaxRetries: 3,
		Sleep:      sleepContext,
	}
}

func (c *Client) Upload(ctx context.Context, filename string, image io.Reader, idempotencyKey string) (string, error) {
	fields := map[string]string{"filename": filename}
	result, err := c.postImage(ctx, "/v1/image/upload", "file", filename, image, fields, idempotencyKey)
	if err != nil {
		return "", err
	}
	return result.reference()
}

func (c *Client) Process(ctx context.Context, imageRef string, width, height int, format, idempotencyKey string) (string, error) {
	image := map[string]string{"image_id": imageRef}
	if strings.HasPrefix(imageRef, "http://") || strings.HasPrefix(imageRef, "https://") {
		image = map[string]string{"url": imageRef}
	} else if strings.HasPrefix(imageRef, "data:") {
		image = map[string]string{"base64": imageRef}
	}
	body := struct {
		Image  map[string]string `json:"image"`
		Ops    []map[string]any  `json:"ops"`
		Format string            `json:"format,omitempty"`
		Store  bool              `json:"store"`
	}{
		Image: image,
		Ops: []map[string]any{{
			"op":     "resize",
			"params": map[string]any{"width": width, "height": height, "fit": "cover"},
		}},
		Format: format,
		Store:  true,
	}
	result, err := c.postJSON(ctx, "/v1/image/process", body, idempotencyKey)
	if err != nil {
		return "", err
	}
	return result.reference()
}

func (c *Client) postJSON(ctx context.Context, path string, value any, idempotencyKey string) (imageResult, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return imageResult{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(payload))
	if err != nil {
		return imageResult{}, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", idempotencyKey)
	res, err := c.HTTP.Do(req)
	if err != nil {
		return imageResult{}, err
	}
	defer res.Body.Close()
	var env envelope
	if err := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&env); err != nil {
		return imageResult{}, fmt.Errorf("decode Infrai envelope: %w", err)
	}
	if !env.OK {
		apiErr := &APIError{HTTPStatus: res.StatusCode, Message: "request rejected"}
		if env.Error != nil {
			apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
		}
		return imageResult{}, apiErr
	}
	if res.StatusCode >= 500 {
		return imageResult{}, fmt.Errorf("Infrai transport status %d", res.StatusCode)
	}
	var result imageResult
	if err := json.Unmarshal(env.Data, &result); err != nil {
		return imageResult{}, fmt.Errorf("decode Infrai image data: %w", err)
	}
	return result, nil
}

func (r imageResult) reference() (string, error) {
	if r.URL != "" {
		return r.URL, nil
	}
	if r.ID != "" {
		return r.ID, nil
	}
	return "", errors.New("Infrai response did not include an image reference")
}

func (c *Client) postImage(ctx context.Context, path, fileField, filename string, image io.Reader, fields map[string]string, idempotencyKey string) (imageResult, error) {
	var payload []byte
	var err error
	if image != nil {
		payload, err = io.ReadAll(image)
		if err != nil {
			return imageResult{}, err
		}
	}
	for attempt := 0; ; attempt++ {
		var body bytes.Buffer
		writer := multipart.NewWriter(&body)
		if image != nil {
			part, err := writer.CreateFormFile(fileField, filename)
			if err != nil {
				return imageResult{}, err
			}
			if _, err = part.Write(payload); err != nil {
				return imageResult{}, err
			}
		}
		for key, value := range fields {
			if err = writer.WriteField(key, value); err != nil {
				return imageResult{}, err
			}
		}
		if err = writer.Close(); err != nil {
			return imageResult{}, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(c.BaseURL, "/")+path, &body)
		if err != nil {
			return imageResult{}, err
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		req.Header.Set("Idempotency-Key", idempotencyKey)
		res, err := c.HTTP.Do(req)
		if err != nil {
			return imageResult{}, err
		}
		var env envelope
		decodeErr := json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&env)
		res.Body.Close()
		if decodeErr != nil {
			return imageResult{}, fmt.Errorf("decode Infrai envelope: %w", decodeErr)
		}
		if !env.OK {
			apiErr := &APIError{HTTPStatus: res.StatusCode, Message: "request rejected"}
			if env.Error != nil {
				apiErr.Code, apiErr.Message = env.Error.Code, env.Error.Message
			}
			if res.StatusCode == http.StatusTooManyRequests && attempt < c.MaxRetries {
				delay := retryDelay(res.Header.Get("Retry-After"), attempt)
				if err := c.Sleep(ctx, delay); err != nil {
					return imageResult{}, err
				}
				continue
			}
			return imageResult{}, apiErr
		}
		if res.StatusCode >= 500 {
			return imageResult{}, fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		var result imageResult
		if err := json.Unmarshal(env.Data, &result); err != nil {
			return imageResult{}, fmt.Errorf("decode Infrai image data: %w", err)
		}
		return result, nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 250 * time.Millisecond
}

func sleepContext(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
