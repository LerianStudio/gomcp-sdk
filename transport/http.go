// Package transport implements MCP transport layers
package transport

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"

	"github.com/LerianStudio/gomcp-sdk/correlation"
	"github.com/LerianStudio/gomcp-sdk/middleware"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/shutdown"
)

// ConnectionPoolMetrics tracks connection pool performance
type ConnectionPoolMetrics struct {
	// Current state metrics
	IdleConnections   int64 `json:"idle_connections"`
	ActiveConnections int64 `json:"active_connections"`
	TotalConnections  int64 `json:"total_connections"`

	// Performance metrics
	ConnectionAcquisitionTime time.Duration `json:"connection_acquisition_time"`
	ConnectionUtilization     float64       `json:"connection_utilization"`

	// Rate metrics
	ConnectionsCreated int64 `json:"connections_created"`
	ConnectionsReused  int64 `json:"connections_reused"`
	ConnectionsClosed  int64 `json:"connections_closed"`

	// Error metrics
	ConnectionTimeouts int64 `json:"connection_timeouts"`
	ConnectionErrors   int64 `json:"connection_errors"`
	TLSHandshakeErrors int64 `json:"tls_handshake_errors"`

	// Timestamp of last update
	LastUpdated time.Time `json:"last_updated"`
}

// ConnectionPoolMonitor provides monitoring capabilities for HTTP connection pool
type ConnectionPoolMonitor struct {
	transport *http.Transport
	metrics   *ConnectionPoolMetrics
	config    *ConnectionPoolConfig
	mu        sync.RWMutex
	ticker    *time.Ticker
	stopCh    chan struct{}
	running   bool
}

// ConnectionPoolConfig configures HTTP connection pooling
type ConnectionPoolConfig struct {
	// Maximum number of idle connections per host
	MaxIdleConns int

	// Maximum number of idle connections to keep alive per host
	MaxIdleConnsPerHost int

	// Maximum number of connections per host
	MaxConnsPerHost int

	// Keep-alive timeout for idle connections
	IdleConnTimeout time.Duration

	// TLS handshake timeout
	TLSHandshakeTimeout time.Duration

	// Response header timeout
	ResponseHeaderTimeout time.Duration

	// Expect continue timeout
	ExpectContinueTimeout time.Duration

	// Enable connection pool monitoring
	EnableMonitoring bool

	// Metrics collection interval
	MetricsInterval time.Duration
}

// HTTPConfig contains configuration for HTTP transport
type HTTPConfig struct {
	// Address to listen on (e.g., ":8080" or "localhost:8080")
	Address string

	// TLS configuration (optional)
	TLSConfig *tls.Config

	// Read/write timeouts
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	IdleTimeout  time.Duration

	// Maximum request body size (default: 10MB)
	MaxBodySize int64

	// Custom headers to include in responses
	CustomHeaders map[string]string

	// Enable CORS
	EnableCORS bool

	// CORS allowed origins (if EnableCORS is true)
	AllowedOrigins []string

	// Path to handle requests (default: "/")
	Path string

	// Error handling configuration
	ErrorHandling *middleware.ErrorHandlingConfig

	// Connection pooling configuration
	ConnectionPool *ConnectionPoolConfig

	// Tracing configuration
	TracingConfig *middleware.TracingConfig

	// Enable correlation ID propagation
	EnableCorrelation bool
}

// HTTPTransport implements HTTP/HTTPS transport for MCP
type HTTPTransport struct {
	config            *HTTPConfig
	server            *http.Server
	listener          net.Listener
	handler           RequestHandler
	errorHandler      *middleware.ErrorHandlingMiddleware
	mu                sync.RWMutex
	running           bool
	certFile          string
	keyFile           string
	connectionPool    *http.Transport
	connectionTracker *shutdown.ConnectionTracker
	activeRequests    int64
	shutdownConfig    *shutdown.ShutdownConfig
	poolMonitor       *ConnectionPoolMonitor
	tracingMiddleware *middleware.TracingMiddleware
}

