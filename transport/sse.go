// Package transport implements MCP transport layers
package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/LerianStudio/gomcp-sdk/correlation"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/shutdown"
)

// SSEConfig contains configuration for Server-Sent Events transport
type SSEConfig struct {
	// Base HTTP configuration
	HTTPConfig

	// SSE-specific settings
	HeartbeatInterval time.Duration

	// Maximum number of clients
	MaxClients int

	// Event buffer size per client
	EventBufferSize int

	// Retry delay for client reconnection (sent in SSE)
	RetryDelay time.Duration

	// Path to handle SSE connections (default: "/events")
	EventPath string
}

// SSETransport implements Server-Sent Events transport for MCP
type SSETransport struct {
	*HTTPTransport
	sseConfig *SSEConfig

	// Client management
	clients  map[string]*sseClient
	clientMu sync.RWMutex

	// Event broadcasting
	broadcast chan *sseEvent

	// Graceful shutdown support
	connectionTracker *shutdown.ConnectionTracker
	shutdownConfig    *shutdown.ShutdownConfig
	activeRequests    int64
}

// sseClient represents a connected SSE client
type sseClient struct {
	id          string
	events      chan *sseEvent
	done        chan struct{}
	lastEventID string
	writer      http.ResponseWriter
	flusher     http.Flusher
}

// sseEvent represents an SSE event
type sseEvent struct {
	ID    string
	Type  string
	Data  interface{}
	Retry int
}

// NewSSETransport creates a new SSE transport
func NewSSETransport(config *SSEConfig) *SSETransport {
	// Set defaults
	if config.HeartbeatInterval == 0 {
		config.HeartbeatInterval = 30 * time.Second
	}
	if config.MaxClients == 0 {
		config.MaxClients = 1000
	}
	if config.EventBufferSize == 0 {
		config.EventBufferSize = 100
	}
	if config.RetryDelay == 0 {
		config.RetryDelay = 5 * time.Second
	}
	if config.EventPath == "" {
		config.EventPath = "/events"
	}

	// Create base HTTP transport
	httpTransport := NewHTTPTransport(&config.HTTPConfig)

	return &SSETransport{
		HTTPTransport:     httpTransport,
		sseConfig:         config,
		clients:           make(map[string]*sseClient),
		broadcast:         make(chan *sseEvent, 1000),
		connectionTracker: shutdown.NewConnectionTracker(),
		shutdownConfig:    shutdown.DefaultShutdownConfig(),
	}
}

