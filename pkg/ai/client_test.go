package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---- NewClient + IsConfigured --------------------------------------------

func TestNewClient_DefaultsToMiniModel(t *testing.T) {
	c := NewClient()
	require.NotNil(t, c)
	assert.Equal(t, "gpt-4o-mini", c.model)
	assert.NotNil(t, c.httpClient)
}

func TestClient_IsConfigured_BothEmpty(t *testing.T) {
	t.Setenv("AI_API_KEY", "")
	t.Setenv("AI_API_URL", "")
	c := NewClient()
	assert.False(t, c.IsConfigured(), "both env vars empty → not configured")
}

func TestClient_IsConfigured_KeyOnly(t *testing.T) {
	t.Setenv("AI_API_KEY", "k")
	t.Setenv("AI_API_URL", "")
	c := NewClient()
	assert.False(t, c.IsConfigured(), "key only is not enough")
}

func TestClient_IsConfigured_URLOOnly(t *testing.T) {
	t.Setenv("AI_API_KEY", "")
	t.Setenv("AI_API_URL", "https://example.test")
	c := NewClient()
	assert.False(t, c.IsConfigured(), "url only is not enough")
}

func TestClient_IsConfigured_BothSet(t *testing.T) {
	t.Setenv("AI_API_KEY", "k")
	t.Setenv("AI_API_URL", "https://example.test")
	c := NewClient()
	assert.True(t, c.IsConfigured())
}

// ---- Chat (httptest server) ----------------------------------------------

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return &Client{
		apiKey:     "test-key",
		apiURL:     srv.URL,
		model:      "gpt-4o-mini",
		httpClient: &http.Client{Timeout: 2 * time.Second},
	}
}

func TestClient_Chat_Success(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		// Auth header must be present
		assert.Equal(t, "Bearer test-key", r.Header.Get("Authorization"))
		assert.Equal(t, "application/json", r.Header.Get("Content-Type"))

		// Body must be parseable
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		var req ChatRequest
		require.NoError(t, json.Unmarshal(body, &req))
		assert.Equal(t, "gpt-4o-mini", req.Model)
		require.Len(t, req.Messages, 1)
		assert.Equal(t, "user", req.Messages[0].Role)

		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Role: "assistant", Content: "hello"}}},
		})
	})

	got, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hi"}})
	require.NoError(t, err)
	assert.Equal(t, "hello", got)
}

func TestClient_Chat_NotConfigured(t *testing.T) {
	c := &Client{apiKey: "", apiURL: ""} // explicitly empty
	_, err := c.Chat(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AI_API_KEY or AI_API_URL not configured")
}

func TestClient_Chat_StatusError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AI API returned status 500")
}

func TestClient_Chat_BadJSON(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("not json"))
	})
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode response")
}

func TestClient_Chat_NoChoices(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{Choices: nil})
	})
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no response choices from LLM")
}

func TestClient_Chat_NetworkError(t *testing.T) {
	// Closed server → Dial fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	c := &Client{
		apiKey:     "k",
		apiURL:     srv.URL,
		model:      "gpt-4o-mini",
		httpClient: &http.Client{Timeout: 500 * time.Millisecond},
	}
	_, err := c.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "x"}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "request failed")
}

func TestClient_Chat_ContextCancel(t *testing.T) {
	// Server hangs forever; cancel the context to force a client-side abort.
	hang := make(chan struct{})
	t.Cleanup(func() { close(hang) })

	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		<-hang
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := c.Chat(ctx, []ChatMessage{{Role: "user", Content: "x"}})
	require.Error(t, err)
}

// ---- stripFences ---------------------------------------------------------

func TestStripFences(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"go_fence_with_newlines", "```go\npackage main\n```", "package main"},
		{"bare_fence", "```\nfoo\n```", "foo"},
		{"no_fence", "package main", "package main"},
		{"only_prefix_go", "```gopackage main", "package main"},
		{"only_prefix_bare", "```package main", "package main"},
		{"only_suffix", "package main```", "package main"},
		{"go_fence_inline", "```go hello ```", "hello"},
		// Leading whitespace blocks TrimPrefix("```go") — only TrimSpace applies.
		{"leading_whitespace_keeps_fences", "  \n```go\nfoo\n```\n  ", "```go\nfoo\n```"},
		{"empty_string", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, stripFences(tc.in))
		})
	}
}