// NewHTTPTransport creates a new HTTP transport
func NewHTTPTransport(config *HTTPConfig) *HTTPTransport {
	if config.MaxBodySize == 0 {
		config.MaxBodySize = 10 * 1024 * 1024 // 10MB default
	}
	if config.Path == "" {
		config.Path = "/"
	}
	if config.ReadTimeout == 0 {
		config.ReadTimeout = 30 * time.Second
	}
	if config.WriteTimeout == 0 {
		config.WriteTimeout = 30 * time.Second
	}
	if config.IdleTimeout == 0 {
		config.IdleTimeout = 120 * time.Second
	}

	// Initialize error handling middleware
	var errorHandler *middleware.ErrorHandlingMiddleware
	if config.ErrorHandling != nil {
		errorHandler = middleware.NewErrorHandlingMiddleware(config.ErrorHandling)
	} else {
		// Use default production-safe error handling
		defaultConfig := &middleware.ErrorHandlingConfig{
			IncludeStackTrace: false,
			IncludeDebugInfo:  false,
			LogErrors:         true,
			ErrorTransformer:  middleware.ProductionErrorTransformer,
		}
		errorHandler = middleware.NewErrorHandlingMiddleware(defaultConfig)
	}

	// Initialize connection pool
	var connectionPool *http.Transport
	if config.ConnectionPool != nil {
		connectionPool = &http.Transport{
			MaxIdleConns:          config.ConnectionPool.MaxIdleConns,
			MaxIdleConnsPerHost:   config.ConnectionPool.MaxIdleConnsPerHost,
			MaxConnsPerHost:       config.ConnectionPool.MaxConnsPerHost,
			IdleConnTimeout:       config.ConnectionPool.IdleConnTimeout,
			TLSHandshakeTimeout:   config.ConnectionPool.TLSHandshakeTimeout,
			ResponseHeaderTimeout: config.ConnectionPool.ResponseHeaderTimeout,
			ExpectContinueTimeout: config.ConnectionPool.ExpectContinueTimeout,
		}
	} else {
		// Use default connection pool settings
		connectionPool = &http.Transport{
			MaxIdleConns:          100,
			MaxIdleConnsPerHost:   10,
			MaxConnsPerHost:       100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		}
	}

	transport := &HTTPTransport{
		config:            config,
		errorHandler:      errorHandler,
		connectionPool:    connectionPool,
		connectionTracker: shutdown.NewConnectionTracker(),
		shutdownConfig:    shutdown.DefaultShutdownConfig(),
	}

	// Initialize connection pool monitor if enabled
	if config.ConnectionPool != nil && config.ConnectionPool.EnableMonitoring {
		transport.poolMonitor = NewConnectionPoolMonitor(connectionPool, config.ConnectionPool)
	}

	// Initialize tracing middleware if enabled
	if config.TracingConfig != nil {
		transport.tracingMiddleware = middleware.NewTracingMiddleware(*config.TracingConfig)
	}

	return transport
}

// NewHTTPSTransport creates a new HTTPS transport
func NewHTTPSTransport(config *HTTPConfig, certFile, keyFile string) *HTTPTransport {
	transport := NewHTTPTransport(config)
	transport.certFile = certFile
	transport.keyFile = keyFile
	return transport
}

// Start starts the HTTP transport
func (t *HTTPTransport) Start(ctx context.Context, handler RequestHandler) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return fmt.Errorf("transport already running")
	}

	t.handler = handler

	mux := http.NewServeMux()
	mux.HandleFunc(t.config.Path, t.handleRequest)

	// Create listener first to get the actual port
	listener, err := net.Listen("tcp", t.config.Address)
	if err != nil {
		return fmt.Errorf("failed to create listener: %w", err)
	}
	t.listener = listener

	t.server = &http.Server{
		Handler:      t.wrapWithMiddleware(mux),
		ReadTimeout:  t.config.ReadTimeout,
		WriteTimeout: t.config.WriteTimeout,
		IdleTimeout:  t.config.IdleTimeout,
		TLSConfig:    t.config.TLSConfig,
	}

	t.running = true

	// Start connection pool monitoring if configured
	t.StartConnectionPoolMonitoring()

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
			fmt.Printf("Error stopping HTTP transport: %v\n", err)
		}
	}()

	return nil
}

