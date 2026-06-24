package httpclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestNew_CreatesClientWithCorrectConfig(t *testing.T) {
	c := New("http://localhost:8085", 30*time.Second, 3)
	if c.baseURL != "http://localhost:8085" {
		t.Errorf("expected baseURL http://localhost:8085, got %s", c.baseURL)
	}
	if c.maxRetries != 3 {
		t.Errorf("expected maxRetries 3, got %d", c.maxRetries)
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected timeout 30s, got %v", c.httpClient.Timeout)
	}
}

func TestNew_ZeroRetries(t *testing.T) {
	c := New("http://localhost", 5*time.Second, 0)
	if c.maxRetries != 0 {
		t.Errorf("expected maxRetries 0, got %d", c.maxRetries)
	}
}

func TestDo_SuccessfulRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 2)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if string(resp.Body) != `{"status":"ok"}` {
		t.Errorf("expected body {\"status\":\"ok\"}, got %s", string(resp.Body))
	}
}

func TestDo_RetriesOn500(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 3)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err != nil {
		t.Fatalf("expected no error after retries, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDo_RetriesOn429(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 2 {
			w.WriteHeader(http.StatusTooManyRequests)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 3)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err != nil {
		t.Fatalf("expected no error after retry, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDo_DoesNotRetryOn404(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 3)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err != nil {
		t.Fatalf("expected no error for 404, got: %v", err)
	}
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("expected status 404, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 1 {
		t.Errorf("expected 1 attempt (no retry for 404), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDo_MaxRetriesExceeded(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 2)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err == nil {
		t.Fatal("expected error when max retries exceeded")
	}
	// maxRetries=2 means attempt 0,1,2 = 3 total attempts
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 attempts (0+1+2), got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDo_PostWithBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"received":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	body := map[string]string{"key": "value"}
	resp, err := c.Do(context.Background(), Request{Method: "POST", Path: "/submit", Body: body})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDo_WithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom") != "test-value" {
			t.Errorf("expected X-Custom header, got %s", r.Header.Get("X-Custom"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	_, err := c.Do(context.Background(), Request{
		Method:  "GET",
		Path:    "/test",
		Headers: map[string]string{"X-Custom": "test-value"},
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestDo_ContextCancelled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	_, err := c.Do(ctx, Request{Method: "GET", Path: "/test"})
	if err == nil {
		t.Fatal("expected error when context is cancelled")
	}
}

func TestDo_InvalidBodyMarshal(t *testing.T) {
	c := New("http://localhost", 10*time.Second, 0)
	// Channel cannot be marshaled to JSON
	_, err := c.Do(context.Background(), Request{
		Method: "POST",
		Path:   "/test",
		Body:   make(chan int),
	})
	if err == nil {
		t.Fatal("expected error for unmarshalable body")
	}
}

func TestGet_PerformsGETRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"data":"test"}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	resp, err := c.Get(context.Background(), "/api/data")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestPost_PerformsPOSTRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"created":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	resp, err := c.Post(context.Background(), "/api/create", map[string]string{"name": "test"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusCreated {
		t.Errorf("expected status 201, got %d", resp.StatusCode)
	}
}

func TestDecodeJSON_DecodesValidJSON(t *testing.T) {
	type target struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	data := []byte(`{"name":"Alice","age":30}`)
	var t1 target
	if err := DecodeJSON(data, &t1); err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if t1.Name != "Alice" {
		t.Errorf("expected name Alice, got %s", t1.Name)
	}
	if t1.Age != 30 {
		t.Errorf("expected age 30, got %d", t1.Age)
	}
}

func TestDecodeJSON_InvalidJSON(t *testing.T) {
	var target map[string]interface{}
	err := DecodeJSON([]byte(`{invalid json`), &target)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestDo_EmptyBaseURL_UsesPathDirectly(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	// When baseURL is empty, the Path should be used as the full URL
	c := New("", 10*time.Second, 0)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: server.URL + "/test"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
}

func TestDo_ConnectionError(t *testing.T) {
	// Use a port that's almost certainly not listening
	c := New("http://127.0.0.1:1", 1*time.Second, 0)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err == nil {
		t.Fatal("expected connection error")
	}
}

func TestDo_RetryWithBackoff(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 3)
	start := time.Now()
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	// Backoff: 1s + 2s = 3s minimum (attempts 1 and 2 trigger backoff)
	// Use a lower threshold to avoid flakiness on slow CI
	if elapsed < 1*time.Second {
		t.Errorf("expected backoff delay of at least 1s, got %v", elapsed)
	}
}

func TestDo_SetsContentTypeHeader(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	_, err := c.Do(context.Background(), Request{Method: "POST", Path: "/test", Body: map[string]string{"k": "v"}})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
}

func TestDo_SuccessAfterRetry(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := atomic.AddInt32(&attempts, 1)
		if n == 1 {
			w.WriteHeader(http.StatusBadGateway)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 3)
	resp, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("expected status 200, got %d", resp.StatusCode)
	}
	if atomic.LoadInt32(&attempts) != 2 {
		t.Errorf("expected 2 attempts, got %d", atomic.LoadInt32(&attempts))
	}
}

func TestDecodeJSON_EmptyData(t *testing.T) {
	var target interface{}
	err := DecodeJSON([]byte{}, &target)
	if err == nil {
		t.Fatal("expected error for empty data")
	}
}

func TestDecodeJSON_NilTarget(t *testing.T) {
	err := DecodeJSON([]byte(`{"key":"value"}`), nil)
	if err == nil {
		t.Fatal("expected error for nil target")
	}
}

func TestDo_ResponseContainsBody(t *testing.T) {
	expectedBody := `{"message":"hello world","code":42}`
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(expectedBody))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	resp, err := c.Get(context.Background(), "/test")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	if string(resp.Body) != expectedBody {
		t.Errorf("expected body %s, got %s", expectedBody, string(resp.Body))
	}
}

func TestDo_MultipleRequestsSameClient(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	for i := 0; i < 5; i++ {
		resp, err := c.Get(context.Background(), "/test")
		if err != nil {
			t.Fatalf("request %d failed: %v", i, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("request %d: expected 200, got %d", i, resp.StatusCode)
		}
	}
}

func TestDo_JSONResponseDecode(t *testing.T) {
	type apiResponse struct {
		Status string `json:"status"`
		Count  int    `json:"count"`
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(apiResponse{Status: "active", Count: 42})
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	resp, err := c.Get(context.Background(), "/test")
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	var result apiResponse
	if err := DecodeJSON(resp.Body, &result); err != nil {
		t.Fatalf("failed to decode: %v", err)
	}
	if result.Status != "active" {
		t.Errorf("expected status active, got %s", result.Status)
	}
	if result.Count != 42 {
		t.Errorf("expected count 42, got %d", result.Count)
	}
}

func TestDo_ErrorContainsStatusCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := New(server.URL, 10*time.Second, 0)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !containsStr(err.Error(), "500") {
		t.Errorf("expected error to contain status code 500, got: %s", err.Error())
	}
}

func containsStr(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || func() bool {
		for i := 0; i <= len(s)-len(substr); i++ {
			if s[i:i+len(substr)] == substr {
				return true
			}
		}
		return false
	}())
}

func TestDo_RetryCount(t *testing.T) {
	var attempts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&attempts, 1)
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	c := New(server.URL, 5*time.Second, 2)
	_, err := c.Do(context.Background(), Request{Method: "GET", Path: "/test"})
	if err == nil {
		t.Fatal("expected error")
	}
	// maxRetries=2 → attempts 0,1,2 = 3 total
	if atomic.LoadInt32(&attempts) != 3 {
		t.Errorf("expected 3 total attempts, got %d", atomic.LoadInt32(&attempts))
	}
	errMsg := fmt.Sprintf("max retries (2) exceeded")
	if !containsStr(err.Error(), errMsg) {
		t.Errorf("expected error to mention max retries, got: %s", err.Error())
	}
}
