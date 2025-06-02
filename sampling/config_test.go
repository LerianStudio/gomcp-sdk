package sampling

import (
	"os"
	"testing"

	"github.com/LerianStudio/gomcp-sdk/sampling/providers"
)

func TestNewConfig(t *testing.T) {
	// Save current env vars
	oldProvider := os.Getenv("MCP_SAMPLING_PROVIDER")
	oldAPIKey := os.Getenv("MCP_SAMPLING_API_KEY")
	oldModel := os.Getenv("MCP_SAMPLING_DEFAULT_MODEL")
	oldLogging := os.Getenv("MCP_SAMPLING_ENABLE_LOGGING")
	defer func() {
		if err := os.Setenv("MCP_SAMPLING_PROVIDER", oldProvider); err != nil {
			t.Logf("Failed to restore MCP_SAMPLING_PROVIDER: %v", err)
		}
		if err := os.Setenv("MCP_SAMPLING_API_KEY", oldAPIKey); err != nil {
			t.Logf("Failed to restore MCP_SAMPLING_API_KEY: %v", err)
		}
		if err := os.Setenv("MCP_SAMPLING_DEFAULT_MODEL", oldModel); err != nil {
			t.Logf("Failed to restore MCP_SAMPLING_DEFAULT_MODEL: %v", err)
		}
		if err := os.Setenv("MCP_SAMPLING_ENABLE_LOGGING", oldLogging); err != nil {
			t.Logf("Failed to restore MCP_SAMPLING_ENABLE_LOGGING: %v", err)
		}
	}()

	tests := []struct {
		name         string
		envVars      map[string]string
		wantProvider string
		wantModel    string
		wantLogging  bool
	}{
		{
			name:         "default config",
			envVars:      map[string]string{},
			wantProvider: "anthropic",
			wantModel:    "claude-3-haiku-20240307",
			wantLogging:  false,
		},
		{
			name: "openai provider",
			envVars: map[string]string{
				"MCP_SAMPLING_PROVIDER":      "openai",
				"MCP_SAMPLING_DEFAULT_MODEL": "gpt-4",
			},
			wantProvider: "openai",
			wantModel:    "gpt-4",
			wantLogging:  false,
		},
		{
			name: "with logging enabled",
			envVars: map[string]string{
				"MCP_SAMPLING_ENABLE_LOGGING": "true",
			},
			wantProvider: "anthropic",
			wantModel:    "claude-3-haiku-20240307",
			wantLogging:  true,
		},
		{
			name: "provider-specific API key",
			envVars: map[string]string{
				"MCP_SAMPLING_PROVIDER": "openai",
				"OPENAI_API_KEY":        "test-openai-key",
			},
			wantProvider: "openai",
			wantModel:    "gpt-3.5-turbo",
			wantLogging:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear env vars
			os.Clearenv()

			// Set test env vars
			for k, v := range tt.envVars {
				if err := os.Setenv(k, v); err != nil {
					t.Fatalf("Failed to set env var %s: %v", k, err)
				}
			}

			config := NewConfig()

			if config.Provider != tt.wantProvider {
				t.Errorf("Provider = %q, want %q", config.Provider, tt.wantProvider)
			}

			if config.DefaultModel != tt.wantModel {
				t.Errorf("DefaultModel = %q, want %q", config.DefaultModel, tt.wantModel)
			}

			if config.EnableLogging != tt.wantLogging {
				t.Errorf("EnableLogging = %v, want %v", config.EnableLogging, tt.wantLogging)
			}
		})
	}
}