// Start starts the SSE transport
func (t *SSETransport) Start(ctx context.Context, handler RequestHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return errors.New("transport already running")
	}

	t.handler = handler

	// Start broadcast handler
	go t.broadcastHandler()

	mux := http.NewServeMux()

	// SSE endpoint
	mux.HandleFunc(t.sseConfig.EventPath, t.handleSSE)

	// Regular HTTP endpoint for sending commands
	mux.HandleFunc(t.config.Path, t.handleCommand)

	t.server = &http.Server{
		Addr:         t.config.Address,
		Handler:      t.wrapWithMiddleware(mux),
		ReadTimeout:  t.config.ReadTimeout,
		WriteTimeout: 0, // Disable write timeout for SSE
		IdleTimeout:  t.config.IdleTimeout,
		TLSConfig:    t.config.TLSConfig,
	}

	// Create listener
	var err error
	if t.config.TLSConfig != nil {
		t.listener, err = tls.Listen("tcp", t.config.Address, t.config.TLSConfig)
	} else {
		t.listener, err = net.Listen("tcp", t.config.Address)
	}
	if err != nil {
		t.running = false
		return fmt.Errorf("failed to create listener: %w", err)
	}

	t.running = true

	// Start server
	go func() {
		var err error
		if t.certFile != "" && t.keyFile != "" && t.config.TLSConfig != nil {
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
			fmt.Printf("Error stopping SSE transport: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the SSE transport gracefully
func (t *SSETransport) Stop() error {
	return t.GracefulShutdown(context.Background())
}

// ForceStop forces immediate shutdown of the SSE transport
func (t *SSETransport) ForceStop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false

	// Close broadcast channel immediately
	close(t.broadcast)

	// Disconnect all clients immediately
	t.clientMu.Lock()
	for _, client := range t.clients {
		close(client.done)
	}
	t.clients = make(map[string]*sseClient)
	t.clientMu.Unlock()

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
func (t *SSETransport) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// ConnectionCount returns the number of active connections
func (t *SSETransport) ConnectionCount() int {
	t.clientMu.RLock()
	clientCount := len(t.clients)
	t.clientMu.RUnlock()

	return clientCount + t.connectionTracker.Count() + int(atomic.LoadInt64(&t.activeRequests))
}

// GracefulShutdown performs graceful shutdown with connection draining
func (t *SSETransport) GracefulShutdown(ctx context.Context) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	t.mu.Unlock()

	fmt.Println("🛑 SSE Transport: Initiating graceful shutdown...")

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, t.shutdownConfig.Timeout)
	defer cancel()

	// Phase 1: Stop accepting new connections
	fmt.Println("🚫 SSE Transport: Stopping new connections...")
	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			fmt.Printf("Error closing SSE listener: %v\n", err)
		}
	}

	// Phase 2: Wait for active requests to complete
	if t.shutdownConfig.GracePeriod > 0 {
		fmt.Printf("⏳ SSE Transport: Grace period (%v) for active requests...\n", t.shutdownConfig.GracePeriod)

		gracePeriodCtx, graceCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.GracePeriod)
		t.waitForActiveRequests(gracePeriodCtx)
		graceCancel()
	}

	// Phase 3: Send disconnect events to SSE clients
	fmt.Println("📤 SSE Transport: Sending disconnect events to clients...")
	t.sendDisconnectEvents()

	// Phase 4: Drain connections
	if t.shutdownConfig.DrainConnections {
		fmt.Printf("🔄 SSE Transport: Draining connections (timeout: %v)...\n", t.shutdownConfig.DrainTimeout)

		drainCtx, drainCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.DrainTimeout)
		t.drainConnections(drainCtx)
		drainCancel()
	}

	// Phase 5: Close broadcast channel and remaining clients
	fmt.Println("📤 SSE Transport: Closing broadcast and client channels...")
	close(t.broadcast)

	t.clientMu.Lock()
	for _, client := range t.clients {
		close(client.done)
	}
	t.clients = make(map[string]*sseClient)
	t.clientMu.Unlock()

	// Phase 6: Shutdown server
	fmt.Println("🔌 SSE Transport: Shutting down server...")
	if t.server != nil {
		err := t.server.Shutdown(shutdownCtx)
		if err != nil {
			fmt.Printf("❌ SSE Transport: Server shutdown error: %v\n", err)
			return err
		}
	}

	fmt.Println("✅ SSE Transport: Graceful shutdown completed")
	return nil
}

// waitForActiveRequests waits for active requests to complete
func (t *SSETransport) waitForActiveRequests(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs > 0 {
				fmt.Printf("⏰ SSE Transport: Grace period timeout with %d active requests\n", activeReqs)
			}
			return
		case <-ticker.C:
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs == 0 {
				fmt.Println("✅ SSE Transport: All active requests completed")
				return
			}
			fmt.Printf("⏳ SSE Transport: Waiting for %d active requests...\n", activeReqs)
		}
	}
}

// sendDisconnectEvents sends disconnect events to all SSE clients
func (t *SSETransport) sendDisconnectEvents() {
	t.clientMu.RLock()
	clients := make([]*sseClient, 0, len(t.clients))
	for _, client := range t.clients {
		clients = append(clients, client)
	}
	t.clientMu.RUnlock()

	// Send disconnect events concurrently
	var wg sync.WaitGroup
	for _, client := range clients {
		wg.Add(1)
		go func(c *sseClient) {
			defer wg.Done()

			// Send disconnect event with 5 second timeout
			disconnectEvent := &sseEvent{
				Type: "disconnect",
				Data: map[string]interface{}{
					"reason": "Server shutting down",
					"retry":  int(t.sseConfig.RetryDelay / time.Millisecond),
				},
			}

			// Try to send disconnect event
			select {
			case c.events <- disconnectEvent:
				// Event queued, give it a moment to be sent
				time.Sleep(100 * time.Millisecond)
			case <-time.After(1 * time.Second):
				// Timeout, continue
			}
		}(client)
	}

	// Wait for all disconnect events to be sent with timeout
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		fmt.Println("📤 SSE Transport: Disconnect events sent to all clients")
	case <-time.After(3 * time.Second):
		fmt.Println("⏰ SSE Transport: Timeout sending disconnect events")
	}
}