// ---- GenerateStrategyCode ------------------------------------------------

func TestClient_GenerateStrategyCode_StripsFences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		require.NoError(t, json.Unmarshal(body, &req))
		// Must include system prompt + user prompt
		require.GreaterOrEqual(t, len(req.Messages), 2)
		assert.Equal(t, "system", req.Messages[0].Role)
		assert.Equal(t, SystemPrompt, req.Messages[0].Content)
		assert.Equal(t, "user", req.Messages[1].Role)
		assert.Contains(t, req.Messages[1].Content, "momentum strategy on top 50 stocks")

		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "```go\npackage plugins\n```"}}},
		})
	})

	got, err := c.GenerateStrategyCode(context.Background(), "momentum strategy on top 50 stocks")
	require.NoError(t, err)
	assert.Equal(t, "package plugins", got)
	assert.NotContains(t, got, "```", "fences must be stripped")
}

func TestClient_GenerateStrategyCode_PropagatesError(t *testing.T) {
	c := &Client{apiKey: "", apiURL: ""}
	_, err := c.GenerateStrategyCode(context.Background(), "x")
	require.Error(t, err)
}

// ---- FixStrategyCode -----------------------------------------------------

func TestClient_FixStrategyCode_StripsFences(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		var req ChatRequest
		require.NoError(t, json.Unmarshal(body, &req))
		require.GreaterOrEqual(t, len(req.Messages), 2)
		// User message must include the original code + the build errors verbatim
		assert.Contains(t, req.Messages[1].Content, "package plugins")
		assert.Contains(t, req.Messages[1].Content, "undefined: foo")

		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "```go\npackage plugins // fixed\n```"}}},
		})
	})

	got, err := c.FixStrategyCode(context.Background(), "package plugins", "undefined: foo")
	require.NoError(t, err)
	assert.Equal(t, "package plugins // fixed", got)
}

func TestClient_FixStrategyCode_PropagatesError(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusBadRequest)
	})
	_, err := c.FixStrategyCode(context.Background(), "code", "err")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "400")
}

// ---- Prompt constants ----------------------------------------------------

func TestPromptConstants_AreNonEmpty(t *testing.T) {
	// Compile-time invariant: if these constants are ever truncated to "" the
	// LLM receives a useless system prompt. Catch that at unit-test time.
	assert.NotEmpty(t, SystemPrompt, "SystemPrompt must not be empty")
	assert.NotEmpty(t, UserPromptTemplate, "UserPromptTemplate must not be empty")
	assert.NotEmpty(t, FixPromptTemplate, "FixPromptTemplate must not be empty")

	// The system prompt must encode the strategy interface signature
	// so the LLM generates compileable code.
	assert.Contains(t, SystemPrompt, "GenerateSignals")
	assert.Contains(t, SystemPrompt, "Configure")
	assert.Contains(t, SystemPrompt, "Cleanup")
	assert.Contains(t, SystemPrompt, "Weight")
	assert.Contains(t, SystemPrompt, `package plugins`)
	assert.Contains(t, SystemPrompt, "strategy.GlobalRegister")

	// User prompt must be a Go-template with exactly one %s
	assert.Equal(t, 1, strings.Count(UserPromptTemplate, "%s"))

	// Fix prompt must have two %s slots (code + errors)
	assert.Equal(t, 2, strings.Count(FixPromptTemplate, "%s"))
}

// ---- end-to-end: GenerateStrategyCode then stripFences boundary ----------

func TestClient_GenerateStrategyCode_NoFencesUnchanged(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "package plugins\n// bare code"}}},
		})
	})
	got, err := c.GenerateStrategyCode(context.Background(), "anything")
	require.NoError(t, err)
	assert.Equal(t, "package plugins\n// bare code", got)
}

