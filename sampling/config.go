package sampling

import (
	"fmt"
	"os"
	"strings"

	"github.com/fredcamaral/gomcp-sdk/sampling/providers"
)

// Config holds the configuration for the sampling handler
type Config struct {
	// Provider specifies which LLM provider to use (openai, anthropic)
	Provider string
	
	// APIKey is the API key for the selected provider
	APIKey string
	
	// DefaultModel is the default model to use if none is specified
	DefaultModel string
	
	// EnableLogging enables detailed logging
	EnableLogging bool
	
	// RetryConfig specifies retry behavior
	RetryConfig *providers.RetryConfig
	
	// ModelMapping allows mapping from generic model names to provider-specific ones
	ModelMapping map[string]string
}

// NewConfig creates a new configuration from environment variables
func NewConfig() *Config {
	config := &Config{
		Provider:      getEnv("MCP_SAMPLING_PROVIDER", "anthropic"),
		APIKey:        getEnv("MCP_SAMPLING_API_KEY", ""),
		DefaultModel:  getEnv("MCP_SAMPLING_DEFAULT_MODEL", ""),
		EnableLogging: getEnvBool("MCP_SAMPLING_ENABLE_LOGGING", false),
		RetryConfig:   providers.DefaultRetryConfig(),
		ModelMapping:  make(map[string]string),
	}
	
	// Set provider-specific API key if generic one not set
	if config.APIKey == "" {
		switch config.Provider {
		case "openai":
			config.APIKey = getEnv("OPENAI_API_KEY", "")
		case "anthropic":
			config.APIKey = getEnv("ANTHROPIC_API_KEY", "")
		}
	}
	
	// Set default model based on provider
	if config.DefaultModel == "" {
		switch config.Provider {
		case "openai":
			config.DefaultModel = "gpt-3.5-turbo"
		case "anthropic":
			config.DefaultModel = "claude-3-haiku-20240307"
		}
	}
	
	// Setup model mappings for cross-provider compatibility
	config.setupModelMappings()
	
	return config
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Provider == "" {
		return fmt.Errorf("provider must be specified")
	}
	
	if c.Provider != "openai" && c.Provider != "anthropic" {
		return fmt.Errorf("unsupported provider: %s (supported: openai, anthropic)", c.Provider)
	}
	
	if c.APIKey == "" {
		return fmt.Errorf("API key must be provided for provider %s", c.Provider)
	}
	
	return nil
}

// CreateProvider creates a provider instance based on the configuration
func (c *Config) CreateProvider() (providers.Provider, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}
	
	var provider providers.Provider
	
	switch c.Provider {
	case "openai":
		p := providers.NewOpenAIProvider(c.APIKey)
		if c.RetryConfig != nil {
			p.SetRetryConfig(c.RetryConfig)
		}
		provider = p
		
	case "anthropic":
		p := providers.NewAnthropicProvider(c.APIKey)
		if c.RetryConfig != nil {
			p.SetRetryConfig(c.RetryConfig)
		}
		provider = p
		
	default:
		return nil, fmt.Errorf("unsupported provider: %s", c.Provider)
	}
	
	return provider, nil
}

// MapModel maps a generic model name to a provider-specific one
func (c *Config) MapModel(model string) string {
	// First check explicit mappings
	if mapped, ok := c.ModelMapping[model]; ok {
		return mapped
	}
	
	// Handle generic model names
	lowerModel := strings.ToLower(model)
	
	switch c.Provider {
	case "openai":
		switch {
		case strings.Contains(lowerModel, "gpt-4"):
			return "gpt-4"
		case strings.Contains(lowerModel, "gpt-3"):
			return "gpt-3.5-turbo"
		default:
			return model
		}
		
	case "anthropic":
		switch {
		case strings.Contains(lowerModel, "claude-3-opus"):
			return "claude-3-opus-20240229"
		case strings.Contains(lowerModel, "claude-3-sonnet"):
			return "claude-3-sonnet-20240229"
		case strings.Contains(lowerModel, "claude-3-haiku"):
			return "claude-3-haiku-20240307"
		case strings.Contains(lowerModel, "claude-2"):
			return "claude-2.1"
		case strings.Contains(lowerModel, "claude-instant"):
			return "claude-instant-1.2"
		default:
			return model
		}
	}
	
	return model
}

func (c *Config) setupModelMappings() {
	// Setup cross-provider model mappings
	switch c.Provider {
	case "openai":
		c.ModelMapping["claude-3-opus"] = "gpt-4"
		c.ModelMapping["claude-3-sonnet"] = "gpt-4"
		c.ModelMapping["claude-3-haiku"] = "gpt-3.5-turbo"
		
	case "anthropic":
		c.ModelMapping["gpt-4"] = "claude-3-opus-20240229"
		c.ModelMapping["gpt-3.5-turbo"] = "claude-3-haiku-20240307"
		c.ModelMapping["gpt-4-turbo"] = "claude-3-sonnet-20240229"
	}
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	
	return strings.ToLower(value) == "true" || value == "1"
}