// Stop stops the HTTP transport gracefully
func (t *HTTPTransport) Stop() error {
	return t.GracefulShutdown(context.Background())
}

// ForceStop forces immediate shutdown of the HTTP transport
func (t *HTTPTransport) ForceStop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	t.running = false

	// Close listener immediately
	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			fmt.Printf("Error closing HTTP listener: %v\n", err)
		}
	}

	// Close server without graceful shutdown
	if t.server != nil {
		return t.server.Close()
	}

	return nil
}

// IsRunning returns whether the transport is running
func (t *HTTPTransport) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// ConnectionCount returns the number of active connections
func (t *HTTPTransport) ConnectionCount() int {
	return t.connectionTracker.Count() + int(atomic.LoadInt64(&t.activeRequests))
}

// GracefulShutdown performs graceful shutdown with connection draining
func (t *HTTPTransport) GracefulShutdown(ctx context.Context) error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	t.mu.Unlock()

	fmt.Println("🛑 HTTP Transport: Initiating graceful shutdown...")

	// Stop connection pool monitoring
	t.StopConnectionPoolMonitoring()

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, t.shutdownConfig.Timeout)
	defer cancel()

	// Phase 1: Stop accepting new connections
	fmt.Println("🚫 HTTP Transport: Stopping new connections...")
	if t.listener != nil {
		if err := t.listener.Close(); err != nil {
			fmt.Printf("Error closing HTTP listener: %v\n", err)
		}
	}

	// Phase 2: Wait for active requests to complete
	if t.shutdownConfig.GracePeriod > 0 {
		fmt.Printf("⏳ HTTP Transport: Grace period (%v) for active requests...\n", t.shutdownConfig.GracePeriod)

		gracePeriodCtx, graceCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.GracePeriod)
		t.waitForActiveRequests(gracePeriodCtx)
		graceCancel()
	}

	// Phase 3: Drain connections
	if t.shutdownConfig.DrainConnections {
		fmt.Printf("🔄 HTTP Transport: Draining connections (timeout: %v)...\n", t.shutdownConfig.DrainTimeout)

		drainCtx, drainCancel := context.WithTimeout(shutdownCtx, t.shutdownConfig.DrainTimeout)
		t.drainConnections(drainCtx)
		drainCancel()
	}

	// Phase 4: Shutdown server
	fmt.Println("🔌 HTTP Transport: Shutting down server...")
	if t.server != nil {
		err := t.server.Shutdown(shutdownCtx)
		if err != nil {
			fmt.Printf("❌ HTTP Transport: Server shutdown error: %v\n", err)
			return err
		}
	}

	fmt.Println("✅ HTTP Transport: Graceful shutdown completed")
	return nil
}

// waitForActiveRequests waits for active requests to complete
func (t *HTTPTransport) waitForActiveRequests(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs > 0 {
				fmt.Printf("⏰ HTTP Transport: Grace period timeout with %d active requests\n", activeReqs)
			}
			return
		case <-ticker.C:
			activeReqs := atomic.LoadInt64(&t.activeRequests)
			if activeReqs == 0 {
				fmt.Println("✅ HTTP Transport: All active requests completed")
				return
			}
			fmt.Printf("⏳ HTTP Transport: Waiting for %d active requests...\n", activeReqs)
		}
	}
}

// drainConnections drains existing connections
func (t *HTTPTransport) drainConnections(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			connCount := t.connectionTracker.Count()
			if connCount > 0 {
				fmt.Printf("⏰ HTTP Transport: Connection draining timeout with %d connections\n", connCount)
			}
			return
		case <-ticker.C:
			connCount := t.connectionTracker.Count()
			if connCount == 0 {
				fmt.Println("✅ HTTP Transport: All connections drained")
				return
			}
			fmt.Printf("🔄 HTTP Transport: Draining %d remaining connections...\n", connCount)
		}
	}
}

