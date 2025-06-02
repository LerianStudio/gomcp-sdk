// Graceful Shutdown Example
//
// This example demonstrates the enhanced graceful shutdown capabilities
// implemented for all MCP transports in the GoMCP SDK.
//
// Features demonstrated:
// - Multi-phase graceful shutdown (stop new connections, grace period, drain connections, force stop)
// - Connection tracking and lifecycle management
// - Coordinated shutdown across multiple transport types
// - Proper cleanup of resources and active connections
// - WebSocket close message handling with timeouts
// - HTTP connection pooling integration with shutdown procedures
//
// To run this example:
//   go run examples/graceful-shutdown/main.go
//
// To test graceful shutdown:
//   1. Start the server
//   2. Connect multiple clients (HTTP/WebSocket)
//   3. Send Ctrl+C to trigger graceful shutdown
//   4. Observe the multi-phase shutdown process in the logs

package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/server"
	"github.com/LerianStudio/gomcp-sdk/shutdown"
	"github.com/LerianStudio/gomcp-sdk/transport"
)

func main() {
	fmt.Println("🚀 Starting Graceful Shutdown Demo Server...")
	
	// Create the MCP server
	srv := server.NewServer("graceful-shutdown-demo", "1.0.0")
	
	// Add a simple echo tool
	srv.AddTool(protocol.Tool{
		Name:        "echo",
		Description: "Echo back a message with connection info",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{
					"type":        "string",
					"description": "Message to echo back",
				},
			},
			"required": []string{"message"},
		},
	}, protocol.ToolHandlerFunc(handleEcho))

	// Create graceful shutdown manager
	gracefulShutdown := shutdown.NewGracefulShutdown(&shutdown.ShutdownConfig{
		Timeout:          30 * time.Second,
		GracePeriod:      10 * time.Second,
		DrainConnections: true,
		DrainTimeout:     15 * time.Second,
		OnShutdownStart: func() {
			fmt.Println("🛑 Graceful shutdown manager: Starting coordinated shutdown...")
		},
		OnGracePeriod: func() {
			fmt.Println("⏳ Graceful shutdown manager: Grace period for active connections...")
		},
		OnForceStop: func() {
			fmt.Println("⚡ Graceful shutdown manager: Force stopping remaining services...")
		},
		OnComplete: func() {
			fmt.Println("✅ Graceful shutdown manager: All services stopped successfully")
		},
	})

	// Start HTTP transport
	httpTransport := startHTTPTransport(srv)
	gracefulShutdown.Register(httpTransport)
	fmt.Printf("🌐 HTTP server listening on %s\n", httpTransport.Address())

	// Start WebSocket transport  
	wsTransport := startWebSocketTransport(srv)
	gracefulShutdown.Register(wsTransport)
	fmt.Printf("🔌 WebSocket server listening on ws://%s/ws\n", wsTransport.Address())

	// Print usage instructions
	printUsageInstructions(httpTransport.Address(), wsTransport.Address())

	// Wait for shutdown signal
	waitForShutdown(gracefulShutdown)
}

func handleEcho(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	message, ok := params["message"].(string)
	if !ok {
		return nil, fmt.Errorf("message parameter is required and must be a string")
	}

	// Include timestamp and echo the message
	response := fmt.Sprintf("[%s] Echo: %s", 
		time.Now().Format("15:04:05"), message)

	return protocol.NewToolCallResult(protocol.NewContent(response)), nil
}

func startHTTPTransport(srv *server.Server) *transport.HTTPTransport {
	httpConfig := &transport.HTTPConfig{
		Address:      ":8080",
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
		MaxBodySize:  1024 * 1024, // 1MB
		CustomHeaders: map[string]string{
			"X-Server": "graceful-shutdown-demo",
		},
	}

	httpTransport := transport.NewHTTPTransport(httpConfig)

	ctx := context.Background()
	go func() {
		if err := httpTransport.Start(ctx, srv); err != nil {
			log.Printf("HTTP transport error: %v", err)
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	return httpTransport
}

func startWebSocketTransport(srv *server.Server) *transport.WebSocketTransport {
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

	return wsTransport
}

func printUsageInstructions(httpAddr, wsAddr string) {
	fmt.Printf("\n📖 Usage Instructions:\n\n")
	
	fmt.Printf("🌐 HTTP Examples:\n")
	fmt.Printf("  # Test echo tool\n")
	fmt.Printf("  curl -X POST -H \"Content-Type: application/json\" \\\\\n")
	fmt.Printf("    -d '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"echo\",\"arguments\":{\"message\":\"Hello World!\"}}}' \\\\\n")
	fmt.Printf("    http://%s/\n\n", httpAddr)

	fmt.Printf("🔌 WebSocket Examples:\n")
	fmt.Printf("  # Connect with wscat and test\n")
	fmt.Printf("  wscat -c ws://%s/ws\n", wsAddr)
	fmt.Printf("  > {\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/call\",\"params\":{\"name\":\"echo\",\"arguments\":{\"message\":\"WebSocket Hello!\"}}}\n\n")

	fmt.Printf("🔄 Testing Graceful Shutdown:\n")
	fmt.Printf("  1. Connect multiple clients using the examples above\n")
	fmt.Printf("  2. Keep connections active (send periodic messages)\n")
	fmt.Printf("  3. Press Ctrl+C to trigger graceful shutdown\n")
	fmt.Printf("  4. Observe the multi-phase shutdown process:\n")
	fmt.Printf("     • Phase 1: Stop accepting new connections\n")
	fmt.Printf("     • Phase 2: Grace period for active requests (10s)\n")
	fmt.Printf("     • Phase 3: Send WebSocket close messages\n")
	fmt.Printf("     • Phase 4: Drain existing connections (15s)\n")
	fmt.Printf("     • Phase 5: Shutdown servers\n")
	fmt.Printf("     • Phase 6: Force stop if needed\n\n")

	fmt.Printf("📊 Connection Monitoring:\n")
	fmt.Printf("  During shutdown, you'll see real-time connection counts\n")
	fmt.Printf("  and progress through each shutdown phase.\n\n")
}

func waitForShutdown(gracefulShutdown *shutdown.GracefulShutdown) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	fmt.Printf("⏳ Demo server running... Press Ctrl+C to test graceful shutdown\n\n")

	<-quit
	fmt.Printf("\n🛑 Shutdown signal received, initiating graceful shutdown...\n\n")

	// Create shutdown context with timeout
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()

	// Perform graceful shutdown
	if err := gracefulShutdown.Shutdown(ctx); err != nil {
		fmt.Printf("❌ Graceful shutdown failed: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("\n✅ Graceful shutdown demo completed successfully!\n")
}