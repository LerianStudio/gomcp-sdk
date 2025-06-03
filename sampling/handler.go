package sampling

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/sampling/providers"
)

// Handler implements the sampling functionality for MCP
type Handler struct {
	config   *Config
	provider providers.Provider
	logger   *log.Logger
}

// NewHandler creates a new sampling handler
func NewHandler() *Handler {
	config := NewConfig()
	return NewHandlerWithConfig(config)
}

// NewHandlerWithConfig creates a new sampling handler with custom configuration
func NewHandlerWithConfig(config *Config) *Handler {
	h := &Handler{
		config: config,
	}

	if config.EnableLogging {
		h.logger = log.New(log.Writer(), "[sampling] ", log.LstdFlags|log.Lshortfile)
	}

	// Create provider
	provider, err := config.CreateProvider()
	if err != nil {
		// Log error but don't fail - we'll return error when CreateMessage is called
		if h.logger != nil {
			h.logger.Printf("Failed to create provider: %v", err)
		}
	} else {
		h.provider = provider
	}

	return h
}

// CreateMessage handles the sampling/createMessage request
func (h *Handler) CreateMessage(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req CreateMessageRequest
	if err := protocol.FlexibleParseParams(params, &req); err != nil {
		return nil, fmt.Errorf("invalid request parameters: %w", err)
	}

	// Validate request
	if len(req.Messages) == 0 {
		return nil, fmt.Errorf("messages array cannot be empty")
	}
	if req.MaxTokens <= 0 {
		return nil, fmt.Errorf("maxTokens must be positive")
	}

	// Check if provider is available
	if h.provider == nil {
		return nil, fmt.Errorf("sampling provider not configured")
	}

	// Log request if enabled
	if h.logger != nil {
		h.logger.Printf("Processing sampling request with %d messages", len(req.Messages))
	}

	// Convert MCP messages to provider messages
	providerMessages := make([]providers.Message, 0, len(req.Messages))
	for _, msg := range req.Messages {
		content := ""
		if msg.Content.Text != "" {
			content = msg.Content.Text
		} else if len(msg.Content.Data) > 0 {
			// Handle structured content by converting to string
			content = string(msg.Content.Data)
		}

		providerMessages = append(providerMessages, providers.Message{
			Role:    msg.Role,
			Content: content,
		})
	}

	// Determine model to use
	model := h.determineModel(&req)

	// Build provider request
	providerReq := &providers.Request{
		Messages:      providerMessages,
		Model:         model,
		Temperature:   req.Temperature,
		MaxTokens:     req.MaxTokens,
		StopSequences: req.StopSequences,
		SystemPrompt:  req.SystemPrompt,
	}

	// Call provider
	resp, err := h.provider.CreateMessage(ctx, providerReq)
	if err != nil {
		if h.logger != nil {
			h.logger.Printf("Provider error: %v", err)
		}
		return nil, fmt.Errorf("failed to create message: %w", err)
	}

	// Log response if enabled
	if h.logger != nil {
		h.logger.Printf("Received response from %s model %s", h.provider.GetName(), resp.Model)
		if resp.Usage != nil {
			h.logger.Printf("Token usage: prompt=%d, completion=%d, total=%d",
				resp.Usage.PromptTokens, resp.Usage.CompletionTokens, resp.Usage.TotalTokens)
		}
	}

	// Build response
	response := CreateMessageResponse{
		Role: "assistant",
		Content: SamplingMessageContent{
			Type: "text",
			Text: resp.Content,
		},
		Model:      resp.Model,
		StopReason: resp.StopReason,
	}

	return response, nil
}

// determineModel determines which model to use based on request and preferences
func (h *Handler) determineModel(req *CreateMessageRequest) string {
	// Check if model hints are provided
	if req.ModelPreferences != nil && len(req.ModelPreferences.Hints) > 0 {
		for _, hint := range req.ModelPreferences.Hints {
			if hint.Name != "" {
				// Map the model name to provider-specific model
				mapped := h.config.MapModel(hint.Name)
				if h.provider.IsModelSupported(mapped) {
					return mapped
				}
			}
		}
	}

	// Use default model
	return h.config.DefaultModel
}

// GetCapabilities returns the sampling capabilities
func (h *Handler) GetCapabilities() map[string]interface{} {
	var models []string

	if h.provider != nil {
		// Return actual supported models from the provider
		models = h.provider.GetSupportedModels()
	} else {
		// Return generic model names if provider not initialized
		models = []string{
			"claude-3-opus",
			"claude-3-sonnet",
			"claude-3-haiku",
			"gpt-4",
			"gpt-3.5-turbo",
		}
	}

	return map[string]interface{}{
		"sampling": map[string]interface{}{
			"enabled": h.provider != nil,
			"models":  models,
		},
	}
}