// handleRequest handles incoming HTTP requests
func (t *HTTPTransport) handleRequest(w http.ResponseWriter, r *http.Request) {
	// Track active request
	atomic.AddInt64(&t.activeRequests, 1)
	defer atomic.AddInt64(&t.activeRequests, -1)

	// Track connection
	connID := t.connectionTracker.AddConnection("http", r.RemoteAddr)
	defer t.connectionTracker.RemoveConnection(connID)

	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Security: Validate Content-Type for JSON-RPC protocol compliance
	contentType := r.Header.Get("Content-Type")
	if contentType != "" && !isValidJSONContentType(contentType) {
		t.writeErrorWithID(w, nil, protocol.ParseError, "Invalid Content-Type: expected application/json", nil)
		return
	}

	// Limit request body size
	r.Body = http.MaxBytesReader(w, r.Body, t.config.MaxBodySize)

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		t.writeErrorWithID(w, nil, protocol.ParseError, "Failed to read request body", nil)
		return
	}

	// Always try to extract ID first for better error responses
	requestID := extractIDFromBody(body)

	// Parse message flexibly to handle both requests and error responses
	parsed, parseErr := protocol.ParseJSONRPCMessage(body)
	if parseErr != nil {
		if jsonRPCErr, ok := parseErr.(*protocol.JSONRPCError); ok {
			t.writeErrorWithID(w, requestID, jsonRPCErr.Code, jsonRPCErr.Message, jsonRPCErr.Data)
		}
		return
	}

	// Only handle requests in HTTP transport
	if parsed.Request == nil {
		// For non-JSON-RPC messages, we already have the ID from extraction above
		t.writeErrorWithID(w, requestID, protocol.InvalidRequest, "Expected JSON-RPC request", nil)
		return
	}

	// Handle request with JSON-RPC tracing
	ctx := r.Context()

	// Add JSON-RPC tracing if enabled
	if t.tracingMiddleware != nil {
		var span trace.Span
		ctx, span = t.tracingMiddleware.TraceJSONRPCRequest(ctx, parsed.Request)
		defer span.End()

		// Handle the request
		resp := t.handler.HandleRequest(ctx, parsed.Request)

		// Set span status based on response
		if resp.Error != nil {
			span.SetStatus(codes.Error, resp.Error.Message)
		} else {
			span.SetStatus(codes.Ok, "")
		}

		// Write response
		t.writeResponse(w, resp)
	} else {
		// Handle without tracing
		resp := t.handler.HandleRequest(ctx, parsed.Request)
		t.writeResponse(w, resp)
	}
}

// writeResponse writes a JSON-RPC response
func (t *HTTPTransport) writeResponse(w http.ResponseWriter, resp *protocol.JSONRPCResponse) {
	w.Header().Set("Content-Type", "application/json")

	// Add custom headers
	for k, v := range t.config.CustomHeaders {
		w.Header().Set(k, v)
	}

	if err := json.NewEncoder(w).Encode(resp); err != nil {
		// Log error but can't send another response
		fmt.Printf("Failed to encode response: %v\n", err)
	}
}

// writeError writes a JSON-RPC error response with nil ID (for backward compatibility)
func (t *HTTPTransport) writeError(w http.ResponseWriter, code int, message string, data interface{}) {
	t.writeErrorWithID(w, nil, code, message, data)
}

// writeErrorWithID writes a JSON-RPC error response with specified ID
func (t *HTTPTransport) writeErrorWithID(w http.ResponseWriter, id interface{}, code int, message string, data interface{}) {
	resp := &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error:   protocol.NewJSONRPCError(code, message, data),
	}
	t.writeResponse(w, resp)
}

// wrapWithMiddleware wraps the handler with middleware
func (t *HTTPTransport) wrapWithMiddleware(handler http.Handler) http.Handler {
	// Correlation middleware (innermost, applied first)
	if t.config.EnableCorrelation {
		handler = correlation.CorrelationMiddleware()(handler)
	}

	// Tracing middleware
	if t.tracingMiddleware != nil {
		handler = t.tracingMiddleware.HTTPMiddleware()(handler)
	}

	// Error handling middleware
	if t.errorHandler != nil {
		handler = t.errorHandler.WrapHTTPHandler(handler)
	}

	// CORS middleware
	if t.config.EnableCORS {
		handler = t.corsMiddleware(handler)
	}

	// Recovery middleware (outermost)
	handler = t.recoveryMiddleware(handler)

	return handler
}

