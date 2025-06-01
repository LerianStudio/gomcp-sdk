package protocol

import (
	"testing"
)

// TestParseJSONRPCMessage_ClaudeDesktopCompatibility tests parsing of various message formats
// that Claude Desktop might send, including error-only responses that were causing issues
func TestParseJSONRPCMessage_ClaudeDesktopCompatibility(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectError bool
		expectType  string // "request", "response", "error"
	}{
		{
			name:       "normal request",
			input:      `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`,
			expectType: "request",
		},
		{
			name:       "error-only response (Claude Desktop format)",
			input:      `{"error":{"code":-32600,"message":"Invalid Request"}}`,
			expectType: "error",
		},
		{
			name:       "success response",
			input:      `{"jsonrpc":"2.0","id":1,"result":{"tools":[]}}`,
			expectType: "response",
		},
		{
			name:       "error response with id",
			input:      `{"jsonrpc":"2.0","id":1,"error":{"code":-32601,"message":"Method not found"}}`,
			expectType: "error",
		},
		{
			name:        "invalid JSON",
			input:       `{invalid json}`,
			expectError: true,
		},
		{
			name:       "minimal error object",
			input:      `{"error":{"message":"Something went wrong"}}`,
			expectType: "error",
		},
		{
			name:       "empty object (unrecognized format)",
			input:      `{}`,
			expectType: "error", // Should be handled gracefully as unrecognized format
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseJSONRPCMessage([]byte(tt.input))

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if parsed == nil {
				t.Errorf("Expected parsed message but got nil")
				return
			}

			switch tt.expectType {
			case "request":
				if parsed.Request == nil {
					t.Errorf("Expected request but got nil")
				}
				if parsed.Response != nil {
					t.Errorf("Expected no response but got one")
				}
				if parsed.IsError {
					t.Errorf("Expected not error but IsError=true")
				}
			case "response":
				if parsed.Response == nil {
					t.Errorf("Expected response but got nil")
				}
				if parsed.Request != nil {
					t.Errorf("Expected no request but got one")
				}
				if parsed.IsError {
					t.Errorf("Expected not error but IsError=true")
				}
			case "error":
				if parsed.Response == nil {
					t.Errorf("Expected response (error) but got nil")
				}
				if parsed.Request != nil {
					t.Errorf("Expected no request but got one")
				}
				if !parsed.IsError {
					t.Errorf("Expected error but IsError=false")
				}
			}
		})
	}
}

// TestParseJSONRPCMessage_ZodStyleErrors tests the specific error format that was causing
// the original Zod validation issues described in the user's error message
func TestParseJSONRPCMessage_ZodStyleErrors(t *testing.T) {
	// This is a Claude Desktop error response that would fail with strict request validation
	claudeDesktopErrorJSON := `{"error":{"code":-32600,"message":"Missing required fields"}}`

	parsed, err := ParseJSONRPCMessage([]byte(claudeDesktopErrorJSON))
	if err != nil {
		t.Fatalf("Should not error on Claude Desktop error format: %v", err)
	}

	if parsed.Request != nil {
		t.Error("Should not parse as request")
	}

	if parsed.Response == nil {
		t.Error("Should parse as response")
	}

	if !parsed.IsError {
		t.Error("Should be marked as error")
	}

	if parsed.Response.Error == nil {
		t.Error("Response should have error field")
	}

	if parsed.Response.Error.Code != -32600 {
		t.Errorf("Expected error code -32600, got %d", parsed.Response.Error.Code)
	}
}
