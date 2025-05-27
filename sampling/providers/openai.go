package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// OpenAIProvider implements the Provider interface for OpenAI
type OpenAIProvider struct {
	apiKey      string
	baseURL     string
	httpClient  *http.Client
	retryConfig *RetryConfig
}

// NewOpenAIProvider creates a new OpenAI provider
func NewOpenAIProvider(apiKey string) *OpenAIProvider {
	return &OpenAIProvider{
		apiKey:  apiKey,
		baseURL: "https://api.openai.com/v1",
		httpClient: &http.Client{
			Timeout: 60 * time.Second,
		},
		retryConfig: DefaultRetryConfig(),
	}
}

// SetRetryConfig sets custom retry configuration
func (p *OpenAIProvider) SetRetryConfig(config *RetryConfig) {
	p.retryConfig = config
}

// GetName returns the provider name
func (p *OpenAIProvider) GetName() string {
	return "openai"
}

// GetSupportedModels returns the list of supported models
func (p *OpenAIProvider) GetSupportedModels() []string {
	return []string{
		"gpt-4-turbo-preview",
		"gpt-4",
		"gpt-3.5-turbo",
		"gpt-3.5-turbo-16k",
	}
}

// IsModelSupported checks if a model is supported
func (p *OpenAIProvider) IsModelSupported(model string) bool {
	for _, m := range p.GetSupportedModels() {
		if m == model {
			return true
		}
	}
	return false
}

// CreateMessage sends a message to OpenAI and returns the response
func (p *OpenAIProvider) CreateMessage(ctx context.Context, req *Request) (*Response, error) {
	if req.Model == "" {
		req.Model = "gpt-3.5-turbo"
	}

	// Build OpenAI request
	openaiReq := openAIRequest{
		Model:       req.Model,
		Messages:    p.convertMessages(req),
		MaxTokens:   req.MaxTokens,
		Temperature: req.Temperature,
		Stop:        req.StopSequences,
	}

	// Marshal request
	reqBody, err := json.Marshal(openaiReq)
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

func (p *OpenAIProvider) convertMessages(req *Request) []openAIMessage {
	messages := make([]openAIMessage, 0, len(req.Messages)+1)
	
	// Add system prompt if provided
	if req.SystemPrompt != nil && *req.SystemPrompt != "" {
		messages = append(messages, openAIMessage{
			Role:    "system",
			Content: *req.SystemPrompt,
		})
	}
	
	// Convert messages
	for _, msg := range req.Messages {
		messages = append(messages, openAIMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}
	
	return messages
}

func (p *OpenAIProvider) doRequest(ctx context.Context, reqBody []byte) (*Response, error) {
	// Create HTTP request
	httpReq, err := http.NewRequestWithContext(ctx, "POST", p.baseURL+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+p.apiKey)

	// Execute request
	httpResp, err := p.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("failed to execute request: %w", err)
	}
	defer httpResp.Body.Close()

	// Read response body
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if httpResp.StatusCode != http.StatusOK {
		var errorResp openAIErrorResponse
		if err := json.Unmarshal(respBody, &errorResp); err == nil && errorResp.Error.Message != "" {
			return nil, fmt.Errorf("OpenAI API error (status %d): %s", httpResp.StatusCode, errorResp.Error.Message)
		}
		return nil, fmt.Errorf("OpenAI API error: status %d: %s", httpResp.StatusCode, string(respBody))
	}

	// Parse response
	var openaiResp openAIResponse
	if err := json.Unmarshal(respBody, &openaiResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if len(openaiResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	choice := openaiResp.Choices[0]
	
	// Convert stop reason
	stopReason := "stop"
	if choice.FinishReason != "" {
		stopReason = choice.FinishReason
	}

	return &Response{
		Content:    choice.Message.Content,
		Model:      openaiResp.Model,
		StopReason: stopReason,
		Usage: &Usage{
			PromptTokens:     openaiResp.Usage.PromptTokens,
			CompletionTokens: openaiResp.Usage.CompletionTokens,
			TotalTokens:      openaiResp.Usage.TotalTokens,
		},
	}, nil
}

func (p *OpenAIProvider) executeWithRetry(ctx context.Context, fn func() error) error {
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

func (p *OpenAIProvider) isRetryableError(err error) bool {
	errStr := err.Error()
	// Retry on rate limits, timeouts, and temporary errors
	return strings.Contains(errStr, "429") ||
		strings.Contains(errStr, "Rate limit") ||
		strings.Contains(errStr, "rate_limit") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "temporary") ||
		strings.Contains(errStr, "503")
}

// OpenAI API types
type openAIRequest struct {
	Model       string          `json:"model"`
	Messages    []openAIMessage `json:"messages"`
	MaxTokens   int             `json:"max_tokens"`
	Temperature *float64        `json:"temperature,omitempty"`
	Stop        []string        `json:"stop,omitempty"`
}

type openAIMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type openAIResponse struct {
	ID      string          `json:"id"`
	Model   string          `json:"model"`
	Choices []openAIChoice  `json:"choices"`
	Usage   openAIUsage     `json:"usage"`
}

type openAIChoice struct {
	Message      openAIMessage `json:"message"`
	FinishReason string        `json:"finish_reason"`
}

type openAIUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type openAIErrorResponse struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    string `json:"code"`
	} `json:"error"`
}