// drainConnections drains existing connections
func (t *SSETransport) drainConnections(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			t.clientMu.RLock()
			clientCount := len(t.clients)
			t.clientMu.RUnlock()

			if clientCount > 0 {
				fmt.Printf("⏰ SSE Transport: Connection draining timeout with %d clients\n", clientCount)
				// Force close remaining clients
				t.clientMu.Lock()
				for _, client := range t.clients {
					close(client.done)
				}
				t.clients = make(map[string]*sseClient)
				t.clientMu.Unlock()
			}
			return
		case <-ticker.C:
			t.clientMu.RLock()
			clientCount := len(t.clients)
			t.clientMu.RUnlock()

			if clientCount == 0 {
				fmt.Println("✅ SSE Transport: All connections drained")
				return
			}
			fmt.Printf("🔄 SSE Transport: Draining %d remaining connections...\n", clientCount)
		}
	}
}

// handleSSE handles SSE connections
func (t *SSETransport) handleSSE(w http.ResponseWriter, r *http.Request) {
	// Track active request
	atomic.AddInt64(&t.activeRequests, 1)
	defer atomic.AddInt64(&t.activeRequests, -1)

	// Check if client supports SSE
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "SSE not supported", http.StatusInternalServerError)
		return
	}

	// Create client
	clientID := generateID()
	client := &sseClient{
		id:          clientID,
		events:      make(chan *sseEvent, t.sseConfig.EventBufferSize),
		done:        make(chan struct{}),
		lastEventID: r.Header.Get("Last-Event-ID"),
		writer:      w,
		flusher:     flusher,
	}

	// Check max clients and register atomically
	t.clientMu.Lock()
	if t.sseConfig.MaxClients > 0 && len(t.clients) >= t.sseConfig.MaxClients {
		t.clientMu.Unlock()
		http.Error(w, "Maximum clients reached", http.StatusServiceUnavailable)
		return
	}
	t.clients[clientID] = client
	t.clientMu.Unlock()

	// Set SSE headers after client registration succeeds
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable Nginx buffering

	// Send initial connection event
	t.sendToClient(client, &sseEvent{
		Type: "connected",
		Data: map[string]interface{}{
			"clientId": clientID,
			"retry":    int(t.sseConfig.RetryDelay / time.Millisecond),
		},
	})

	// Send server capabilities
	initReq := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      "sse-init-" + clientID,
		Method:  "initialize",
		Params: map[string]interface{}{
			"protocolVersion": protocol.Version,
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "SSE Client",
				"version": "1.0.0",
			},
		},
	}

	resp := t.handler.HandleRequest(r.Context(), initReq)
	if resp.Result != nil {
		t.sendToClient(client, &sseEvent{
			Type: "capabilities",
			Data: resp.Result,
		})
	}

	// Handle client
	t.handleClient(client, r.Context())

	// Cleanup
	t.clientMu.Lock()
	delete(t.clients, clientID)
	t.clientMu.Unlock()
	close(client.events)
}

// handleClient handles a connected SSE client
func (t *SSETransport) handleClient(client *sseClient, ctx context.Context) {
	ticker := time.NewTicker(t.sseConfig.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-client.done:
			return
		case event := <-client.events:
			if err := t.writeSSEEvent(client, event); err != nil {
				return
			}
		case <-ticker.C:
			// Send heartbeat
			if err := t.writeSSEComment(client, "heartbeat"); err != nil {
				return
			}
		}
	}
}

// handleCommand handles command requests via regular HTTP POST
func (t *SSETransport) handleCommand(w http.ResponseWriter, r *http.Request) {
	// Track active request
	atomic.AddInt64(&t.activeRequests, 1)
	defer atomic.AddInt64(&t.activeRequests, -1)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Get client ID from header or query
	clientID := r.Header.Get("X-Client-ID")
	if clientID == "" {
		clientID = r.URL.Query().Get("client_id")
	}

	// Parse request
	var req protocol.JSONRPCRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		t.writeError(w, protocol.ParseError, "Invalid JSON", nil)
		return
	}

	// Handle request
	resp := t.handler.HandleRequest(r.Context(), &req)

	// Send response via SSE if client ID provided
	if clientID != "" {
		t.clientMu.RLock()
		client, exists := t.clients[clientID]
		t.clientMu.RUnlock()

		if exists {
			event := &sseEvent{
				ID:   fmt.Sprintf("%v", req.ID),
				Type: "response",
				Data: resp,
			}

			select {
			case client.events <- event:
				// Response will be sent via SSE
				w.WriteHeader(http.StatusAccepted)
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"status": "accepted",
					"id":     req.ID,
				})
				return
			default:
				// Client buffer full
			}
		}
	}

	// Send response via HTTP
	t.writeResponse(w, resp)
}

