// Package protocol implements the Model Context Protocol types and interfaces
package protocol

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"reflect"
	"runtime"
	"time"
)

// Version represents the MCP protocol version
const Version = "2024-11-05"

// JSONRPCRequest represents a JSON-RPC request
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id,omitempty"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC response
type JSONRPCResponse struct {
	JSONRPC string        `json:"jsonrpc"`
	ID      interface{}   `json:"id,omitempty"`
	Result  interface{}   `json:"result,omitempty"`
	Error   *JSONRPCError `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error
type JSONRPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// StandardError represents a standardized error format across all transports
type StandardError struct {
	*JSONRPCError
	CorrelationID string                 `json:"correlationId,omitempty"`
	Timestamp     int64                  `json:"timestamp,omitempty"`
	Details       map[string]interface{} `json:"details,omitempty"`
	Stack         string                 `json:"stack,omitempty"`
}

// Error implements the error interface
func (e *JSONRPCError) Error() string {
	return e.Message
}

// Tool represents an MCP tool definition
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// ToolCallRequest represents a tool call request
type ToolCallRequest struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments,omitempty"`
}

// ToolCallResult represents a tool call result
type ToolCallResult struct {
	Content []Content `json:"content"`
	IsError bool      `json:"isError,omitempty"`
}

// Content represents content in MCP responses
type Content struct {
	Type string `json:"type"`
	Text string `json:"text,omitempty"`
}

// Resource represents an MCP resource
type Resource struct {
	URI         string `json:"uri"`
	Name        string `json:"name,omitempty"`
	Description string `json:"description,omitempty"`
	MimeType    string `json:"mimeType,omitempty"`
}

// Prompt represents an MCP prompt template
type Prompt struct {
	Name        string           `json:"name"`
	Description string           `json:"description,omitempty"`
	Arguments   []PromptArgument `json:"arguments,omitempty"`
}

// PromptArgument represents a prompt argument
type PromptArgument struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Required    bool   `json:"required,omitempty"`
}

// ServerCapabilities represents server capabilities
type ServerCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Logging      map[string]interface{} `json:"logging,omitempty"`
	Prompts      *PromptCapability      `json:"prompts,omitempty"`
	Resources    *ResourceCapability    `json:"resources,omitempty"`
	Tools        *ToolCapability        `json:"tools,omitempty"`
	Sampling     *SamplingCapability    `json:"sampling,omitempty"`
	Roots        *RootsCapability       `json:"roots,omitempty"`
}

// PromptCapability represents prompt capabilities
type PromptCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourceCapability represents resource capabilities
type ResourceCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolCapability represents tool capabilities
type ToolCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// SamplingCapability represents sampling capabilities
type SamplingCapability struct {
	// No specific fields defined in the spec yet
}

// RootsCapability represents roots capabilities
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// InitializeRequest represents an initialization request
type InitializeRequest struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ClientInfo         `json:"clientInfo"`
}

// ClientCapabilities represents client capabilities
type ClientCapabilities struct {
	Experimental map[string]interface{} `json:"experimental,omitempty"`
	Sampling     map[string]interface{} `json:"sampling,omitempty"`
}

// ClientInfo represents client information
type ClientInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult represents initialization result
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ServerInfo         `json:"serverInfo"`
}

// ServerInfo represents server information
type ServerInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// ToolHandler defines the interface for tool handlers
type ToolHandler interface {
	Handle(ctx context.Context, params map[string]interface{}) (interface{}, error)
}

// ToolHandlerFunc is a function adapter for ToolHandler
type ToolHandlerFunc func(ctx context.Context, params map[string]interface{}) (interface{}, error)

// Handle implements the ToolHandler interface
func (f ToolHandlerFunc) Handle(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	return f(ctx, params)
}

// Error codes as defined by MCP specification
const (
	ParseError     = -32700
	InvalidRequest = -32600
	MethodNotFound = -32601
	InvalidParams  = -32602
	InternalError  = -32603
)

// NewJSONRPCError creates a new JSON-RPC error
func NewJSONRPCError(code int, message string, data interface{}) *JSONRPCError {
	return &JSONRPCError{
		Code:    code,
		Message: message,
		Data:    data,
	}
}

// NewContent creates new text content
func NewContent(text string) Content {
	return Content{
		Type: "text",
		Text: text,
	}
}

// NewToolCallResult creates a new tool call result
func NewToolCallResult(content ...Content) *ToolCallResult {
	return &ToolCallResult{
		Content: content,
		IsError: false,
	}
}

// NewToolCallError creates a new tool call error result
func NewToolCallError(message string) *ToolCallResult {
	return &ToolCallResult{
		Content: []Content{NewContent(message)},
		IsError: true,
	}
}

// ParsedMessage represents a parsed JSON-RPC message that could be a request, response, or error
type ParsedMessage struct {
	Request  *JSONRPCRequest  `json:"-"`
	Response *JSONRPCResponse `json:"-"`
	IsError  bool             `json:"-"`
}

