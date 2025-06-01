package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

// TestJSONRPCIDTypePreservation tests that ID types are preserved exactly
// between request and response for TypeScript client compatibility
func TestJSONRPCIDTypePreservation(t *testing.T) {
	tests := []struct {
		name     string
		reqJSON  string
		wantID   interface{}
		wantType string
	}{
		{
			name:     "string ID",
			reqJSON:  `{"jsonrpc":"2.0","id":"test-123","method":"ping"}`,
			wantID:   "test-123",
			wantType: "string",
		},
		{
			name:     "number ID (int)",
			reqJSON:  `{"jsonrpc":"2.0","id":42,"method":"ping"}`,
			wantID:   float64(42), // JSON unmarshals numbers as float64
			wantType: "float64",
		},
		{
			name:     "number ID (float)",
			reqJSON:  `{"jsonrpc":"2.0","id":3.14,"method":"ping"}`,
			wantID:   3.14,
			wantType: "float64",
		},
		{
			name:     "null ID",
			reqJSON:  `{"jsonrpc":"2.0","id":null,"method":"ping"}`,
			wantID:   nil,
			wantType: "nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Parse request
			parsed, err := ParseJSONRPCMessage([]byte(tt.reqJSON))
			if err != nil {
				t.Fatalf("Failed to parse request: %v", err)
			}

			if parsed.Request == nil {
				t.Fatal("Expected request, got nil")
			}

			// Check ID type and value preservation
			req := parsed.Request
			if !reflect.DeepEqual(req.ID, tt.wantID) {
				t.Errorf("ID value mismatch: got %v (%T), want %v (%T)",
					req.ID, req.ID, tt.wantID, tt.wantID)
			}

			// Test response creation preserves ID type
			resp := &JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID, // This should preserve exact type
				Result:  "pong",
			}

			// Marshal response back to JSON
			respJSON, err := json.Marshal(resp)
			if err != nil {
				t.Fatalf("Failed to marshal response: %v", err)
			}

			// Parse response back to verify ID preservation
			var respParsed map[string]interface{}
			if err := json.Unmarshal(respJSON, &respParsed); err != nil {
				t.Fatalf("Failed to unmarshal response: %v", err)
			}

			// Verify ID in response matches original
			if !reflect.DeepEqual(respParsed["id"], tt.wantID) {
				t.Errorf("Response ID mismatch: got %v (%T), want %v (%T)",
					respParsed["id"], respParsed["id"], tt.wantID, tt.wantID)
			}
		})
	}
}

// TestJSONRPCIDEdgeCases tests edge cases that might break TypeScript clients
func TestJSONRPCIDEdgeCases(t *testing.T) {
	tests := []struct {
		name      string
		reqJSON   string
		expectErr bool
	}{
		{
			name:      "missing ID (valid)",
			reqJSON:   `{"jsonrpc":"2.0","method":"ping"}`,
			expectErr: false,
		},
		{
			name:      "zero ID",
			reqJSON:   `{"jsonrpc":"2.0","id":0,"method":"ping"}`,
			expectErr: false,
		},
		{
			name:      "empty string ID",
			reqJSON:   `{"jsonrpc":"2.0","id":"","method":"ping"}`,
			expectErr: false,
		},
		{
			name:      "negative ID",
			reqJSON:   `{"jsonrpc":"2.0","id":-1,"method":"ping"}`,
			expectErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := ParseJSONRPCMessage([]byte(tt.reqJSON))

			if tt.expectErr && err == nil {
				t.Error("Expected error but got none")
			}
			if !tt.expectErr && err != nil {
				t.Errorf("Unexpected error: %v", err)
			}

			if err == nil && parsed.Request != nil {
				// Test that response creation works
				resp := &JSONRPCResponse{
					JSONRPC: "2.0",
					ID:      parsed.Request.ID,
					Result:  "pong",
				}

				_, err := json.Marshal(resp)
				if err != nil {
					t.Errorf("Failed to marshal response: %v", err)
				}
			}
		})
	}
}
