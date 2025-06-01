package providers

import (
	"context"
	"time"
)

// Provider defines the interface for LLM providers
type Provider interface {
	// CreateMessage sends a message to the LLM and returns the response
	CreateMessage(ctx context.Context, req *Request) (*Response, error)

	// GetName returns the provider name
	GetName() string

	// GetSupportedModels returns the list of supported models
	GetSupportedModels() []string

	// IsModelSupported checks if a model is supported
	IsModelSupported(model string) bool
}

// Request represents a request to an LLM provider
type Request struct {
	Messages      []Message
	Model         string
	Temperature   *float64
	MaxTokens     int
	StopSequences []string
	SystemPrompt  *string
}

// Message represents a message in the conversation
type Message struct {
	Role    string
	Content string
}

// Response represents a response from an LLM provider
type Response struct {
	Content    string
	Model      string
	StopReason string
	Usage      *Usage
}

// Usage represents token usage information
type Usage struct {
	PromptTokens     int
	CompletionTokens int
	TotalTokens      int
}

// RetryConfig defines retry behavior for providers
type RetryConfig struct {
	MaxRetries     int
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	BackoffFactor  float64
}

// DefaultRetryConfig returns the default retry configuration
func DefaultRetryConfig() *RetryConfig {
	return &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 1 * time.Second,
		MaxBackoff:     30 * time.Second,
		BackoffFactor:  2.0,
	}
}
