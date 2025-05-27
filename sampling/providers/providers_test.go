package providers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestRetryConfig(t *testing.T) {
	config := DefaultRetryConfig()
	
	if config.MaxRetries != 3 {
		t.Errorf("MaxRetries = %d, want 3", config.MaxRetries)
	}
	
	if config.InitialBackoff != 1*time.Second {
		t.Errorf("InitialBackoff = %v, want 1s", config.InitialBackoff)
	}
	
	if config.MaxBackoff != 30*time.Second {
		t.Errorf("MaxBackoff = %v, want 30s", config.MaxBackoff)
	}
	
	if config.BackoffFactor != 2.0 {
		t.Errorf("BackoffFactor = %v, want 2.0", config.BackoffFactor)
	}
}

func TestOpenAIProvider_RetryLogic(t *testing.T) {
	retryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount < 3 {
			// Simulate rate limit error
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error": {"message": "Rate limit exceeded", "type": "rate_limit_error"}}`))
			return
		}
		// Success on third attempt
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "test",
			"model": "gpt-3.5-turbo",
			"choices": [{
				"message": {"role": "assistant", "content": "Success after retries"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 20, "total_tokens": 30}
		}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider("test-key")
	provider.baseURL = server.URL
	provider.retryConfig = &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	req := &Request{
		Messages:  []Message{{Role: "user", Content: "Test"}},
		Model:     "gpt-3.5-turbo",
		MaxTokens: 100,
	}

	start := time.Now()
	resp, err := provider.CreateMessage(context.Background(), req)
	elapsed := time.Since(start)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Success after retries" {
		t.Errorf("response content = %q, want %q", resp.Content, "Success after retries")
	}

	if retryCount != 3 {
		t.Errorf("retry count = %d, want 3", retryCount)
	}

	// Check that backoff was applied (should take at least 30ms for 2 retries)
	if elapsed < 30*time.Millisecond {
		t.Errorf("elapsed time = %v, expected at least 30ms for backoff", elapsed)
	}
}

func TestAnthropicProvider_RetryLogic(t *testing.T) {
	retryCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		retryCount++
		if retryCount < 3 {
			// Simulate server error
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"error": {"type": "overloaded_error", "message": "Service temporarily unavailable"}}`))
			return
		}
		// Success on third attempt
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "test",
			"type": "message",
			"role": "assistant",
			"content": [{"type": "text", "text": "Success after retries"}],
			"model": "claude-3-haiku-20240307",
			"stop_reason": "end_turn",
			"usage": {"input_tokens": 10, "output_tokens": 20}
		}`))
	}))
	defer server.Close()

	provider := NewAnthropicProvider("test-key")
	provider.baseURL = server.URL
	provider.retryConfig = &RetryConfig{
		MaxRetries:     3,
		InitialBackoff: 10 * time.Millisecond,
		MaxBackoff:     100 * time.Millisecond,
		BackoffFactor:  2.0,
	}

	req := &Request{
		Messages:  []Message{{Role: "user", Content: "Test"}},
		Model:     "claude-3-haiku-20240307",
		MaxTokens: 100,
	}

	resp, err := provider.CreateMessage(context.Background(), req)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if resp.Content != "Success after retries" {
		t.Errorf("response content = %q, want %q", resp.Content, "Success after retries")
	}

	if retryCount != 3 {
		t.Errorf("retry count = %d, want 3", retryCount)
	}
}

func TestProvider_NonRetryableErrors(t *testing.T) {
	tests := []struct {
		name         string
		statusCode   int
		errorMessage string
		shouldRetry  bool
	}{
		{
			name:         "authentication error",
			statusCode:   http.StatusUnauthorized,
			errorMessage: "Invalid API key",
			shouldRetry:  false,
		},
		{
			name:         "bad request",
			statusCode:   http.StatusBadRequest,
			errorMessage: "Invalid request",
			shouldRetry:  false,
		},
		{
			name:         "rate limit",
			statusCode:   http.StatusTooManyRequests,
			errorMessage: "Rate limit exceeded",
			shouldRetry:  true,
		},
		{
			name:         "server error",
			statusCode:   http.StatusInternalServerError,
			errorMessage: "Internal server error",
			shouldRetry:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attemptCount := 0
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				attemptCount++
				w.WriteHeader(tt.statusCode)
				_, _ = w.Write([]byte(`{"error": {"message": "` + tt.errorMessage + `"}}`))
			}))
			defer server.Close()

			provider := NewOpenAIProvider("test-key")
			provider.baseURL = server.URL
			provider.retryConfig = &RetryConfig{
				MaxRetries:     3,
				InitialBackoff: 1 * time.Millisecond,
				MaxBackoff:     10 * time.Millisecond,
				BackoffFactor:  2.0,
			}

			req := &Request{
				Messages:  []Message{{Role: "user", Content: "Test"}},
				Model:     "gpt-3.5-turbo",
				MaxTokens: 100,
			}

			_, err := provider.CreateMessage(context.Background(), req)

			if err == nil {
				t.Fatal("expected error but got none")
			}

			expectedAttempts := 1
			if tt.shouldRetry {
				expectedAttempts = provider.retryConfig.MaxRetries + 1
			}

			if attemptCount != expectedAttempts {
				t.Errorf("attempt count = %d, want %d", attemptCount, expectedAttempts)
			}
		})
	}
}

func TestProvider_ContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Simulate slow response
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"choices": [{"message": {"content": "test"}}]}`))
	}))
	defer server.Close()

	provider := NewOpenAIProvider("test-key")
	provider.baseURL = server.URL

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	req := &Request{
		Messages:  []Message{{Role: "user", Content: "Test"}},
		Model:     "gpt-3.5-turbo",
		MaxTokens: 100,
	}

	_, err := provider.CreateMessage(ctx, req)

	if err == nil {
		t.Fatal("expected context cancellation error")
	}

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("error = %v, want context.DeadlineExceeded", err)
	}
}

func TestProvider_ModelSupport(t *testing.T) {
	tests := []struct {
		name     string
		provider Provider
		model    string
		expected bool
	}{
		{
			name:     "OpenAI supported model",
			provider: NewOpenAIProvider("test-key"),
			model:    "gpt-4",
			expected: true,
		},
		{
			name:     "OpenAI unsupported model",
			provider: NewOpenAIProvider("test-key"),
			model:    "claude-3-opus",
			expected: false,
		},
		{
			name:     "Anthropic supported model",
			provider: NewAnthropicProvider("test-key"),
			model:    "claude-3-opus-20240229",
			expected: true,
		},
		{
			name:     "Anthropic unsupported model",
			provider: NewAnthropicProvider("test-key"),
			model:    "gpt-4",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.provider.IsModelSupported(tt.model)
			if got != tt.expected {
				t.Errorf("IsModelSupported(%q) = %v, want %v", tt.model, got, tt.expected)
			}
		})
	}
}