// Package testing provides contract testing functionality for MCP
package mcp_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/server"
	"github.com/LerianStudio/gomcp-sdk/transport"
	"github.com/gorilla/websocket"
)

// ContractTestSuite defines the contract test suite
type ContractTestSuite struct {
	server    *server.Server
	httpURL   string
	wsURL     string
	httpTest  *httptest.Server
	transport transport.Transport
	wsCleanup func()
}

// NewContractTestSuite creates a new contract test suite
func NewContractTestSuite(t *testing.T) *ContractTestSuite {
	// Create test server with sample tools
	srv := server.NewServer("test-server", "1.0.0")

	// Add sample tools for testing
	srv.AddTool(protocol.Tool{
		Name:        "echo",
		Description: "Echo input text",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo",
				},
			},
			"required": []string{"message"},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		message, ok := params["message"].(string)
		if !ok {
			return nil, fmt.Errorf("message parameter is required")
		}
		return protocol.NewToolCallResult(protocol.NewContent(message)), nil
	}))

	// Add calculator tool
	srv.AddTool(protocol.Tool{
		Name:        "calculator",
		Description: "Simple calculator",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide"},
				},
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"operation", "a", "b"},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		op, _ := params["operation"].(string)
		a, _ := params["a"].(float64)
		b, _ := params["b"].(float64)

		var result float64
		switch op {
		case "add":
			result = a + b
		case "subtract":
			result = a - b
		case "multiply":
			result = a * b
		case "divide":
			if b == 0 {
				return protocol.NewToolCallError("division by zero"), nil
			}
			result = a / b
		default:
			return nil, fmt.Errorf("unknown operation: %s", op)
		}

		return protocol.NewToolCallResult(protocol.NewContent(fmt.Sprintf("%.2f", result))), nil
	}))

	return &ContractTestSuite{
		server: srv,
	}
}

// SetupHTTP sets up HTTP transport for testing
func (s *ContractTestSuite) SetupHTTP() {
	// Create REST transport
	restTransport := transport.NewRESTTransport(&transport.RESTConfig{
		APIPrefix:  "/api/v1",
		EnableDocs: true,
	})

	// Create test server
	s.httpTest = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		restTransport.ServeHTTP(w, r, s.server)
	}))

	s.httpURL = s.httpTest.URL
}

// SetupWebSocket sets up WebSocket transport for testing
func (s *ContractTestSuite) SetupWebSocket() {
	// Create WebSocket transport
	wsTransport := transport.NewWebSocketTransport(&transport.WebSocketConfig{
		Address:           ":0", // Use random port
		Path:              "/ws",
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		HandshakeTimeout:  10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      30 * time.Second,
		PongTimeout:       60 * time.Second,
		MaxMessageSize:    1024 * 1024, // 1MB
		EnableCompression: false,
		CheckOrigin: func(r *http.Request) bool {
			return true // Allow all origins for testing
		},
	})

	// Start the transport in the background
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		if err := wsTransport.Start(ctx, s.server); err != nil {
			fmt.Printf("WebSocket transport error: %v\n", err)
		}
	}()

	// Wait a bit for server to start
	time.Sleep(100 * time.Millisecond)

	// Get the actual address
	addr := wsTransport.Address()
	s.wsURL = fmt.Sprintf("ws://%s/ws", addr)

	// Store cleanup function
	s.wsCleanup = func() {
		cancel()
		wsTransport.Stop()
	}
}

// TearDown cleans up test resources
func (s *ContractTestSuite) TearDown() {
	if s.httpTest != nil {
		s.httpTest.Close()
	}
	if s.wsCleanup != nil {
		s.wsCleanup()
	}
}

// TestHTTPContractCompliance tests HTTP REST API contract compliance
func (s *ContractTestSuite) TestHTTPContractCompliance(t *testing.T) {
	s.SetupHTTP()
	defer s.TearDown()

	t.Run("GET /api/v1/tools", s.testListTools)
	t.Run("POST /api/v1/tools/{name}", s.testExecuteTool)
	t.Run("GET /api/v1/info", s.testGetInfo)
	t.Run("GET /api/v1/health", s.testGetHealth)
	t.Run("Error Response Format", s.testErrorResponseFormat)
	t.Run("Correlation ID", s.testCorrelationID)
}

