// Enhanced MCP Server Example
//
// This example demonstrates all the enhanced features implemented in the GoMCP SDK:
// - Centralized error handling middleware
// - HTTP connection pooling
// - API versioning with multiple strategies
// - WebSocket and HTTP transport support
// - Comprehensive contract testing support
//
// To run this example:
//   go run examples/enhanced-server/main.go
//
// Test endpoints:
//   HTTP: curl -H "API-Version: v1.0" http://localhost:8080/api/v1/tools
//   WebSocket: ws://localhost:8081/ws
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LerianStudio/gomcp-sdk/middleware"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/server"
	"github.com/LerianStudio/gomcp-sdk/transport"
	"github.com/LerianStudio/gomcp-sdk/versioning"
)

func main() {
	// Create the MCP server
	srv := createEnhancedServer()

	// Configure error handling
	errorConfig := createErrorHandlingConfig()

	// Configure connection pooling
	poolConfig := createConnectionPoolConfig()

	// Configure API versioning
	versionManager := createVersionManager()

	// Start HTTP transport
	httpAddr := startHTTPTransport(srv, errorConfig, poolConfig, versionManager)
	fmt.Printf("🚀 HTTP server started on %s\n", httpAddr)
	fmt.Printf("   📚 API docs: http://%s/api/v1/docs\n", httpAddr)
	fmt.Printf("   🔍 Health: http://%s/api/v1/health\n", httpAddr)

	// Start WebSocket transport
	wsAddr := startWebSocketTransport(srv)
	fmt.Printf("🔌 WebSocket server started on ws://%s/ws\n", wsAddr)

	// Start monitoring endpoints
	monitoringAddr := startMonitoringServer()
	fmt.Printf("📊 Monitoring server started on %s\n", monitoringAddr)

	// Print usage examples
	printUsageExamples(httpAddr, wsAddr)

	// Wait for shutdown signal
	waitForShutdown()
}

// createEnhancedServer creates an MCP server with sample tools
func createEnhancedServer() *server.Server {
	srv := server.NewServer("enhanced-mcp-server", "1.0.0")

	// Add enhanced echo tool
	srv.AddTool(protocol.Tool{
		Name:        "enhanced_echo",
		Description: "Enhanced echo tool with versioning and error handling support",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo back",
				},
				"format": map[string]interface{}{
					"type":        "string",
					"description": "Output format (plain, json, or uppercase)",
					"enum":        []string{"plain", "json", "uppercase"},
					"default":     "plain",
				},
				"metadata": map[string]interface{}{
					"type":        "object",
					"description": "Additional metadata to include",
				},
			},
			"required": []string{"message"},
		},
	}, protocol.ToolHandlerFunc(handleEnhancedEcho))

	// Add calculator tool with versioning
	srv.AddTool(protocol.Tool{
		Name:        "calculator",
		Description: "Calculator with enhanced error handling and correlation tracking",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"operation": map[string]interface{}{
					"type": "string",
					"enum": []string{"add", "subtract", "multiply", "divide", "power"},
				},
				"a": map[string]interface{}{"type": "number"},
				"b": map[string]interface{}{"type": "number"},
			},
			"required": []string{"operation", "a", "b"},
		},
	}, protocol.ToolHandlerFunc(handleCalculator))

	// Add system info tool
	srv.AddTool(protocol.Tool{
		Name:        "system_info",
		Description: "Get system information with API version details",
		InputSchema: map[string]interface{}{
			"type":       "object",
			"properties": map[string]interface{}{},
		},
	}, protocol.ToolHandlerFunc(handleSystemInfo))

	return srv
}

// handleEnhancedEcho handles the enhanced echo tool with versioning support
func handleEnhancedEcho(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	message, ok := params["message"].(string)
	if !ok {
		return nil, fmt.Errorf("message parameter is required and must be a string")
	}

	format, _ := params["format"].(string)
	if format == "" {
		format = "plain"
	}

	metadata, _ := params["metadata"].(map[string]interface{})

	// Get API version from context
	version, hasVersion := versioning.GetVersionFromContext(ctx)
	correlationID := protocol.GetCorrelationID(ctx)

	var content string
	switch format {
	case "json":
		result := map[string]interface{}{
			"message":       message,
			"timestamp":     time.Now().Unix(),
			"correlationId": correlationID,
		}
		if hasVersion {
			result["apiVersion"] = version.String()
		}
		if metadata != nil {
			result["metadata"] = metadata
		}
		jsonBytes, _ := json.Marshal(result)
		content = string(jsonBytes)
	case "uppercase":
		content = fmt.Sprintf("MESSAGE: %s", message)
		if hasVersion {
			content += fmt.Sprintf(" (API: %s)", version.String())
		}
	default:
		content = message
		if hasVersion {
			content += fmt.Sprintf(" [v%s]", version.String())
		}
	}

	return protocol.NewToolCallResult(protocol.NewContent(content)), nil
}