// ParseJSONRPCMessage attempts to parse a JSON message as either a request or response/error
func ParseJSONRPCMessage(data []byte) (*ParsedMessage, error) {
	// First, try to determine the message type by checking for required fields
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, NewJSONRPCError(ParseError, "Parse error", err.Error())
	}

	// Check if it has a method field (indicates request)
	if _, hasMethod := raw["method"]; hasMethod {
		var req JSONRPCRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return nil, NewJSONRPCError(ParseError, "Invalid request format", err.Error())
		}
		return &ParsedMessage{Request: &req}, nil
	}

	// Check if it has an error field (indicates error response)
	if _, hasError := raw["error"]; hasError {
		var resp JSONRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, NewJSONRPCError(ParseError, "Invalid response format", err.Error())
		}
		return &ParsedMessage{Response: &resp, IsError: true}, nil
	}

	// Check if it has a result field (indicates success response)
	if _, hasResult := raw["result"]; hasResult {
		var resp JSONRPCResponse
		if err := json.Unmarshal(data, &resp); err != nil {
			return nil, NewJSONRPCError(ParseError, "Invalid response format", err.Error())
		}
		return &ParsedMessage{Response: &resp}, nil
	}

	// If none of the above, it might be a malformed message - try to be flexible
	// This handles cases where Claude Desktop sends minimal error objects
	return &ParsedMessage{
		Response: &JSONRPCResponse{
			JSONRPC: "2.0",
			Error:   NewJSONRPCError(InvalidRequest, "Unrecognized message format", string(data)),
		},
		IsError: true,
	}, nil
}

// FlexibleParseParams safely parses JSON-RPC parameters with fallback handling
// This replaces the rigid marshal-unmarshal pattern that causes compatibility issues
func FlexibleParseParams(params interface{}, target interface{}) error {
	if params == nil {
		return nil
	}

	// Handle different input types
	switch p := params.(type) {
	case json.RawMessage:
		// Already JSON bytes - unmarshal directly
		return FlexibleUnmarshal(p, target)
	case []byte:
		// Raw bytes - unmarshal directly
		return FlexibleUnmarshal(p, target)
	case string:
		// JSON string - unmarshal directly
		return FlexibleUnmarshal([]byte(p), target)
	case map[string]interface{}:
		// Already parsed map - convert using reflection
		return mapToStruct(p, target)
	default:
		// Fallback to marshal-unmarshal with better error context
		jsonBytes, err := json.Marshal(params)
		if err != nil {
			return fmt.Errorf("failed to marshal params of type %T: %w", params, err)
		}
		return FlexibleUnmarshal(jsonBytes, target)
	}
}

// FlexibleUnmarshal performs flexible JSON unmarshaling with fallback strategies
func FlexibleUnmarshal(data []byte, target interface{}) error {
	// Try direct unmarshal first
	if err := json.Unmarshal(data, target); err == nil {
		return nil
	}

	// If direct unmarshal fails, try flexible approach
	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Convert map to target struct with field mapping
	return mapToStruct(raw, target)
}

// mapToStruct converts a map to struct using reflection with flexible field matching
func mapToStruct(source map[string]interface{}, target interface{}) error {
	// Get reflection values
	targetValue := reflect.ValueOf(target)
	if targetValue.Kind() != reflect.Ptr {
		return fmt.Errorf("target must be a pointer, got %T", target)
	}

	targetValue = targetValue.Elem()
	if !targetValue.CanSet() {
		return fmt.Errorf("target is not settable")
	}

	targetType := targetValue.Type()

	// Iterate through struct fields
	for i := 0; i < targetValue.NumField(); i++ {
		field := targetValue.Field(i)
		fieldType := targetType.Field(i)

		// Skip unexported fields
		if !field.CanSet() {
			continue
		}

		// Get JSON tag name or use field name
		jsonTag := fieldType.Tag.Get("json")
		fieldName := fieldType.Name
		if jsonTag != "" && jsonTag != "-" {
			// Parse JSON tag (could be "name,omitempty")
			if commaIdx := len(jsonTag); commaIdx > 0 {
				for j, r := range jsonTag {
					if r == ',' {
						commaIdx = j
						break
					}
				}
				fieldName = jsonTag[:commaIdx]
			}
		}

		// Try to find value in source map with case-insensitive matching
		var sourceValue interface{}
		var found bool

		// First try exact match
		if val, ok := source[fieldName]; ok {
			sourceValue = val
			found = true
		} else {
			// Try case-insensitive match
			for key, val := range source {
				if equalFold(key, fieldName) {
					sourceValue = val
					found = true
					break
				}
			}
		}

		if !found {
			continue
		}

		// Set the field value with type conversion
		if err := setFieldValue(field, sourceValue); err != nil {
			return fmt.Errorf("failed to set field %s: %w", fieldName, err)
		}
	}

	return nil
}

// Context keys for correlation ID
type contextKey string

const (
	CorrelationIDKey contextKey = "correlationId"
)

// GenerateCorrelationID generates a new correlation ID
func GenerateCorrelationID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp-based ID if crypto/rand fails
		return fmt.Sprintf("id_%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(bytes)
}

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// GetCorrelationID extracts correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return id
	}
	return ""
}

// NewStandardError creates a new standardized error with correlation ID and stack trace
func NewStandardError(ctx context.Context, code int, message string, data interface{}) *StandardError {
	correlationID := GetCorrelationID(ctx)
	if correlationID == "" {
		correlationID = GenerateCorrelationID()
	}

	var stack string
	if code >= InternalError { // Only include stack trace for server errors
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		stack = string(buf[:n])
	}

	return &StandardError{
		JSONRPCError: &JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
		CorrelationID: correlationID,
		Timestamp:     time.Now().Unix(),
		Details:       make(map[string]interface{}),
		Stack:         stack,
	}
}

// NewStandardErrorFromJSONRPC converts a JSONRPCError to StandardError
func NewStandardErrorFromJSONRPC(ctx context.Context, err *JSONRPCError) *StandardError {
	return NewStandardError(ctx, err.Code, err.Message, err.Data)
}

// ToJSONRPCError converts StandardError back to JSONRPCError for compatibility
func (e *StandardError) ToJSONRPCError() *JSONRPCError {
	return e.JSONRPCError
}

// Error implements the error interface for StandardError
func (e *StandardError) Error() string {
	return e.Message
}
