package sampling

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/LerianStudio/gomcp-sdk/sampling/providers"
)

// mockProvider implements a mock LLM provider for testing
type mockProvider struct {
	name          string
	models        []string
	shouldError   bool
	errorMessage  string
	responseDelay time.Duration
	lastRequest   *providers.Request
}

func (m *mockProvider) CreateMessage(ctx context.Context, req *providers.Request) (*providers.Response, error) {
	m.lastRequest = req

	if m.responseDelay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.responseDelay):
		}
	}

	if m.shouldError {
		return nil, errors.New(m.errorMessage)
	}

	return &providers.Response{
		Content:    "Mock response for: " + req.Messages[len(req.Messages)-1].Content,
		Model:      req.Model,
		StopReason: "stop",
		Usage: &providers.Usage{
			PromptTokens:     10,
			CompletionTokens: 20,
			TotalTokens:      30,
		},
	}, nil
}

func (m *mockProvider) GetName() string {
	return m.name
}

func (m *mockProvider) GetSupportedModels() []string {
	return m.models
}

func (m *mockProvider) IsModelSupported(model string) bool {
	for _, supported := range m.models {
		if supported == model {
			return true
		}
	}
	return false
}

func TestHandler_CreateMessage(t *testing.T) {
	tests := []struct {
		name          string
		params        interface{}
		provider      *mockProvider
		config        *Config
		wantError     bool
		errorContains string
	}{
		{
			name: "valid request",
			params: CreateMessageRequest{
				Messages: []SamplingMessage{
					{Role: "user", Content: SamplingMessageContent{Type: "text", Text: "Hello"}},
				},
				MaxTokens: 100,
			},
			provider: &mockProvider{
				name:   "test",
				models: []string{"test-model"},
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
			},
			wantError: false,
		},
		{
			name: "empty messages",
			params: CreateMessageRequest{
				Messages:  []SamplingMessage{},
				MaxTokens: 100,
			},
			provider: &mockProvider{
				name:   "test",
				models: []string{"test-model"},
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
			},
			wantError:     true,
			errorContains: "messages array cannot be empty",
		},
		{
			name: "invalid max tokens",
			params: CreateMessageRequest{
				Messages: []SamplingMessage{
					{Role: "user", Content: SamplingMessageContent{Type: "text", Text: "Hello"}},
				},
				MaxTokens: 0,
			},
			provider: &mockProvider{
				name:   "test",
				models: []string{"test-model"},
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
			},
			wantError:     true,
			errorContains: "maxTokens must be positive",
		},
		{
			name: "provider error",
			params: CreateMessageRequest{
				Messages: []SamplingMessage{
					{Role: "user", Content: SamplingMessageContent{Type: "text", Text: "Hello"}},
				},
				MaxTokens: 100,
			},
			provider: &mockProvider{
				name:         "test",
				models:       []string{"test-model"},
				shouldError:  true,
				errorMessage: "provider error",
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
			},
			wantError:     true,
			errorContains: "failed to create message",
		},
		{
			name: "with system prompt",
			params: CreateMessageRequest{
				Messages: []SamplingMessage{
					{Role: "user", Content: SamplingMessageContent{Type: "text", Text: "Hello"}},
				},
				MaxTokens:    100,
				SystemPrompt: stringPtr("You are a helpful assistant"),
			},
			provider: &mockProvider{
				name:   "test",
				models: []string{"test-model"},
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
			},
			wantError: false,
		},
		{
			name: "with model preferences",
			params: CreateMessageRequest{
				Messages: []SamplingMessage{
					{Role: "user", Content: SamplingMessageContent{Type: "text", Text: "Hello"}},
				},
				MaxTokens: 100,
				ModelPreferences: &ModelPreferences{
					Hints: []ModelHint{{Name: "preferred-model"}},
				},
			},
			provider: &mockProvider{
				name:   "test",
				models: []string{"test-model", "preferred-model"},
			},
			config: &Config{
				Provider:     "test",
				APIKey:       "test-key",
				DefaultModel: "test-model",
				ModelMapping: map[string]string{},
			},
			wantError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				config:   tt.config,
				provider: tt.provider,
			}

			// Marshal params to JSON
			params, err := json.Marshal(tt.params)
			if err != nil {
				t.Fatalf("failed to marshal params: %v", err)
			}

			// Call CreateMessage
			resp, err := h.CreateMessage(context.Background(), params)

			// Check error
			if tt.wantError {
				if err == nil {
					t.Errorf("expected error but got none")
				} else if tt.errorContains != "" && !contains(err.Error(), tt.errorContains) {
					t.Errorf("error = %v, want error containing %v", err, tt.errorContains)
				}
				return
			}

			if err != nil {
				t.Errorf("unexpected error: %v", err)
				return
			}

			// Check response
			respMsg, ok := resp.(CreateMessageResponse)
			if !ok {
				t.Errorf("response type = %T, want CreateMessageResponse", resp)
				return
			}

			if respMsg.Role != "assistant" {
				t.Errorf("response role = %v, want assistant", respMsg.Role)
			}

			if respMsg.Content.Type != "text" {
				t.Errorf("response content type = %v, want text", respMsg.Content.Type)
			}

			if respMsg.Content.Text == "" {
				t.Errorf("response content text is empty")
			}
		})
	}
}