// handleCalculator handles calculator operations with enhanced error handling
func handleCalculator(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	operation, ok := params["operation"].(string)
	if !ok {
		return nil, fmt.Errorf("operation parameter is required")
	}

	a, ok := params["a"].(float64)
	if !ok {
		return nil, fmt.Errorf("parameter 'a' must be a number")
	}

	b, ok := params["b"].(float64)
	if !ok {
		return nil, fmt.Errorf("parameter 'b' must be a number")
	}

	var result float64
	var err error

	switch operation {
	case "add":
		result = a + b
	case "subtract":
		result = a - b
	case "multiply":
		result = a * b
	case "divide":
		if b == 0 {
			return protocol.NewToolCallError("division by zero is not allowed"), nil
		}
		result = a / b
	case "power":
		// Simple power implementation
		if b < 0 {
			return nil, fmt.Errorf("negative exponents not supported")
		}
		result = 1
		for i := 0; i < int(b); i++ {
			result *= a
		}
	default:
		return nil, fmt.Errorf("unsupported operation: %s", operation)
	}

	// Include correlation ID and version info in response
	correlationID := protocol.GetCorrelationID(ctx)
	version, _ := versioning.GetVersionFromContext(ctx)

	response := fmt.Sprintf("%.2f", result)
	if correlationID != "" {
		response += fmt.Sprintf(" [ID: %s]", correlationID)
	}
	if version.Major > 0 {
		response += fmt.Sprintf(" [API: %s]", version.String())
	}

	return protocol.NewToolCallResult(protocol.NewContent(response)), err
}

// handleSystemInfo provides system information including API version details
func handleSystemInfo(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	info := map[string]interface{}{
		"server":        "enhanced-mcp-server",
		"version":       "1.0.0",
		"timestamp":     time.Now().Unix(),
		"correlationId": protocol.GetCorrelationID(ctx),
		"features": []string{
			"error_handling",
			"connection_pooling",
			"api_versioning",
			"websocket_support",
			"contract_testing",
		},
	}

	if version, hasVersion := versioning.GetVersionFromContext(ctx); hasVersion {
		info["apiVersion"] = version.String()
	}

	jsonBytes, _ := json.Marshal(info)
	return protocol.NewToolCallResult(protocol.NewContent(string(jsonBytes))), nil
}

// createErrorHandlingConfig creates error handling configuration
func createErrorHandlingConfig() *middleware.ErrorHandlingConfig {
	isDevelopment := os.Getenv("ENV") == "development"

	if isDevelopment {
		return &middleware.ErrorHandlingConfig{
			IncludeStackTrace: true,
			IncludeDebugInfo:  true,
			LogErrors:        true,
			ErrorTransformer: middleware.DevelopmentErrorTransformer,
		}
	}

	return &middleware.ErrorHandlingConfig{
		IncludeStackTrace: false,
		IncludeDebugInfo:  false,
		LogErrors:        true,
		ErrorTransformer: middleware.ProductionErrorTransformer,
	}
}

