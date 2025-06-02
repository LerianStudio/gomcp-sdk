// Package transport implements MCP transport layers
package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/LerianStudio/gomcp-sdk/correlation"
	"github.com/LerianStudio/gomcp-sdk/middleware"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/shutdown"
	"github.com/gorilla/websocket"
)

// WebSocketConfig contains configuration for WebSocket transport
type WebSocketConfig struct {
	// Address to listen on
	Address string

	// TLS configuration (optional)
	TLSConfig *tls.Config

	// WebSocket upgrade configuration
	ReadBufferSize  int
	WriteBufferSize int

	// Timeouts
	HandshakeTimeout time.Duration
	ReadTimeout      time.Duration
	WriteTimeout     time.Duration
	PingInterval     time.Duration
	PongTimeout      time.Duration

	// Maximum message size (default: 10MB)
	MaxMessageSize int64

	// Enable compression
	EnableCompression bool

	// Check origin function
	CheckOrigin func(r *http.Request) bool

	// Path to handle WebSocket connections (default: "/ws")
	Path string

	// Tracing configuration
	TracingConfig *middleware.TracingConfig

	// Enable correlation ID propagation
	EnableCorrelation bool
}

// WebSocketTransport implements WebSocket transport for MCP
type WebSocketTransport struct {
	config            *WebSocketConfig
	server            *http.Server
	listener          net.Listener
	upgrader          *websocket.Upgrader
	handler           RequestHandler
	mu                sync.RWMutex
	running           bool
	certFile          string
	keyFile           string
	connectionTracker *shutdown.ConnectionTracker
	shutdownConfig    *shutdown.ShutdownConfig
	activeRequests    int64

	// Active connections
	connections       map[*websocket.Conn]bool
	connMu            sync.RWMutex
	tracingMiddleware *middleware.TracingMiddleware
}

// NewWebSocketTransport creates a new WebSocket transport
func NewWebSocketTransport(config *WebSocketConfig) *WebSocketTransport {
	// Set defaults
	if config.ReadBufferSize == 0 {
		config.ReadBufferSize = 4096
	}
	if config.WriteBufferSize == 0 {
		config.WriteBufferSize = 4096
	}
	if config.MaxMessageSize == 0 {
		config.MaxMessageSize = 10 * 1024 * 1024 // 10MB
	}
	if config.HandshakeTimeout == 0 {
		config.HandshakeTimeout = 10 * time.Second
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 60 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 10 * time.Second
	}
	if config.PingInterval == 0 {
		config.PingInterval = 30 * time.Second
	}
	if config.PongTimeout == 0 {
		config.PongTimeout = 60 * time.Second
	}
	if config.Path == "" {
		config.Path = "/ws"
	}
	if config.CheckOrigin == nil {
		config.CheckOrigin = func(r *http.Request) bool { return true }
	}

	upgrader := &websocket.Upgrader{
		ReadBufferSize:    config.ReadBufferSize,
		WriteBufferSize:   config.WriteBufferSize,
		HandshakeTimeout:  config.HandshakeTimeout,
		EnableCompression: config.EnableCompression,
		CheckOrigin:       config.CheckOrigin,
	}

	transport := &WebSocketTransport{
		config:            config,
		upgrader:          upgrader,
		connections:       make(map[*websocket.Conn]bool),
		connectionTracker: shutdown.NewConnectionTracker(),
		shutdownConfig:    shutdown.DefaultShutdownConfig(),
	}

	// Initialize tracing middleware if enabled
	if config.TracingConfig != nil {
		transport.tracingMiddleware = middleware.NewTracingMiddleware(*config.TracingConfig)
	}

	return transport
}

// NewSecureWebSocketTransport creates a new secure WebSocket transport
func NewSecureWebSocketTransport(config *WebSocketConfig, certFile, keyFile string) *WebSocketTransport {
	transport := NewWebSocketTransport(config)
	transport.certFile = certFile
	transport.keyFile = keyFile
	return transport
}

