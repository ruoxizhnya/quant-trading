package httpclient

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/ruoxizhnya/quant-trading/pkg/logging"
	"github.com/rs/zerolog"
)

// Client is a generic HTTP client with timeout, retry, and structured logging.
type Client struct {
	baseURL    string
	httpClient *http.Client
	logger     zerolog.Logger
	maxRetries int
}

// New creates a new HTTP client.
func New(baseURL string, timeout time.Duration, maxRetries int) *Client {
	return &Client{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost:  10,
				IdleConnTimeout:      90 * time.Second,
			},
		},
		logger:     logging.Logger,
		maxRetries: maxRetries,
	}
}

// Request represents an HTTP request configuration.
type Request struct {
	Method  string
	Path    string
	Body    interface{}
	Headers map[string]string
}

// Response represents a decoded HTTP response.
type Response struct {
	StatusCode int
	Body       []byte
}

// Do sends an HTTP request with retry logic and exponential backoff.
func (c *Client) Do(ctx context.Context, req Request) (*Response, error) {
	var bodyReader io.Reader
	if req.Body != nil {
		data, err := json.Marshal(req.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	url := c.baseURL + req.Path
	if c.baseURL == "" {
		url = req.Path
	}

	var lastErr error
	for attempt := 0; attempt <= c.maxRetries; attempt++ {
		if attempt > 0 {
			backoff := time.Duration(1<<uint(attempt-1)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(backoff):
			}
		}

		r, err := http.NewRequestWithContext(ctx, req.Method, url, bodyReader)
		if err != nil {
			return nil, fmt.Errorf("failed to create request: %w", err)
		}

		r.Header.Set("Content-Type", "application/json")
		for k, v := range req.Headers {
			r.Header.Set(k, v)
		}

		c.logger.Debug().
			Str("method", req.Method).
			Str("url", url).
			Int("attempt", attempt+1).
			Msg("executing HTTP request")

		resp, err := c.httpClient.Do(r)
		if err != nil {
			lastErr = fmt.Errorf("request failed: %w", err)
			c.logger.Warn().
				Err(lastErr).
				Int("attempt", attempt+1).
				Msg("HTTP request failed, will retry")
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("failed to read response body: %w", err)
			c.logger.Warn().
				Err(lastErr).
				Int("attempt", attempt+1).
				Msg("Failed to read response body, will retry")
			continue
		}

		c.logger.Debug().
			Int("status", resp.StatusCode).
			Int("body_size", len(body)).Str("body", string(body[:min(len(body),200)])).
			Msg("HTTP response received")

		// Retry on 5xx errors or rate limiting
		if resp.StatusCode >= 500 || resp.StatusCode == 429 {
			lastErr = fmt.Errorf("server error: %d", resp.StatusCode)
			c.logger.Warn().
				Err(lastErr).
				Int("status", resp.StatusCode).
				Int("attempt", attempt+1).
				Msg("HTTP server error, will retry")
			continue
		}

		return &Response{
			StatusCode: resp.StatusCode,
			Body:       body,
		}, nil
	}

	return nil, fmt.Errorf("max retries (%d) exceeded: %w", c.maxRetries, lastErr)
}

// Get performs a GET request.
func (c *Client) Get(ctx context.Context, path string) (*Response, error) {
	return c.Do(ctx, Request{Method: http.MethodGet, Path: path})
}

// Post performs a POST request with a body.
func (c *Client) Post(ctx context.Context, path string, body interface{}) (*Response, error) {
	return c.Do(ctx, Request{Method: http.MethodPost, Path: path, Body: body})
}

// DecodeJSON decodes JSON response body into the target type.
func DecodeJSON(data []byte, target interface{}) error {
	return json.Unmarshal(data, target)
}