// broadcastHandler handles event broadcasting
func (t *SSETransport) broadcastHandler() {
	for event := range t.broadcast {
		t.clientMu.RLock()
		clients := make([]*sseClient, 0, len(t.clients))
		for _, client := range t.clients {
			clients = append(clients, client)
		}
		t.clientMu.RUnlock()

		// Send to all clients
		for _, client := range clients {
			select {
			case client.events <- event:
				// Event queued
			default:
				// Client buffer full, skip
			}
		}
	}
}

// BroadcastEvent broadcasts an event to all connected clients
func (t *SSETransport) BroadcastEvent(eventType string, data interface{}) {
	event := &sseEvent{
		ID:   generateID(),
		Type: eventType,
		Data: data,
	}

	select {
	case t.broadcast <- event:
		// Event queued for broadcast
	default:
		// Broadcast buffer full
	}
}

// SendToClient sends an event to a specific client
func (t *SSETransport) SendToClient(clientID string, eventType string, data interface{}) error {
	t.clientMu.RLock()
	client, exists := t.clients[clientID]
	t.clientMu.RUnlock()

	if !exists {
		return fmt.Errorf("client not found: %s", clientID)
	}

	event := &sseEvent{
		ID:   generateID(),
		Type: eventType,
		Data: data,
	}

	select {
	case client.events <- event:
		return nil
	default:
		return errors.New("client buffer full")
	}
}

// sendToClient sends an event to a client (internal)
func (t *SSETransport) sendToClient(client *sseClient, event *sseEvent) {
	select {
	case client.events <- event:
		// Event queued
	default:
		// Buffer full, drop event
	}
}

// writeSSEEvent writes an SSE event
func (t *SSETransport) writeSSEEvent(client *sseClient, event *sseEvent) error {
	// Write event ID if provided
	if event.ID != "" {
		if _, err := fmt.Fprintf(client.writer, "id: %s\n", event.ID); err != nil {
			return err
		}
		client.lastEventID = event.ID
	}

	// Write event type
	if event.Type != "" {
		if _, err := fmt.Fprintf(client.writer, "event: %s\n", event.Type); err != nil {
			return err
		}
	}

	// Write retry if provided
	if event.Retry > 0 {
		if _, err := fmt.Fprintf(client.writer, "retry: %d\n", event.Retry); err != nil {
			return err
		}
	}

	// Write data
	data, err := json.Marshal(event.Data)
	if err != nil {
		return err
	}

	if _, err := fmt.Fprintf(client.writer, "data: %s\n\n", data); err != nil {
		return err
	}

	// Flush
	client.flusher.Flush()
	return nil
}

// writeSSEComment writes an SSE comment (for keepalive)
func (t *SSETransport) writeSSEComment(client *sseClient, comment string) error {
	if _, err := fmt.Fprintf(client.writer, ": %s\n\n", comment); err != nil {
		return err
	}
	client.flusher.Flush()
	return nil
}

// ClientCount returns the number of connected SSE clients
func (t *SSETransport) ClientCount() int {
	t.clientMu.RLock()
	defer t.clientMu.RUnlock()
	return len(t.clients)
}

// GetClients returns a list of connected client IDs
func (t *SSETransport) GetClients() []string {
	t.clientMu.RLock()
	defer t.clientMu.RUnlock()

	clients := make([]string, 0, len(t.clients))
	for id := range t.clients {
		clients = append(clients, id)
	}
	return clients
}

// wrapWithMiddleware wraps the handler with SSE-compatible middleware
// This overrides HTTPTransport's method to ensure middleware doesn't break SSE
func (t *SSETransport) wrapWithMiddleware(handler http.Handler) http.Handler {
	// Only apply middleware that preserve the http.Flusher interface
	// Skip error handling and recovery middleware that might wrap the response writer

	// Correlation middleware (safe for SSE)
	if t.config.EnableCorrelation {
		handler = correlation.CorrelationMiddleware()(handler)
	}

	// Tracing middleware (safe for SSE)
	if t.tracingMiddleware != nil {
		handler = t.tracingMiddleware.HTTPMiddleware()(handler)
	}

	// CORS middleware (safe for SSE)
	if t.config.EnableCORS {
		handler = t.corsMiddleware(handler)
	}

	// Skip recovery and error handling middleware for SSE
	// as they can wrap the response writer and break the Flusher interface

	return handler
}