// createConnectionPoolConfig creates connection pool configuration
func createConnectionPoolConfig() *transport.ConnectionPoolConfig {
	return &transport.ConnectionPoolConfig{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		MaxConnsPerHost:       100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
}

// createVersionManager creates API version manager with multiple versions
func createVersionManager() *versioning.VersionManager {
	config := &versioning.VersioningConfig{
		Strategy:       versioning.VersioningStrategyHeader,
		DefaultVersion: versioning.APIVersion{Major: 1, Minor: 0, Patch: 0},
		SupportedVersions: []versioning.APIVersion{
			{Major: 1, Minor: 0, Patch: 0},
			{Major: 1, Minor: 1, Patch: 0},
			{Major: 1, Minor: 2, Patch: 0},
		},
		DeprecatedVersions: []versioning.APIVersion{
			{Major: 1, Minor: 0, Patch: 0}, // v1.0 is deprecated
		},
		HeaderName:            "API-Version",
		QueryParam:            "version",
		ContentTypeVendor:     "application/vnd.mcp",
		IncludeVersionHeaders: true,
		StrictVersioning:      false,
	}

	return versioning.NewVersionManager(config)
}

// startHTTPTransport starts the HTTP transport with all enhancements
func startHTTPTransport(srv *server.Server, errorConfig *middleware.ErrorHandlingConfig, 
	poolConfig *transport.ConnectionPoolConfig, versionManager *versioning.VersionManager) string {
	
	// Configure HTTP transport
	httpConfig := &transport.HTTPConfig{
		Address:        ":8080",
		ErrorHandling:  errorConfig,
		ConnectionPool: poolConfig,
		EnableCORS:     true,
		AllowedOrigins: []string{"*"}, // Configure appropriately for production
		CustomHeaders: map[string]string{
			"X-Server": "enhanced-mcp-server",
		},
	}

	// Create HTTP transport
	httpTransport := transport.NewHTTPTransport(httpConfig)

	// Create REST transport
	restConfig := &transport.RESTConfig{
		HTTPConfig: *httpConfig,
		APIPrefix:  "/api/v1",
		EnableDocs: true,
		RateLimit:  1000, // 1000 requests per minute
	}
	restTransport := transport.NewRESTTransport(restConfig)

	// Start transports
	ctx := context.Background()
	
	go func() {
		if err := httpTransport.Start(ctx, srv); err != nil {
			log.Printf("HTTP transport error: %v", err)
		}
	}()

	go func() {
		if err := restTransport.Start(ctx, srv); err != nil {
			log.Printf("REST transport error: %v", err)
		}
	}()

	// Wait for servers to start
	time.Sleep(100 * time.Millisecond)

	return httpTransport.Address()
}

// startWebSocketTransport starts the WebSocket transport
func startWebSocketTransport(srv *server.Server) string {
	wsConfig := &transport.WebSocketConfig{
		Address:           ":8081",
		Path:              "/ws",
		ReadBufferSize:    1024,
		WriteBufferSize:   1024,
		HandshakeTimeout:  10 * time.Second,
		ReadTimeout:       60 * time.Second,
		WriteTimeout:      10 * time.Second,
		PingInterval:      30 * time.Second,
		PongTimeout:       60 * time.Second,
		MaxMessageSize:    1024 * 1024, // 1MB
		EnableCompression: true,
		CheckOrigin: func(r *http.Request) bool {
			// Configure origin checking for security
			return true // Allow all origins for demo
		},
	}

	wsTransport := transport.NewWebSocketTransport(wsConfig)

	ctx := context.Background()
	go func() {
		if err := wsTransport.Start(ctx, srv); err != nil {
			log.Printf("WebSocket transport error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	return wsTransport.Address()
}

// startMonitoringServer starts a simple monitoring/stats server
func startMonitoringServer() string {
	mux := http.NewServeMux()

	// Health endpoint
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]interface{}{
			"status":    "healthy",
			"timestamp": time.Now().Unix(),
			"server":    "enhanced-mcp-server",
			"version":   "1.0.0",
		}); err != nil {
			log.Printf("Failed to encode health response: %v", err)
		}
	})

	// Metrics endpoint (placeholder for Prometheus integration)
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		fmt.Fprintf(w, "# Enhanced MCP Server Metrics\n")
		fmt.Fprintf(w, "mcp_server_uptime_seconds %d\n", int64(time.Since(time.Now()).Seconds()))
		fmt.Fprintf(w, "mcp_server_version{version=\"1.0.0\"} 1\n")
	})

	server := &http.Server{
		Addr:              ":8082",
		Handler:           mux,
		ReadHeaderTimeout: 30 * time.Second,
	}

	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("Monitoring server error: %v", err)
		}
	}()

	return ":8082"
}

// printUsageExamples prints usage examples for the enhanced server
func printUsageExamples(httpAddr, wsAddr string) {
	fmt.Printf("\n📖 Usage Examples:\n\n")

	fmt.Printf("🌐 HTTP Examples:\n")
	fmt.Printf("  # List tools with version header\n")
	fmt.Printf("  curl -H \"API-Version: v1.1\" http://%s/api/v1/tools\n\n", httpAddr)

	fmt.Printf("  # Call enhanced echo tool\n")
	fmt.Printf("  curl -X POST -H \"API-Version: v1.1\" -H \"Content-Type: application/json\" \\\n")
	fmt.Printf("    -d '{\"message\":\"Hello Enhanced MCP!\",\"format\":\"json\"}' \\\n")
	fmt.Printf("    http://%s/api/v1/tools/enhanced_echo\n\n", httpAddr)

	fmt.Printf("  # Call calculator with error handling\n")
	fmt.Printf("  curl -X POST -H \"API-Version: v1.1\" -H \"Content-Type: application/json\" \\\n")
	fmt.Printf("    -d '{\"operation\":\"add\",\"a\":5,\"b\":3}' \\\n")
	fmt.Printf("    http://%s/api/v1/tools/calculator\n\n", httpAddr)

	fmt.Printf("🔌 WebSocket Examples:\n")
	fmt.Printf("  # Connect and send JSON-RPC request\n")
	fmt.Printf("  wscat -c ws://%s/ws\n", wsAddr)
	fmt.Printf("  > {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}\n\n")

	fmt.Printf("  # Call tool via WebSocket\n")
	fmt.Printf("  > {\"jsonrpc\":\"2.0\",\"id\":2,\"method\":\"tools/call\",\"params\":{\"name\":\"system_info\",\"arguments\":{}}}\n\n")

	fmt.Printf("📊 Monitoring:\n")
	fmt.Printf("  # Health check\n")
	fmt.Printf("  curl http://localhost:8082/health\n\n")

	fmt.Printf("  # Metrics (Prometheus format)\n")
	fmt.Printf("  curl http://localhost:8082/metrics\n\n")

	fmt.Printf("🧪 Testing:\n")
	fmt.Printf("  # Run contract tests\n")
	fmt.Printf("  go test -v ./testing/...\n\n")
}

// waitForShutdown waits for shutdown signal
func waitForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("⏳ Server running... Press Ctrl+C to shutdown\n\n")

	<-quit
	fmt.Printf("\n🛑 Shutting down server...\n")
}