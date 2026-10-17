package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type InfraiClient struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	Sleep      func(context.Context, time.Duration) error
}

type InfraiError struct {
	Code       string `json:"code"`
	Message    string `json:"message"`
	HTTPStatus int    `json:"-"`
}

func (e *InfraiError) Error() string {
	if e.Message == "" {
		return e.Code
	}
	return e.Code + ": " + e.Message
}

type envelope struct {
	OK       bool            `json:"ok"`
	Data     json.RawMessage `json:"data"`
	Error    *InfraiError    `json:"error"`
	Metadata json.RawMessage `json:"metadata"`
}

func (c *InfraiClient) call(ctx context.Context, method, path string, body any, out any) error {
	var payload []byte
	var err error
	if body != nil {
		payload, err = json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode request: %w", err)
		}
	}

	for attempt := 0; attempt < 4; attempt++ {
		req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.BaseURL, "/")+path, bytes.NewReader(payload))
		if err != nil {
			return fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Authorization", "Bearer "+c.APIKey)
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		res, err := c.HTTPClient.Do(req)
		if err != nil {
			return fmt.Errorf("call Infrai: %w", err)
		}
		responseBody, readErr := io.ReadAll(io.LimitReader(res.Body, 1<<20))
		res.Body.Close()
		if readErr != nil {
			return fmt.Errorf("read Infrai response: %w", readErr)
		}

		var env envelope
		if err := json.Unmarshal(responseBody, &env); err != nil {
			return fmt.Errorf("decode Infrai envelope (HTTP %d): %w", res.StatusCode, err)
		}
		if res.StatusCode == http.StatusTooManyRequests && attempt < 3 {
			if err := c.sleep(ctx, retryDelay(res.Header.Get("Retry-After"), attempt)); err != nil {
				return err
			}
			continue
		}
		if !env.OK {
			if env.Error == nil {
				return errors.New("Infrai request was rejected")
			}
			env.Error.HTTPStatus = res.StatusCode
			return env.Error
		}
		if res.StatusCode >= 500 {
			return fmt.Errorf("Infrai transport status %d", res.StatusCode)
		}
		if out != nil && len(env.Data) > 0 && string(env.Data) != "null" {
			if err := json.Unmarshal(env.Data, out); err != nil {
				return fmt.Errorf("decode Infrai data: %w", err)
			}
		}
		return nil
	}
	return errors.New("Infrai retry budget exhausted")
}

func (c *InfraiClient) sleep(ctx context.Context, delay time.Duration) error {
	if c.Sleep != nil {
		return c.Sleep(ctx, delay)
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryDelay(header string, attempt int) time.Duration {
	if seconds, err := strconv.Atoi(header); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	return time.Duration(1<<attempt) * 200 * time.Millisecond
}

func segment(value string) string { return url.PathEscape(value) }

func (c *InfraiClient) EnsureBucket(ctx context.Context, bucket string) error {
	err := c.call(ctx, http.MethodGet, "/v1/storage/bucket/get/"+segment(bucket), nil, nil)
	if err == nil {
		return nil
	}
	var rejected *InfraiError
	if !errors.As(err, &rejected) || rejected.HTTPStatus != http.StatusNotFound {
		return err
	}
	return c.call(ctx, http.MethodPost, "/v1/storage/bucket/create", map[string]any{
		"name": bucket,
	}, nil)
}

func (c *InfraiClient) VerifySession(ctx context.Context, sessionID string) error {
	return c.call(ctx, http.MethodGet, "/v1/auth/session/verify/"+segment(sessionID), nil, nil)
}

func (c *InfraiClient) PresignDownload(ctx context.Context, bucket, key string, expiresSeconds int, disposition, idempotencyKey string) (string, error) {
	var data struct {
		URL string `json:"url"`
	}
	// Canonical operation: infrai.storage.object.presign.
	err := c.call(ctx, http.MethodPost, "/v1/storage/object/presign/"+segment(bucket)+"/"+segment(key), map[string]any{
		"op":                   "get",
		"expires_seconds":      expiresSeconds,
		"response_disposition": disposition,
		"idempotency_key":      idempotencyKey,
	}, &data)
	if err != nil {
		return "", err
	}
	if data.URL == "" {
		return "", errors.New("Infrai response omitted signed URL")
	}
	return data.URL, nil
}