// Start starts the WebSocket transport
func (t *WebSocketTransport) Start(ctx context.Context, handler RequestHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("transport already running")
	}

	t.handler = handler

	mux := http.NewServeMux()

	// Wrap WebSocket handler with middleware
	var wsHandler http.Handler = http.HandlerFunc(t.handleWebSocket)

	// Correlation middleware (innermost)
	if t.config.EnableCorrelation {
		wsHandler = correlation.CorrelationMiddleware()(wsHandler)
	}

	// Tracing middleware
	if t.tracingMiddleware != nil {
		wsHandler = t.tracingMiddleware.WebSocketMiddleware()(wsHandler)
	}

	mux.Handle(t.config.Path, wsHandler)

	// Create listener first to get the actual port
	listener, err := net.Listen("tcp", t.config.Address)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	t.listener = listener

	t.server = &http.Server{
		Handler:           mux,
		TLSConfig:         t.config.TLSConfig,
		ReadHeaderTimeout: 30 * time.Second,
	}

	t.running = true

	// Start server in a goroutine
	go func() {
		var err error
		if t.certFile != "" && t.keyFile != "" {
			err = t.server.ServeTLS(t.listener, t.certFile, t.keyFile)
		} else {
			err = t.server.Serve(t.listener)
		}
		if err != nil && err != http.ErrServerClosed {
			t.mu.Lock()
			t.running = false
			t.mu.Unlock()
		}
	}()

	// Wait for context cancellation
	go func() {
		<-ctx.Done()
		if err := t.Stop(); err != nil {
			// Log error but don't fail since context is cancelled
			fmt.Printf("Error stopping WebSocket transport: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the WebSocket transport gracefully
func (t *WebSocketTransport) Stop() error {
	return t.GracefulShutdown(context.Background())
}

// ForceStop forces immediate shutdown of the WebSocket transport
func (t *WebSocketTransport) ForceStop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false

	// Close all active connections immediately
	t.connMu.Lock()
	for conn := range t.connections {
		_ = conn.Close()
	}
	t.connections = make(map[*websocket.Conn]bool)
	t.connMu.Unlock()

	// Close listener immediately
	if t.listener != nil {
		_ = t.listener.Close()
	}

	// Close server without graceful shutdown
	if t.server != nil {
		return t.server.Close()
	}

	return nil
}

// IsRunning returns whether the transport is running
func (t *WebSocketTransport) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// ConnectionCount returns the number of active connections
func (t *WebSocketTransport) ConnectionCount() int {
	t.connMu.RLock()
	connCount := len(t.connections)
	t.connMu.RUnlock()

	return connCount
}

// GracefulShutdown performs graceful shutdown with connection draining
func (t *WebSocketTransport) GracefulShutdown(ctx context.Context) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	t.mu.Unlock()

	fmt.Println("🛑 WebSocket Transport: Initiating graceful shutdown...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, t.shutdownConfig.Timeout)
	defer cancel()

	// Phase 1: Stop accepting new connections
	fmt.Println("🚫 WebSocket Transport: Stopping new connections...")
	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			fmt.Printf("Error closing WebSocket listener: %v\n", err)
		}
	}

	// Phase 2: Wait for active requests to complete
	if t.shutdownConfig.GracePeriod > 0 {
		fmt.Printf("⏳ WebSocket Transport: Grace period (%v) for active requests...\n", t.shutdownConfig.GracePeriod)

		gracePeriodCtx, graceCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.GracePeriod)
		t.waitForActiveRequests(gracePeriodCtx)
		graceCancel()
	}

	// Phase 3: Send close messages to WebSocket connections
	fmt.Println("📤 WebSocket Transport: Sending close messages to connections...")
	t.sendCloseMessages()

	// Phase 4: Drain connections with timeout
	if t.shutdownConfig.DrainConnections {
		fmt.Printf("🔄 WebSocket Transport: Draining connections (timeout: %v)...\n", t.shutdownConfig.DrainTimeout)

		drainCtx, drainCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.DrainTimeout)
		t.drainConnections(drainCtx)
		drainCancel()
	}

	// Phase 5: Shutdown server
	fmt.Println("🔌 WebSocket Transport: Shutting down server...")
	if t.server != nil {
		err := t.server.Shutdown(shutdownCtx)
		if err != nil {
			fmt.Printf("❌ WebSocket Transport: Server shutdown error: %v\n", err)
			return err
		}
	}

	fmt.Println("✅ WebSocket Transport: Graceful shutdown completed")
	return nil
}

