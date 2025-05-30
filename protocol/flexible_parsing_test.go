package protocol

import (
	"encoding/json"
	"testing"
)

// TestFlexibleParseParams tests the flexible parameter parsing with various input formats
func TestFlexibleParseParams(t *testing.T) {
	type TestStruct struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
		Flag  bool   `json:"flag,omitempty"`
	}

	tests := []struct {
		name      string
		input     interface{}
		expected  TestStruct
		expectErr bool
	}{
		{
			name:     "map input",
			input:    map[string]interface{}{"name": "test", "value": 42, "flag": true},
			expected: TestStruct{Name: "test", Value: 42, Flag: true},
		},
		{
			name:     "json bytes",
			input:    json.RawMessage(`{"name":"test","value":42,"flag":true}`),
			expected: TestStruct{Name: "test", Value: 42, Flag: true},
		},
		{
			name:     "json string",
			input:    `{"name":"test","value":42,"flag":true}`,
			expected: TestStruct{Name: "test", Value: 42, Flag: true},
		},
		{
			name:     "case insensitive fields",
			input:    map[string]interface{}{"NAME": "test", "VALUE": 42, "FLAG": true},
			expected: TestStruct{Name: "test", Value: 42, Flag: true},
		},
		{
			name:     "type conversion int to string",
			input:    map[string]interface{}{"name": 123, "value": 42},
			expected: TestStruct{Name: "123", Value: 42},
		},
		{
			name:     "type conversion float to int",
			input:    map[string]interface{}{"name": "test", "value": 42.7},
			expected: TestStruct{Name: "test", Value: 42},
		},
		{
			name:     "nil input",
			input:    nil,
			expected: TestStruct{},
		},
		{
			name:      "invalid json string",
			input:     `{invalid json}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result TestStruct
			err := FlexibleParseParams(tt.input, &result)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result != tt.expected {
				t.Errorf("Expected %+v, got %+v", tt.expected, result)
			}
		})
	}
}

// TestFlexibleUnmarshal tests flexible JSON unmarshaling
func TestFlexibleUnmarshal(t *testing.T) {
	type ComplexStruct struct {
		Text   string                 `json:"text"`
		Number int                    `json:"number"`
		Items  []string               `json:"items"`
		Meta   map[string]interface{} `json:"meta"`
	}

	tests := []struct {
		name      string
		input     string
		expected  ComplexStruct
		expectErr bool
	}{
		{
			name:  "normal json",
			input: `{"text":"hello","number":42,"items":["a","b"],"meta":{"key":"value"}}`,
			expected: ComplexStruct{
				Text:   "hello",
				Number: 42,
				Items:  []string{"a", "b"},
				Meta:   map[string]interface{}{"key": "value"},
			},
		},
		{
			name:  "case insensitive",
			input: `{"TEXT":"hello","NUMBER":42,"ITEMS":["a","b"],"META":{"key":"value"}}`,
			expected: ComplexStruct{
				Text:   "hello",
				Number: 42,
				Items:  []string{"a", "b"},
				Meta:   map[string]interface{}{"key": "value"},
			},
		},
		{
			name:      "invalid json",
			input:     `{invalid}`,
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result ComplexStruct
			err := FlexibleUnmarshal([]byte(tt.input), &result)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
				return
			}

			if result.Text != tt.expected.Text ||
				result.Number != tt.expected.Number ||
				len(result.Items) != len(tt.expected.Items) {
				t.Errorf("Expected %+v, got %+v", tt.expected, result)
			}
		})
	}
}

// TestValidateAndParseParams tests validation with flexible parsing
func TestValidateAndParseParams(t *testing.T) {
	type RequiredStruct struct {
		Required string `json:"required"`
		Optional string `json:"optional,omitempty"`
	}

	tests := []struct {
		name      string
		input     interface{}
		required  []string
		expectErr bool
	}{
		{
			name:     "valid with all required",
			input:    map[string]interface{}{"required": "value", "optional": "opt"},
			required: []string{"required"},
		},
		{
			name:      "missing required field",
			input:     map[string]interface{}{"optional": "opt"},
			required:  []string{"required"},
			expectErr: true,
		},
		{
			name:     "case insensitive required check",
			input:    map[string]interface{}{"REQUIRED": "value"},
			required: []string{"required"},
		},
		{
			name:     "no required fields",
			input:    map[string]interface{}{"optional": "opt"},
			required: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var result RequiredStruct
			err := ValidateAndParseParams(tt.input, &result, tt.required)

			if tt.expectErr {
				if err == nil {
					t.Errorf("Expected error but got none")
				}
				return
			}

			if err != nil {
				t.Errorf("Unexpected error: %v", err)
			}
		})
	}
}

// TestBackwardCompatibility ensures old code still works
func TestBackwardCompatibility(t *testing.T) {
	type InitializeRequest struct {
		ProtocolVersion string `json:"protocolVersion"`
		ClientInfo      struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"clientInfo"`
	}

	// Test that normal JSON-RPC requests still work perfectly
	normalJSON := `{
		"protocolVersion": "2024-11-05",
		"clientInfo": {
			"name": "test-client",
			"version": "1.0.0"
		}
	}`

	var result InitializeRequest
	err := FlexibleParseParams(json.RawMessage(normalJSON), &result)
	if err != nil {
		t.Errorf("Backward compatibility failed: %v", err)
	}

	if result.ProtocolVersion != "2024-11-05" {
		t.Errorf("Expected protocol version 2024-11-05, got %s", result.ProtocolVersion)
	}

	if result.ClientInfo.Name != "test-client" {
		t.Errorf("Expected client name test-client, got %s", result.ClientInfo.Name)
	}
}

// TestComplexTypesSupport tests support for various Go types
func TestComplexTypesSupport(t *testing.T) {
	type ComplexTypes struct {
		Interfaces []interface{}          `json:"interfaces"`
		MapSlice   []map[string]string    `json:"mapSlice"`
		Numbers    []int                  `json:"numbers"`
		Nested     map[string]interface{} `json:"nested"`
	}

	input := map[string]interface{}{
		"interfaces": []interface{}{"string", 42, true},
		"mapSlice":   []interface{}{map[string]interface{}{"key": "value"}},
		"numbers":    []interface{}{1, 2, 3}, // Use all numeric types
		"nested": map[string]interface{}{
			"level2": map[string]interface{}{
				"value": "deep",
			},
		},
	}

	var result ComplexTypes
	err := FlexibleParseParams(input, &result)
	if err != nil {
		t.Errorf("Complex types parsing failed: %v", err)
	}

	if len(result.Interfaces) != 3 {
		t.Errorf("Expected 3 interfaces, got %d", len(result.Interfaces))
	}

	if len(result.Numbers) != 3 {
		t.Errorf("Expected 3 numbers, got %d", len(result.Numbers))
	}
}