func (s *ContractTestSuite) testListTools(t *testing.T) {
	resp, err := http.Get(s.httpURL + "/api/v1/tools")
	if err != nil {
		t.Fatalf("Failed to get tools: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Validate response structure
	tools, ok := result["tools"].([]interface{})
	if !ok {
		t.Error("Response should contain 'tools' array")
	}

	if len(tools) < 2 {
		t.Error("Expected at least 2 tools")
	}

	// Validate tool structure
	for i, toolInterface := range tools {
		tool, ok := toolInterface.(map[string]interface{})
		if !ok {
			t.Errorf("Tool %d should be an object", i)
			continue
		}

		if _, ok := tool["name"]; !ok {
			t.Errorf("Tool %d should have 'name' field", i)
		}
		if _, ok := tool["inputSchema"]; !ok {
			t.Errorf("Tool %d should have 'inputSchema' field", i)
		}
	}
}

func (s *ContractTestSuite) testExecuteTool(t *testing.T) {
	// Test echo tool
	reqBody := map[string]interface{}{
		"message": "Hello, World!",
	}
	jsonBody, _ := json.Marshal(reqBody)

	resp, err := http.Post(s.httpURL+"/api/v1/tools/echo", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Failed to execute echo tool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Validate response structure
	content, ok := result["content"].([]interface{})
	if !ok {
		t.Error("Response should contain 'content' array")
	}

	if len(content) != 1 {
		t.Error("Expected exactly 1 content item")
	}

	// Test calculator tool
	calcReqBody := map[string]interface{}{
		"operation": "add",
		"a":         5.0,
		"b":         3.0,
	}
	jsonBody, _ = json.Marshal(calcReqBody)

	resp, err = http.Post(s.httpURL+"/api/v1/tools/calculator", "application/json", bytes.NewReader(jsonBody))
	if err != nil {
		t.Fatalf("Failed to execute calculator tool: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func (s *ContractTestSuite) testGetInfo(t *testing.T) {
	resp, err := http.Get(s.httpURL + "/api/v1/info")
	if err != nil {
		t.Fatalf("Failed to get info: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Validate required fields
	requiredFields := []string{"protocolVersion", "capabilities", "serverInfo"}
	for _, field := range requiredFields {
		if _, ok := result[field]; !ok {
			t.Errorf("Response should contain '%s' field", field)
		}
	}
}

func (s *ContractTestSuite) testGetHealth(t *testing.T) {
	resp, err := http.Get(s.httpURL + "/api/v1/health")
	if err != nil {
		t.Fatalf("Failed to get health: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	// Validate health response structure
	if status, ok := result["status"].(string); !ok || (status != "healthy" && status != "unhealthy") {
		t.Error("Health response should have valid 'status' field")
	}

	if _, ok := result["timestamp"]; !ok {
		t.Error("Health response should have 'timestamp' field")
	}
}

func (s *ContractTestSuite) testErrorResponseFormat(t *testing.T) {
	// Test with invalid tool name
	resp, err := http.Post(s.httpURL+"/api/v1/tools/nonexistent", "application/json", strings.NewReader("{}"))
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("Expected status 404, got %d", resp.StatusCode)
	}

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		t.Fatalf("Failed to decode error response: %v", err)
	}

	// Validate error response structure
	errorObj, ok := result["error"].(map[string]interface{})
	if !ok {
		t.Error("Error response should contain 'error' object")
		return
	}

	// Check required error fields
	if _, ok := errorObj["code"]; !ok {
		t.Error("Error should have 'code' field")
	}
	if _, ok := errorObj["message"]; !ok {
		t.Error("Error should have 'message' field")
	}
}

func (s *ContractTestSuite) testCorrelationID(t *testing.T) {
	// Test with custom correlation ID header
	req, _ := http.NewRequest("GET", s.httpURL+"/api/v1/tools", nil)
	req.Header.Set("X-Correlation-ID", "test-correlation-123")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("Failed to make request: %v", err)
	}
	defer resp.Body.Close()

	// Check if correlation ID is echoed back
	if correlationID := resp.Header.Get("X-Correlation-ID"); correlationID == "" {
		t.Error("Response should include correlation ID header")
	}
}

// TestWebSocketContractCompliance tests WebSocket contract compliance
func (s *ContractTestSuite) TestWebSocketContractCompliance(t *testing.T) {
	s.SetupWebSocket()
	defer s.TearDown()

	t.Run("WebSocket Connection", s.testWebSocketConnection)
	t.Run("JSON-RPC over WebSocket", s.testJSONRPCOverWebSocket)
	t.Run("Tool Execution via WebSocket", s.testToolExecutionViaWebSocket)
	t.Run("WebSocket Error Handling", s.testWebSocketErrorHandling)
	t.Run("WebSocket Ping/Pong", s.testWebSocketPingPong)
}

// TestJSONRPCCompliance tests JSON-RPC 2.0 compliance
func (s *ContractTestSuite) TestJSONRPCCompliance(t *testing.T) {
	s.SetupHTTP()
	defer s.TearDown()

	t.Run("Valid Request ID Handling", s.testValidRequestID)
	t.Run("Error Code Compliance", s.testErrorCodeCompliance)
	t.Run("Protocol Version", s.testProtocolVersion)
}

func (s *ContractTestSuite) testValidRequestID(t *testing.T) {
	// Test with string ID
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "test-123",
		"method":  "tools/list",
	}
	jsonReq, _ := json.Marshal(req)

	resp, err := http.Post(s.httpURL, "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		t.Fatalf("Failed to make JSON-RPC request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["id"] != "test-123" {
		t.Error("Response should preserve request ID")
	}
}

func (s *ContractTestSuite) testErrorCodeCompliance(t *testing.T) {
	// Test invalid method
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "invalid/method",
	}
	jsonReq, _ := json.Marshal(req)

	resp, err := http.Post(s.httpURL, "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		t.Fatalf("Failed to make JSON-RPC request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if errorObj, ok := result["error"].(map[string]interface{}); ok {
		if code, ok := errorObj["code"].(float64); !ok || code != -32601 {
			t.Error("Invalid method should return -32601 error code")
		}
	} else {
		t.Error("Invalid method should return error response")
	}
}

func (s *ContractTestSuite) testProtocolVersion(t *testing.T) {
	req := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "tools/list",
	}
	jsonReq, _ := json.Marshal(req)

	resp, err := http.Post(s.httpURL, "application/json", bytes.NewReader(jsonReq))
	if err != nil {
		t.Fatalf("Failed to make JSON-RPC request: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&result)

	if result["jsonrpc"] != "2.0" {
		t.Error("Response should include jsonrpc version 2.0")
	}
}

// WebSocket test methods

func (s *ContractTestSuite) testWebSocketConnection(t *testing.T) {
	// Test basic WebSocket connection
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test connection is established
	if conn == nil {
		t.Error("WebSocket connection should be established")
	}
}

func (s *ContractTestSuite) testJSONRPCOverWebSocket(t *testing.T) {
	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send JSON-RPC request
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "test-1",
		"method":  "tools/list",
	}

	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("Failed to send JSON-RPC request: %v", err)
	}

	// Read response
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read JSON-RPC response: %v", err)
	}

	// Validate response structure
	if response["jsonrpc"] != "2.0" {
		t.Error("Response should include jsonrpc version 2.0")
	}

	if response["id"] != "test-1" {
		t.Error("Response should preserve request ID")
	}

	if response["result"] == nil {
		t.Error("Response should include result for successful tools/list request")
	}
}

func (s *ContractTestSuite) testToolExecutionViaWebSocket(t *testing.T) {
	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Test echo tool
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "tool-test-1",
		"method":  "tools/call",
		"params": map[string]interface{}{
			"name": "echo",
			"arguments": map[string]interface{}{
				"message": "Hello WebSocket!",
			},
		},
	}

	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("Failed to send tool call request: %v", err)
	}

	// Read response
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read tool call response: %v", err)
	}

	// Validate response
	if response["jsonrpc"] != "2.0" {
		t.Error("Response should include jsonrpc version 2.0")
	}

	if response["id"] != "tool-test-1" {
		t.Error("Response should preserve request ID")
	}

	if response["result"] == nil {
		t.Error("Response should include result for successful tool call")
	}

	// Validate tool result structure
	result, ok := response["result"].(map[string]interface{})
	if !ok {
		t.Error("Result should be an object")
		return
	}

	content, ok := result["content"].([]interface{})
	if !ok {
		t.Error("Result should contain content array")
		return
	}

	if len(content) != 1 {
		t.Error("Content should contain exactly one item")
	}
}

func (s *ContractTestSuite) testWebSocketErrorHandling(t *testing.T) {
	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Send invalid JSON-RPC request
	request := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      "error-test-1",
		"method":  "nonexistent/method",
	}

	if err := conn.WriteJSON(request); err != nil {
		t.Fatalf("Failed to send invalid request: %v", err)
	}

	// Read error response
	var response map[string]interface{}
	if err := conn.ReadJSON(&response); err != nil {
		t.Fatalf("Failed to read error response: %v", err)
	}

	// Validate error response structure
	if response["jsonrpc"] != "2.0" {
		t.Error("Error response should include jsonrpc version 2.0")
	}

	if response["id"] != "error-test-1" {
		t.Error("Error response should preserve request ID")
	}

	errorObj, ok := response["error"].(map[string]interface{})
	if !ok {
		t.Error("Response should contain error object")
		return
	}

	if _, ok := errorObj["code"]; !ok {
		t.Error("Error should have code field")
	}

	if _, ok := errorObj["message"]; !ok {
		t.Error("Error should have message field")
	}
}

func (s *ContractTestSuite) testWebSocketPingPong(t *testing.T) {
	// Connect to WebSocket
	conn, _, err := websocket.DefaultDialer.Dial(s.wsURL, nil)
	if err != nil {
		t.Fatalf("Failed to connect to WebSocket: %v", err)
	}
	defer conn.Close()

	// Set up pong handler
	pongReceived := make(chan bool, 1)
	conn.SetPongHandler(func(string) error {
		pongReceived <- true
		return nil
	})

	// Send ping
	if err := conn.WriteMessage(websocket.PingMessage, []byte("ping")); err != nil {
		t.Fatalf("Failed to send ping: %v", err)
	}

	// Wait for pong with timeout
	select {
	case <-pongReceived:
		// Pong received successfully
	case <-time.After(5 * time.Second):
		t.Error("Pong response not received within timeout")
	}
}

// RunContractTests runs all contract tests
func RunContractTests(t *testing.T) {
	suite := NewContractTestSuite(t)

	t.Run("HTTP Contract Compliance", suite.TestHTTPContractCompliance)
	t.Run("JSON-RPC Compliance", suite.TestJSONRPCCompliance)
	t.Run("WebSocket Contract Compliance", suite.TestWebSocketContractCompliance)
}