// waitForActiveRequests waits for active requests to complete
func (t *WebSocketTransport) waitForActiveRequests(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs > 0 {
				fmt.Printf("⏰ WebSocket Transport: Grace period timeout with %d active requests\n", activeReqs)
			}
			return
		case <-ticker.C:
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs == 0 {
				fmt.Println("✅ WebSocket Transport: All active requests completed")
				return
			}
			fmt.Printf("⏳ WebSocket Transport: Waiting for %d active requests...\n", activeReqs)
		}
	}
}

// sendCloseMessages sends close messages to all WebSocket connections
func (t *WebSocketTransport) sendCloseMessages() {
	t.connMu.RLock()
	connections := make([]*websocket.Conn, 0, len(t.connections))
	for conn := range t.connections {
		connections = append(connections, conn)
	}
	t.connMu.RUnlock()

	// Send close messages concurrently
	var wg sync.WaitGroup
	for _, conn := range connections {
		wg.Add(1)
		go func(c *websocket.Conn) {
			defer wg.Done()

			// Send close message with 10 second timeout
			deadline := time.Now().Add(10 * time.Second)
			if err := c.SetWriteDeadline(deadline); err != nil {
				// Connection might be broken, continue with close attempt
				return
			}

			closeMessage := websocket.FormatCloseMessage(websocket.CloseGoingAway, "Server shutting down")
			if err := c.WriteMessage(websocket.CloseMessage, closeMessage); err != nil {
				// Ignore errors, connection might already be closed
			}
		}(conn)
	}

	// Wait for all close messages to be sent with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("📤 WebSocket Transport: Close messages sent to all connections")
	case <-time.After(5 * time.Second):
		fmt.Println("⏰ WebSocket Transport: Timeout sending close messages")
	}
}

// drainConnections drains existing connections
func (t *WebSocketTransport) drainConnections(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.connMu.RLock()
			connCount := len(t.connections)
			t.connMu.RUnlock()

			if connCount > 0 {
				fmt.Printf("⏰ WebSocket Transport: Connection draining timeout with %d connections\n", connCount)
				// Force close remaining connections
				t.connMu.Lock()
				for conn := range t.connections {
					_ = conn.Close()
				}
				t.connections = make(map[*websocket.Conn]bool)
				t.connMu.Unlock()
			}
			return
		case <-ticker.C:
			t.connMu.RLock()
			connCount := len(t.connections)
			t.connMu.RUnlock()

			if connCount == 0 {
				fmt.Println("✅ WebSocket Transport: All connections drained")
				return
			}
			fmt.Printf("🔄 WebSocket Transport: Draining %d remaining connections...\n", connCount)
		}
	}
}

// handleWebSocket handles WebSocket upgrade and connection
func (t *WebSocketTransport) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := t.upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}

	// Track connection
	connID := t.connectionTracker.AddConnection("websocket", r.RemoteAddr)

	// Register connection
	t.connMu.Lock()
	t.connections[conn] = true
	t.connMu.Unlock()

	// Handle connection with context
	go t.handleConnection(conn, connID, r.Context())
}