func TestConfig_Validate(t *testing.T) {
	tests := []struct {
		name    string
		config  *Config
		wantErr bool
	}{
		{
			name: "valid config",
			config: &Config{
				Provider: "openai",
				APIKey:   "test-key",
			},
			wantErr: false,
		},
		{
			name: "missing provider",
			config: &Config{
				Provider: "",
				APIKey:   "test-key",
			},
			wantErr: true,
		},
		{
			name: "unsupported provider",
			config: &Config{
				Provider: "unsupported",
				APIKey:   "test-key",
			},
			wantErr: true,
		},
		{
			name: "missing API key",
			config: &Config{
				Provider: "openai",
				APIKey:   "",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestConfig_CreateProvider(t *testing.T) {
	tests := []struct {
		name         string
		config       *Config
		wantProvider string
		wantErr      bool
	}{
		{
			name: "create openai provider",
			config: &Config{
				Provider:     "openai",
				APIKey:       "test-key",
				DefaultModel: "gpt-4",
				RetryConfig:  providers.DefaultRetryConfig(),
			},
			wantProvider: "openai",
			wantErr:      false,
		},
		{
			name: "create anthropic provider",
			config: &Config{
				Provider:     "anthropic",
				APIKey:       "test-key",
				DefaultModel: "claude-3-opus",
				RetryConfig:  providers.DefaultRetryConfig(),
			},
			wantProvider: "anthropic",
			wantErr:      false,
		},
		{
			name: "invalid config",
			config: &Config{
				Provider: "invalid",
				APIKey:   "test-key",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			provider, err := tt.config.CreateProvider()

			if (err != nil) != tt.wantErr {
				t.Errorf("CreateProvider() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && provider.GetName() != tt.wantProvider {
				t.Errorf("provider name = %q, want %q", provider.GetName(), tt.wantProvider)
			}
		})
	}
}

func TestConfig_MapModel(t *testing.T) {
	tests := []struct {
		name   string
		config *Config
		input  string
		want   string
	}{
		{
			name: "openai gpt-4 variations",
			config: &Config{
				Provider:     "openai",
				ModelMapping: make(map[string]string),
			},
			input: "GPT-4-Turbo",
			want:  "gpt-4",
		},
		{
			name: "anthropic claude variations",
			config: &Config{
				Provider:     "anthropic",
				ModelMapping: make(map[string]string),
			},
			input: "Claude-3-Opus",
			want:  "claude-3-opus-20240229",
		},
		{
			name: "cross-provider mapping openai to anthropic",
			config: &Config{
				Provider: "anthropic",
				ModelMapping: map[string]string{
					"gpt-4": "claude-3-opus-20240229",
				},
			},
			input: "gpt-4",
			want:  "claude-3-opus-20240229",
		},
		{
			name: "cross-provider mapping anthropic to openai",
			config: &Config{
				Provider: "openai",
				ModelMapping: map[string]string{
					"claude-3-opus": "gpt-4",
				},
			},
			input: "claude-3-opus",
			want:  "gpt-4",
		},
		{
			name: "no mapping passthrough",
			config: &Config{
				Provider:     "openai",
				ModelMapping: make(map[string]string),
			},
			input: "custom-model-123",
			want:  "custom-model-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.config.setupModelMappings()
			got := tt.config.MapModel(tt.input)
			if got != tt.want {
				t.Errorf("MapModel(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}

func TestGetEnv(t *testing.T) {
	// Save current env
	oldValue := os.Getenv("TEST_ENV_VAR")
	defer func() {
		if err := os.Setenv("TEST_ENV_VAR", oldValue); err != nil {
			t.Logf("Failed to restore env var: %v", err)
		}
	}()

	tests := []struct {
		name         string
		envValue     string
		defaultValue string
		want         string
	}{
		{
			name:         "env var set",
			envValue:     "custom-value",
			defaultValue: "default",
			want:         "custom-value",
		},
		{
			name:         "env var not set",
			envValue:     "",
			defaultValue: "default",
			want:         "default",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := os.Setenv("TEST_ENV_VAR", tt.envValue); err != nil {
				t.Fatalf("Failed to set env var: %v", err)
			}
			got := getEnv("TEST_ENV_VAR", tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnv() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestGetEnvBool(t *testing.T) {
	// Save current env
	oldValue := os.Getenv("TEST_BOOL_VAR")
	defer os.Setenv("TEST_BOOL_VAR", oldValue)

	tests := []struct {
		name         string
		envValue     string
		defaultValue bool
		want         bool
	}{
		{
			name:         "true string",
			envValue:     "true",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "TRUE uppercase",
			envValue:     "TRUE",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "1 as true",
			envValue:     "1",
			defaultValue: false,
			want:         true,
		},
		{
			name:         "false string",
			envValue:     "false",
			defaultValue: true,
			want:         false,
		},
		{
			name:         "empty uses default",
			envValue:     "",
			defaultValue: true,
			want:         true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			os.Setenv("TEST_BOOL_VAR", tt.envValue)
			got := getEnvBool("TEST_BOOL_VAR", tt.defaultValue)
			if got != tt.want {
				t.Errorf("getEnvBool() = %v, want %v", got, tt.want)
			}
		})
	}
}