func TestHandler_GetCapabilities(t *testing.T) {
	tests := []struct {
		name     string
		provider providers.Provider
		want     map[string]interface{}
	}{
		{
			name: "with provider",
			provider: &mockProvider{
				name:   "test",
				models: []string{"model1", "model2"},
			},
			want: map[string]interface{}{
				"sampling": map[string]interface{}{
					"enabled": true,
					"models":  []string{"model1", "model2"},
				},
			},
		},
		{
			name:     "without provider",
			provider: nil,
			want: map[string]interface{}{
				"sampling": map[string]interface{}{
					"enabled": false,
					"models": []string{
						"claude-3-opus",
						"claude-3-sonnet",
						"claude-3-haiku",
						"gpt-4",
						"gpt-3.5-turbo",
					},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				provider: tt.provider,
			}

			got := h.GetCapabilities()

			// Compare enabled status
			gotSampling := got["sampling"].(map[string]interface{})
			wantSampling := tt.want["sampling"].(map[string]interface{})

			if gotSampling["enabled"] != wantSampling["enabled"] {
				t.Errorf("enabled = %v, want %v", gotSampling["enabled"], wantSampling["enabled"])
			}

			// Compare models
			gotModels := gotSampling["models"].([]string)
			wantModels := wantSampling["models"].([]string)

			if len(gotModels) != len(wantModels) {
				t.Errorf("models length = %v, want %v", len(gotModels), len(wantModels))
			}
		})
	}
}

func TestHandler_DetermineModel(t *testing.T) {
	tests := []struct {
		name   string
		req    *CreateMessageRequest
		config *Config
		want   string
	}{
		{
			name: "use default model",
			req:  &CreateMessageRequest{},
			config: &Config{
				DefaultModel: "default-model",
			},
			want: "default-model",
		},
		{
			name: "use model hint",
			req: &CreateMessageRequest{
				ModelPreferences: &ModelPreferences{
					Hints: []ModelHint{{Name: "hint-model"}},
				},
			},
			config: &Config{
				DefaultModel: "default-model",
				ModelMapping: map[string]string{},
			},
			want: "hint-model",
		},
		{
			name: "use mapped model",
			req: &CreateMessageRequest{
				ModelPreferences: &ModelPreferences{
					Hints: []ModelHint{{Name: "gpt-4"}},
				},
			},
			config: &Config{
				DefaultModel: "default-model",
				ModelMapping: map[string]string{
					"gpt-4": "claude-3-opus",
				},
			},
			want: "claude-3-opus",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := &Handler{
				config: tt.config,
				provider: &mockProvider{
					models: []string{"hint-model", "claude-3-opus", "default-model"},
				},
			}

			got := h.determineModel(tt.req)
			if got != tt.want {
				t.Errorf("determineModel() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestHandlerConfig_MapModel(t *testing.T) {
	tests := []struct {
		name     string
		config   *Config
		model    string
		expected string
	}{
		{
			name: "explicit mapping",
			config: &Config{
				Provider: "openai",
				ModelMapping: map[string]string{
					"custom-model": "mapped-model",
				},
			},
			model:    "custom-model",
			expected: "mapped-model",
		},
		{
			name: "openai gpt-4 mapping",
			config: &Config{
				Provider: "openai",
			},
			model:    "GPT-4-turbo",
			expected: "gpt-4",
		},
		{
			name: "anthropic claude mapping",
			config: &Config{
				Provider: "anthropic",
			},
			model:    "Claude-3-Opus",
			expected: "claude-3-opus-20240229",
		},
		{
			name: "no mapping",
			config: &Config{
				Provider: "unknown",
			},
			model:    "some-model",
			expected: "some-model",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.config.ModelMapping == nil {
				tt.config.ModelMapping = make(map[string]string)
			}
			got := tt.config.MapModel(tt.model)
			if got != tt.expected {
				t.Errorf("MapModel(%q) = %q, want %q", tt.model, got, tt.expected)
			}
		})
	}
}

// Helper functions
func stringPtr(s string) *string {
	return &s
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && s[:len(substr)] == substr ||
		len(s) >= len(substr) && contains(s[1:], substr)
}