// Sanity: when a context is canceled before the request, the error must
// come from transport (not from a panic in NewRequestWithContext).
func TestClient_Chat_AlreadyCanceledContext(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(ChatResponse{
			Choices: []Choice{{Message: ChatMessage{Content: "ok"}}},
		})
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// The test server may or may not return a reply depending on timing;
	// either a response or an error is acceptable, but it must not panic.
	_, _ = c.Chat(ctx, []ChatMessage{{Role: "user", Content: "x"}})
}

// ---- NewClientWithOptions + functional options ----------------------------

func TestNewClientWithOptions_Defaults(t *testing.T) {
	c, _ := NewClientWithOptions()
	require.NotNil(t, c)
	assert.Equal(t, "gpt-4o-mini", c.model)
	assert.NotNil(t, c.httpClient)
	assert.Equal(t, defaultHTTPTimeout, c.httpClient.Timeout)
	// apiKey / apiURL default to empty → not configured.
	assert.False(t, c.IsConfigured())
}

func TestWithAPIKey_OverridesEnv(t *testing.T) {
	t.Setenv("AI_API_KEY", "from-env")
	c, _ := NewClientWithOptions(
		WithAPIKey("from-option"),
		WithAPIURL("https://x.test"),
	)
	assert.True(t, c.IsConfigured())
	assert.Equal(t, "from-option", c.apiKey,
		"WithAPIKey must override the AI_API_KEY env var")
}

func TestWithAPIURL_OverridesEnv(t *testing.T) {
	t.Setenv("AI_API_URL", "https://env.example.test")
	c, _ := NewClientWithOptions(
		WithAPIKey("k"),
		WithAPIURL("https://opt.example.test"),
	)
	assert.True(t, c.IsConfigured())
	assert.Equal(t, "https://opt.example.test", c.apiURL)
}

func TestWithModel_OverridesDefault(t *testing.T) {
	c, _ := NewClientWithOptions(WithModel("gpt-4o"))
	assert.Equal(t, "gpt-4o", c.model)
}

func TestWithHTTPClient_ReplacesTransport(t *testing.T) {
	custom := &http.Client{Timeout: 7 * time.Second}
	c, _ := NewClientWithOptions(WithHTTPClient(custom))
	assert.Same(t, custom, c.httpClient,
		"WithHTTPClient must replace the transport by pointer equality")
}

func TestWithTimeout_SetsNewClient(t *testing.T) {
	c, _ := NewClientWithOptions(WithTimeout(123 * time.Millisecond))
	assert.Equal(t, 123*time.Millisecond, c.httpClient.Timeout)
}

func TestWithTimeout_ZeroDisables(t *testing.T) {
	c, _ := NewClientWithOptions(WithTimeout(0))
	// Zero means "no timeout" — http.Client.Timeout is 0 but the
	// pointer must be non-nil so the client is still usable.
	require.NotNil(t, c.httpClient)
	assert.Equal(t, time.Duration(0), c.httpClient.Timeout)
}

func TestWithTimeout_NegativeDisables(t *testing.T) {
	// Defensive: a negative value is meaningless in net/http.
	// The option should treat <=0 as "disable" rather than panic.
	c, _ := NewClientWithOptions(WithTimeout(-5 * time.Second))
	require.NotNil(t, c.httpClient)
	assert.Equal(t, time.Duration(0), c.httpClient.Timeout)
}

func TestNewClientWithOptions_AllCombined(t *testing.T) {
	c, _ := NewClientWithOptions(
		WithAPIKey("k"),
		WithAPIURL("https://x.test"),
		WithModel("gpt-4o"),
		WithTimeout(2*time.Second),
	)
	assert.True(t, c.IsConfigured())
	assert.Equal(t, "k", c.apiKey)
	assert.Equal(t, "https://x.test", c.apiURL)
	assert.Equal(t, "gpt-4o", c.model)
	assert.Equal(t, 2*time.Second, c.httpClient.Timeout)
}