// corsMiddleware adds CORS headers
func (t *HTTPTransport) corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")

		// Check if origin is allowed
		allowed := false
		if len(t.config.AllowedOrigins) == 0 {
			allowed = true // Allow all origins if none specified
			w.Header().Set("Access-Control-Allow-Origin", "*")
		} else {
			for _, allowedOrigin := range t.config.AllowedOrigins {
				if allowedOrigin == "*" || allowedOrigin == origin {
					allowed = true
					w.Header().Set("Access-Control-Allow-Origin", origin)
					break
				}
			}
		}

		if allowed {
			w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "86400")
		}

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// recoveryMiddleware recovers from panics
func (t *HTTPTransport) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := recover(); err != nil {
				t.writeError(w, protocol.InternalError, "Internal server error", nil)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// Address returns the actual listening address
func (t *HTTPTransport) Address() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if t.listener != nil {
		return t.listener.Addr().String()
	}
	return t.config.Address
}

// GetConnectionPool returns the connection pool for HTTP clients
func (t *HTTPTransport) GetConnectionPool() *http.Transport {
	return t.connectionPool
}

// ConnectionPoolStats returns connection pool statistics
func (t *HTTPTransport) ConnectionPoolStats() map[string]interface{} {
	if t.connectionPool == nil {
		return map[string]interface{}{
			"enabled": false,
		}
	}

	// Note: Go's http.Transport doesn't expose internal statistics
	// In a production environment, you might want to use a custom
	// transport that tracks these metrics
	return map[string]interface{}{
		"enabled":                 true,
		"max_idle_conns":          t.connectionPool.MaxIdleConns,
		"max_idle_conns_per_host": t.connectionPool.MaxIdleConnsPerHost,
		"max_conns_per_host":      t.connectionPool.MaxConnsPerHost,
		"idle_conn_timeout":       t.connectionPool.IdleConnTimeout.String(),
		"tls_handshake_timeout":   t.connectionPool.TLSHandshakeTimeout.String(),
		"response_header_timeout": t.connectionPool.ResponseHeaderTimeout.String(),
		"expect_continue_timeout": t.connectionPool.ExpectContinueTimeout.String(),
	}
}

// extractIDFromBody attempts to extract an ID from JSON body for error responses
func extractIDFromBody(body []byte) interface{} {
	var raw map[string]interface{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil // Can't parse JSON, no ID available
	}

	if id, exists := raw["id"]; exists {
		return id
	}

	return nil
}

// isValidJSONContentType validates that the content type is appropriate for JSON-RPC
func isValidJSONContentType(contentType string) bool {
	// Split content type and charset/boundary parameters
	parts := strings.Split(strings.ToLower(contentType), ";")
	if len(parts) == 0 {
		return false
	}

	mediaType := strings.TrimSpace(parts[0])

	// Accept standard JSON media types
	validTypes := []string{
		"application/json",
		"application/json-rpc",
		"text/json",
	}

	for _, validType := range validTypes {
		if mediaType == validType {
			return true
		}
	}

	return false
}

// NewConnectionPoolMonitor creates a new connection pool monitor
func NewConnectionPoolMonitor(transport *http.Transport, config *ConnectionPoolConfig) *ConnectionPoolMonitor {
	interval := config.MetricsInterval
	if interval == 0 {
		interval = 30 * time.Second // Default monitoring interval
	}

	return &ConnectionPoolMonitor{
		transport: transport,
		config:    config,
		metrics: &ConnectionPoolMetrics{
			LastUpdated: time.Now(),
		},
		stopCh: make(chan struct{}),
	}
}

// Start begins monitoring the connection pool
func (m *ConnectionPoolMonitor) Start() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.running {
		return
	}

	m.running = true
	interval := m.config.MetricsInterval
	if interval == 0 {
		interval = 30 * time.Second
	}

	m.ticker = time.NewTicker(interval)

	go m.monitorLoop()
}

// Stop stops monitoring the connection pool
func (m *ConnectionPoolMonitor) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()

	if !m.running {
		return
	}

	m.running = false
	close(m.stopCh)
	if m.ticker != nil {
		m.ticker.Stop()
	}
}

