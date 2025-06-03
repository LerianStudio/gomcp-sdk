package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// AnthropicProvider implements the Provider interface for Anthropic
type AnthropicProvider struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	retryConfig *RetryConfig
}

// NewAnthropicProvider creates a new Anthropic provider
func NewAnthropicProvider(apiKey string) *AnthropicProvider {
	return &AnthropicProvider{
		apiKey:  apiKey,
		baseURL: "https://api.anthropic.com/v1",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		retryConfig: DefaultRetryConfig(),
	}
}

// SetRetryConfig sets custom retry configuration
func (p *AnthropicProvider) SetRetryConfig(config *RetryConfig) {
	p.retryConfig = config
}

// GetName returns the provider name
func (p *AnthropicProvider) GetName() string {
	return "anthropic"
}

// GetSupportedModels returns the list of supported models
func (p *AnthropicProvider) GetSupportedModels() []string {
	return []string{
		"claude-3-opus-20240229",
		"claude-3-sonnet-20240229",
		"claude-3-haiku-20240307",
		"claude-2.1",
		"claude-2.0",
		"claude-instant-1.2",
	}
}

// IsModelSupported checks if a model is supported
func (p *AnthropicProvider) IsModelSupported(model string) bool {
	for _, m := range p.GetSupportedModels() {
		if m == model {
			return true
		}
	}
	return false
}

// CreateMessage sends a message to Anthropic and returns the response
func (p *AnthropicProvider) CreateMessage(ctx context.Context, req *Request) (*Response, error) {
	if req.Model == "" {
		req.Model = "claude-3-haiku-20240307"
	}

	// Build Anthropic request
	anthropicReq := anthropicRequest{
		Model:         req.Model,
		Messages:      p.convertMessages(req),
		MaxTokens:     req.MaxTokens,
		Temperature:   req.Temperature,
		StopSequences: req.StopSequences,
	}

	// Add system prompt if provided
	if req.SystemPrompt != nil && *req.SystemPrompt != "" {
		anthropicReq.System = *req.SystemPrompt
	}

	// Marshal request
	reqBody, err := json.Marshal(anthropicReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	// Execute request with retries
	var resp *Response
	err = p.executeWithRetry(ctx, func() error {
		var retryErr error
		resp, retryErr = p.doRequest(ctx, reqBody)
		return retryErr
	})

	return resp, err
}

func (p *AnthropicProvider) convertMessages(req *Request) []anthropicMessage {
	messages := make([]anthropicMessage, 0, len(req.Messages))

	for _, msg := range req.Messages {
		// Anthropic doesn't support system role in messages array
		if msg.Role == "system" {
			continue
		}

		convertedMsg := anthropicMessage(msg)
		messages = append(messages, convertedMsg)
	}

	return messages
}

func (p *AnthropicProvider) doRequest(ctx context.Context, reqBody []byte) (*Response, error) {
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/messages", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("x-api-key", p.apiKey)
	httpReq.Header.Set("anthropic-version", "2023-06-01")

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer func() {
		if err := httpResp.Body.Close(); err != nil {
			log.Printf("Error closing response body: %v", err)
		}
	}()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if httpResp.StatusCode != http.StatusOK {
		var errorResp anthropicErrorResponse
		if err := json.Unmarshal(respBody, &errorResp); err == nil && errorResp.Error.Message != "" {
			return nil, fmt.Errorf("anthropic API error (status %d): %s", httpResp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("anthropic API error: status %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Parse response
	var anthropicResp anthropicResponse
	if err := json.Unmarshal(respBody, &anthropicResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// Extract content
	content := ""
	if len(anthropicResp.Content) > 0 {
		// Anthropic returns content as an array of content blocks
		for _, block := range anthropicResp.Content {
			if block.Type == "text" {
				content += block.Text
			}
		}
	}

	return &Response{
		Content:    content,
		Model:      anthropicResp.Model,
		StopReason: anthropicResp.StopReason,
		Usage: &Usage{
			PromptTokens:     anthropicResp.Usage.InputTokens,
			CompletionTokens: anthropicResp.Usage.OutputTokens,
			TotalTokens:      anthropicResp.Usage.InputTokens + anthropicResp.Usage.OutputTokens,
		},
	}, nil
}

func (p *AnthropicProvider) executeWithRetry(ctx context.Context, fn func() error) error {
	backoff := p.retryConfig.InitialBackoff

	for i := 0; i <= p.retryConfig.MaxRetries; i++ {
		err := fn()
		if err == nil {
			return nil
		}

		// Don't retry on last attempt
		if i == p.retryConfig.MaxRetries {
			return err
		}

		// Check if error is retryable
		if !p.isRetryableError(err) {
			return err
		}

		// Wait with backoff
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
			// Calculate next backoff
			backoff = time.Duration(float64(backoff) * p.retryConfig.BackoffFactor)
			if backoff > p.retryConfig.MaxBackoff {
				backoff = p.retryConfig.MaxBackoff
			}
		}
	}

	return fmt.Errorf("max retries exceeded")
}

func (p *AnthropicProvider) isRetryableError(err error) bool {
	errStr := err.Error()
	// Retry on rate limits, timeouts, and temporary errors
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "Rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary") ||
		strings.Contains(errStr, "temporarily unavailable") ||
		strings.Contains(errStr, "503") ||
		strings.Contains(errStr, "529")
}

// Anthropic API types
type anthropicRequest struct {
	Model         string             `json:"model"`
	Messages      []anthropicMessage `json:"messages"`
	MaxTokens     int                `json:"max_tokens"`
	Temperature   *float64           `json:"temperature,omitempty"`
	System        string             `json:"system,omitempty"`
	StopSequences []string           `json:"stop_sequences,omitempty"`
}

type anthropicMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type anthropicResponse struct {
	ID           string             `json:"id"`
	Type         string             `json:"type"`
	Role         string             `json:"role"`
	Content      []anthropicContent `json:"content"`
	Model        string             `json:"model"`
	StopReason   string             `json:"stop_reason"`
	StopSequence string             `json:"stop_sequence,omitempty"`
	Usage        anthropicUsage     `json:"usage"`
}

type anthropicContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type anthropicUsage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

type anthropicErrorResponse struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}
