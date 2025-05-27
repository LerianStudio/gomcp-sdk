package discovery

import (
	"context"
	"testing"
	"time"

	"github.com/fredcamaral/gomcp-sdk/protocol"
)

func TestHandlerLoader(t *testing.T) {
	loader := NewHandlerLoader()
	ctx := context.Background()

	// Test embedded handler loading
	t.Run("LoadEmbeddedHandler", func(t *testing.T) {
		tool := protocol.Tool{
			Name:        "echo",
			Description: "Echo tool",
		}

		config := &HandlerConfig{
			Type: HandlerTypeEmbedded,
		}

		// Load handler
		err := loader.LoadHandler(tool, config, "test")
		if err != nil {
			t.Fatalf("Failed to load handler: %v", err)
		}

		// Get handler
		handler, err := loader.GetHandler("echo")
		if err != nil {
			t.Fatalf("Failed to get handler: %v", err)
		}

		// Test handler
		result, err := handler.Handle(ctx, map[string]interface{}{
			"message": "test message",
		})
		if err != nil {
			t.Fatalf("Handler failed: %v", err)
		}

		// Check result
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}

		if echo, ok := resultMap["echo"].(string); !ok || echo != "test message" {
			t.Errorf("Expected echo 'test message', got %v", resultMap["echo"])
		}
	})

	// Test duplicate handler loading
	t.Run("LoadDuplicateHandler", func(t *testing.T) {
		tool := protocol.Tool{
			Name:        "echo",
			Description: "Echo tool",
		}

		config := &HandlerConfig{
			Type: HandlerTypeEmbedded,
		}

		// Should fail since handler already loaded
		err := loader.LoadHandler(tool, config, "test")
		if err == nil {
			t.Error("Expected error loading duplicate handler")
		}
	})

	// Test unload handler
	t.Run("UnloadHandler", func(t *testing.T) {
		err := loader.UnloadHandler("echo")
		if err != nil {
			t.Fatalf("Failed to unload handler: %v", err)
		}

		// Should not be able to get handler after unload
		_, err = loader.GetHandler("echo")
		if err == nil {
			t.Error("Expected error getting unloaded handler")
		}
	})

	// Test configurable handler
	t.Run("ConfigurableHandler", func(t *testing.T) {
		tool := protocol.Tool{
			Name:        "configurable_echo",
			Description: "Configurable echo tool",
		}

		config := &HandlerConfig{
			Type: HandlerTypeEmbedded,
			Config: map[string]interface{}{
				"prefix": "[PREFIX] ",
				"suffix": " [SUFFIX]",
			},
		}

		// Load handler
		err := loader.LoadHandler(tool, config, "test")
		if err != nil {
			t.Fatalf("Failed to load configurable handler: %v", err)
		}
		defer func() {
			if err := loader.UnloadHandler("configurable_echo"); err != nil {
				t.Logf("Failed to unload handler: %v", err)
			}
		}()

		// Get handler
		handler, err := loader.GetHandler("configurable_echo")
		if err != nil {
			t.Fatalf("Failed to get handler: %v", err)
		}

		// Test handler
		result, err := handler.Handle(ctx, map[string]interface{}{
			"message": "test",
		})
		if err != nil {
			t.Fatalf("Handler failed: %v", err)
		}

		// Check result
		resultMap, ok := result.(map[string]interface{})
		if !ok {
			t.Fatalf("Expected map result, got %T", result)
		}

		if echo, ok := resultMap["echo"].(string); !ok || echo != "[PREFIX] test [SUFFIX]" {
			t.Errorf("Expected configured echo, got %v", resultMap["echo"])
		}
	})
}

func TestEmbeddedHandlers(t *testing.T) {
	ctx := context.Background()

	t.Run("CalculateHandler", func(t *testing.T) {
		tests := []struct {
			name      string
			params    map[string]interface{}
			wantError bool
			checkFunc func(t *testing.T, result interface{})
		}{
			{
				name: "Add",
				params: map[string]interface{}{
					"operation": "add",
					"a":         10.0,
					"b":         5.0,
				},
				checkFunc: func(t *testing.T, result interface{}) {
					res := result.(map[string]interface{})
					if res["result"].(float64) != 15.0 {
						t.Errorf("Expected 15, got %v", res["result"])
					}
				},
			},
			{
				name: "Divide",
				params: map[string]interface{}{
					"operation": "divide",
					"a":         20.0,
					"b":         4.0,
				},
				checkFunc: func(t *testing.T, result interface{}) {
					res := result.(map[string]interface{})
					if res["result"].(float64) != 5.0 {
						t.Errorf("Expected 5, got %v", res["result"])
					}
				},
			},
			{
				name: "DivideByZero",
				params: map[string]interface{}{
					"operation": "divide",
					"a":         10.0,
					"b":         0.0,
				},
				wantError: true,
			},
			{
				name: "Sqrt",
				params: map[string]interface{}{
					"operation": "sqrt",
					"a":         16.0,
				},
				checkFunc: func(t *testing.T, result interface{}) {
					res := result.(map[string]interface{})
					value := res["result"].(float64)
					if value < 3.99 || value > 4.01 {
						t.Errorf("Expected ~4, got %v", value)
					}
				},
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				result, err := calculateHandler(ctx, tt.params)
				if tt.wantError {
					if err == nil {
						t.Error("Expected error but got none")
					}
					return
				}
				if err != nil {
					t.Fatalf("Unexpected error: %v", err)
				}
				if tt.checkFunc != nil {
					tt.checkFunc(t, result)
				}
			})
		}
	})

	t.Run("TimeHandler", func(t *testing.T) {
		result, err := timeHandler(ctx, map[string]interface{}{
			"format": "Kitchen",
		})
		if err != nil {
			t.Fatalf("Unexpected error: %v", err)
		}

		res := result.(map[string]interface{})
		if _, ok := res["time"].(string); !ok {
			t.Error("Expected time string in result")
		}
		if _, ok := res["zone"].(string); !ok {
			t.Error("Expected zone string in result")
		}
	})
}

func TestSubprocessHandler(t *testing.T) {
	// Skip if Python is not available
	if !IsPythonAvailable() {
		t.Skip("Python not available")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := RunSubprocessHandlerExample(ctx); err != nil {
		t.Errorf("Subprocess handler test failed: %v", err)
	}
}