// monitorLoop continuously collects metrics
func (m *ConnectionPoolMonitor) monitorLoop() {
	for {
		select {
		case <-m.stopCh:
			return
		case <-m.ticker.C:
			m.collectMetrics()
		}
	}
}

// collectMetrics gathers current connection pool metrics
func (m *ConnectionPoolMonitor) collectMetrics() {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Note: Go's http.Transport doesn't expose detailed connection pool metrics
	// directly. In a real implementation, we might need to use reflection or
	// custom instrumentation to get these values. For now, we'll simulate the
	// structure and provide placeholder functionality.

	now := time.Now()

	// Update metrics (in a real implementation, these would be actual values)
	m.metrics.LastUpdated = now

	// Calculate utilization based on configured limits
	if m.config.MaxIdleConnsPerHost > 0 {
		m.metrics.ConnectionUtilization = float64(m.metrics.ActiveConnections) / float64(m.config.MaxIdleConnsPerHost)
	}

	// Log metrics for debugging (could be replaced with proper metrics export)
	if m.metrics.TotalConnections > 0 {
		fmt.Printf("🔍 Connection Pool Metrics: Active=%d, Idle=%d, Total=%d, Utilization=%.2f%%\n",
			m.metrics.ActiveConnections,
			m.metrics.IdleConnections,
			m.metrics.TotalConnections,
			m.metrics.ConnectionUtilization*100)
	}
}

// GetMetrics returns current connection pool metrics
func (m *ConnectionPoolMonitor) GetMetrics() *ConnectionPoolMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()

	// Return a copy to prevent race conditions
	metricsCopy := *m.metrics
	return &metricsCopy
}

// IncrementConnectionsCreated increments the connections created counter
func (m *ConnectionPoolMonitor) IncrementConnectionsCreated() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.AddInt64(&m.metrics.ConnectionsCreated, 1)
	atomic.AddInt64(&m.metrics.TotalConnections, 1)
	atomic.AddInt64(&m.metrics.ActiveConnections, 1)
}

// IncrementConnectionsReused increments the connections reused counter
func (m *ConnectionPoolMonitor) IncrementConnectionsReused() {
	if m == nil {
		return
	}
	atomic.AddInt64(&m.metrics.ConnectionsReused, 1)
}

// IncrementConnectionsClosed increments the connections closed counter
func (m *ConnectionPoolMonitor) IncrementConnectionsClosed() {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()

	atomic.AddInt64(&m.metrics.ConnectionsClosed, 1)
	atomic.AddInt64(&m.metrics.ActiveConnections, -1)
	atomic.AddInt64(&m.metrics.IdleConnections, 1)
}

// IncrementConnectionTimeouts increments the connection timeout counter
func (m *ConnectionPoolMonitor) IncrementConnectionTimeouts() {
	if m == nil {
		return
	}
	atomic.AddInt64(&m.metrics.ConnectionTimeouts, 1)
}

// IncrementConnectionErrors increments the connection error counter
func (m *ConnectionPoolMonitor) IncrementConnectionErrors() {
	if m == nil {
		return
	}
	atomic.AddInt64(&m.metrics.ConnectionErrors, 1)
}

// IncrementTLSHandshakeErrors increments the TLS handshake error counter
func (m *ConnectionPoolMonitor) IncrementTLSHandshakeErrors() {
	if m == nil {
		return
	}
	atomic.AddInt64(&m.metrics.TLSHandshakeErrors, 1)
}

// GetConnectionPoolMetrics returns current connection pool metrics for the HTTP transport
func (t *HTTPTransport) GetConnectionPoolMetrics() *ConnectionPoolMetrics {
	if t.poolMonitor == nil {
		return nil
	}
	return t.poolMonitor.GetMetrics()
}

// StartConnectionPoolMonitoring starts monitoring the connection pool
func (t *HTTPTransport) StartConnectionPoolMonitoring() {
	if t.poolMonitor != nil {
		t.poolMonitor.Start()
	}
}

// StopConnectionPoolMonitoring stops monitoring the connection pool
func (t *HTTPTransport) StopConnectionPoolMonitoring() {
	if t.poolMonitor != nil {
		t.poolMonitor.Stop()
	}
}
