package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Client wraps an HTTP client for LLM chat completions.
type Client struct {
	apiKey     string
	apiURL     string
	model      string
	httpClient *http.Client
}

// NewClient creates a new AI client reading config from environment.
func NewClient() *Client {
	return &Client{
		apiKey:     os.Getenv("AI_API_KEY"),
		apiURL:     os.Getenv("AI_API_URL"),
		model:     "gpt-4o-mini",
		httpClient: &http.Client{},
	}
}

// IsConfigured returns true when both AI_API_KEY and AI_API_URL are set.
func (c *Client) IsConfigured() bool {
	return c.apiKey != "" && c.apiURL != ""
}

// ChatMessage represents a single message in a chat conversation.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest is the request body for OpenAI-compatible chat completions.
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
}

// ChatResponse is the top-level response from OpenAI-compatible chat completions.
type ChatResponse struct {
	Choices []Choice `json:"choices"`
}

// Choice represents one completion choice.
type Choice struct {
	Message ChatMessage `json:"message"`
}

// Chat sends a chat completion request and returns the assistant's reply text.
func (c *Client) Chat(ctx context.Context, messages []ChatMessage) (string, error) {
	if !c.IsConfigured() {
		return "", fmt.Errorf("AI_API_KEY or AI_API_URL not configured")
	}

	reqBody := ChatRequest{Model: c.model, Messages: messages}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", c.apiURL, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return "", fmt.Errorf("AI API returned status %d", resp.StatusCode)
	}

	var chatResp ChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&chatResp); err != nil {
		return "", fmt.Errorf("failed to decode response: %w", err)
	}
	if len(chatResp.Choices) == 0 {
		return "", fmt.Errorf("no response choices from LLM")
	}
	return chatResp.Choices[0].Message.Content, nil
}

// GenerateStrategyCode calls the LLM with system + user prompts and returns
// the raw Go code, stripping any markdown fences.
func (c *Client) GenerateStrategyCode(ctx context.Context, description string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: fmt.Sprintf(UserPromptTemplate, description)},
	}
	resp, err := c.Chat(ctx, messages)
	if err != nil {
		return "", err
	}
	return stripFences(resp), nil
}

// FixStrategyCode asks the LLM to fix compilation errors in the given code.
func (c *Client) FixStrategyCode(ctx context.Context, code string, buildErrors string) (string, error) {
	messages := []ChatMessage{
		{Role: "system", Content: SystemPrompt},
		{Role: "user", Content: fmt.Sprintf(FixPromptTemplate, code, buildErrors)},
	}
	resp, err := c.Chat(ctx, messages)
	if err != nil {
		return "", err
	}
	return stripFences(resp), nil
}

func stripFences(s string) string {
	s = strings.TrimPrefix(s, "```go")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}
