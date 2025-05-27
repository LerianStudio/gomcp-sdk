package discovery

import (
	"context"
	"fmt"
	"time"

	"github.com/fredcamaral/gomcp-sdk/protocol"
)

// Example embedded handlers for demonstration

func init() {
	// Register some example embedded handlers
	RegisterEmbeddedHandler("echo", protocol.ToolHandlerFunc(echoHandler))
	RegisterEmbeddedHandler("time", protocol.ToolHandlerFunc(timeHandler))
	RegisterEmbeddedHandler("calculate", protocol.ToolHandlerFunc(calculateHandler))
}

// echoHandler is a simple echo tool handler
func echoHandler(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	message, ok := params["message"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'message' parameter")
	}

	return map[string]interface{}{
		"echo": message,
		"timestamp": time.Now().Format(time.RFC3339),
	}, nil
}

// timeHandler returns the current time in various formats
func timeHandler(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	format, _ := params["format"].(string)
	if format == "" {
		format = "RFC3339"
	}

	now := time.Now()
	var formatted string

	switch format {
	case "RFC3339":
		formatted = now.Format(time.RFC3339)
	case "Unix":
		formatted = fmt.Sprintf("%d", now.Unix())
	case "UnixNano":
		formatted = fmt.Sprintf("%d", now.UnixNano())
	case "Kitchen":
		formatted = now.Format(time.Kitchen)
	default:
		// Try to use format as a custom format string
		formatted = now.Format(format)
	}

	return map[string]interface{}{
		"time": formatted,
		"zone": now.Location().String(),
	}, nil
}

// calculateHandler performs simple calculations
func calculateHandler(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	operation, ok := params["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'operation' parameter")
	}

	a, ok := getFloat64(params["a"])
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'a' parameter")
	}

	b, ok := getFloat64(params["b"])
	if !ok && operation != "sqrt" && operation != "abs" {
		return nil, fmt.Errorf("missing or invalid 'b' parameter")
	}

	var result float64
	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return nil, fmt.Errorf("division by zero")
		}
		result = a / b
	case "power":
		result = 1
		for i := 0; i < int(b); i++ {
			result *= a
		}
	case "sqrt":
		if a < 0 {
			return nil, fmt.Errorf("cannot calculate square root of negative number")
		}
		// Simple Newton's method
		result = a
		for i := 0; i < 10; i++ {
			result = (result + a/result) / 2
		}
	case "abs":
		if a < 0 {
			result = -a
		} else {
			result = a
		}
	default:
		return nil, fmt.Errorf("unknown operation: %s", operation)
	}

	return map[string]interface{}{
		"result": result,
		"operation": operation,
		"inputs": map[string]interface{}{
			"a": a,
			"b": b,
		},
	}, nil
}

// getFloat64 safely extracts a float64 from an interface{}
func getFloat64(v interface{}) (float64, bool) {
	switch val := v.(type) {
	case float64:
		return val, true
	case float32:
		return float64(val), true
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case int32:
		return float64(val), true
	default:
		return 0, false
	}
}

// ConfigurableEchoHandler is an example of a configurable handler
type ConfigurableEchoHandler struct {
	prefix string
	suffix string
}

// Configure implements the ConfigurableHandler interface
func (h *ConfigurableEchoHandler) Configure(config map[string]interface{}) error {
	if prefix, ok := config["prefix"].(string); ok {
		h.prefix = prefix
	}
	if suffix, ok := config["suffix"].(string); ok {
		h.suffix = suffix
	}
	return nil
}

// Handle implements the ToolHandler interface
func (h *ConfigurableEchoHandler) Handle(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	message, ok := params["message"].(string)
	if !ok {
		return nil, fmt.Errorf("missing or invalid 'message' parameter")
	}

	return map[string]interface{}{
		"echo": fmt.Sprintf("%s%s%s", h.prefix, message, h.suffix),
		"configured": true,
	}, nil
}

func init() {
	// Register the configurable handler
	RegisterEmbeddedHandler("configurable_echo", &ConfigurableEchoHandler{})
}