// handleConnection handles a WebSocket connection
func (t *WebSocketTransport) handleConnection(conn *websocket.Conn, connID string, baseCtx context.Context) {
	// Track active request
	atomic.AddInt64(&t.activeRequests, 1)
	defer atomic.AddInt64(&t.activeRequests, -1)

	defer func() {
		// Remove connection tracking
		t.connectionTracker.RemoveConnection(connID)

		// Unregister connection
		t.connMu.Lock()
		delete(t.connections, conn)
		t.connMu.Unlock()
		_ = conn.Close()
	}()

	// Set connection parameters
	conn.SetReadLimit(t.config.MaxMessageSize)
	if err := conn.SetReadDeadline(time.Now().Add(t.config.PongTimeout)); err != nil {
		return
	}
	conn.SetPongHandler(func(string) error {
		_ = conn.SetReadDeadline(time.Now().Add(t.config.PongTimeout))
		return nil
	})

	// Start ping ticker
	ticker := time.NewTicker(t.config.PingInterval)
	defer ticker.Stop()

	// Message handling
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			messageType, message, err := conn.ReadMessage()
			if err != nil {
				return
			}

			if messageType != websocket.TextMessage {
				continue
			}

			// Parse message flexibly to handle both requests and error responses
			parsed, parseErr := protocol.ParseJSONRPCMessage(message)
			if parseErr != nil {
				if jsonRPCErr, ok := parseErr.(*protocol.JSONRPCError); ok {
					if err := t.sendError(conn, nil, jsonRPCErr.Code, jsonRPCErr.Message, jsonRPCErr.Data); err != nil {
						return
					}
				}
				continue
			}

			// Handle requests normally
			if parsed.Request != nil {
				// Create context for this request, inheriting correlation data
				ctx := correlation.CreateChildContext(baseCtx)

				// Add tracing for WebSocket JSON-RPC requests
				if t.tracingMiddleware != nil {
					var span trace.Span
					ctx, span = t.tracingMiddleware.TraceJSONRPCRequest(ctx, parsed.Request)
					// Also trace the WebSocket message
					ctx, msgSpan := t.tracingMiddleware.TraceWebSocketMessage(ctx, "request", connID)

					// Handle the request
					resp := t.handler.HandleRequest(ctx, parsed.Request)

					// Set span status based on response
					if resp.Error != nil {
						span.SetStatus(codes.Error, resp.Error.Message)
						msgSpan.SetStatus(codes.Error, resp.Error.Message)
					} else {
						span.SetStatus(codes.Ok, "")
						msgSpan.SetStatus(codes.Ok, "")
					}

					span.End()
					msgSpan.End()

					// Send response
					if err := t.sendResponse(conn, resp); err != nil {
						return
					}
				} else {
					// Handle without tracing
					resp := t.handler.HandleRequest(ctx, parsed.Request)
					if err := t.sendResponse(conn, resp); err != nil {
						return
					}
				}
			} else if parsed.Response != nil && parsed.IsError {
				// Claude Desktop sent an error response - continue gracefully
				continue
			}
		}
	}()

	// Ping loop
	for {
		select {
		case <-ticker.C:
			if err := conn.SetWriteDeadline(time.Now().Add(t.config.WriteTimeout)); err != nil {
				return
			}
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-done:
			return
		}
	}
}

// sendResponse sends a JSON-RPC response
func (t *WebSocketTransport) sendResponse(conn *websocket.Conn, resp *protocol.JSONRPCResponse) error {
	if err := conn.SetWriteDeadline(time.Now().Add(t.config.WriteTimeout)); err != nil {
		return err
	}
	return conn.WriteJSON(resp)
}

// sendError sends a JSON-RPC error response
func (t *WebSocketTransport) sendError(conn *websocket.Conn, id interface{}, code int, message string, data interface{}) error {
	resp := &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   protocol.NewJSONRPCError(code, message, data),
	}
	return t.sendResponse(conn, resp)
}

// Broadcast sends a message to all connected clients
func (t *WebSocketTransport) Broadcast(message interface{}) error {
	t.connMu.RLock()
	defer t.connMu.RUnlock()

	data, err := json.Marshal(message)
	if err != nil {
		return err
	}

	for conn := range t.connections {
		if err := conn.SetWriteDeadline(time.Now().Add(t.config.WriteTimeout)); err != nil {
			// Connection might be dead, will be cleaned up by handleConnection
			continue
		}
		if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
			// Connection might be dead, will be cleaned up by handleConnection
			continue
		}
	}

	return nil
}

// Address returns the actual listening address
func (t *WebSocketTransport) Address() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.config.Address
}
