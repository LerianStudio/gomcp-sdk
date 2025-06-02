package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/LerianStudio/gomcp-sdk/correlation"
	"github.com/LerianStudio/gomcp-sdk/protocol"
)

// TracingConfig configures the tracing middleware
type TracingConfig struct {
	ServiceName      string
	ServiceVersion   string
	TracerName       string
	Propagator       propagation.TextMapPropagator
	SkipHealthCheck  bool
	EnableMetrics    bool
	CustomAttributes []attribute.KeyValue
}

// TracingMiddleware provides distributed tracing capabilities
type TracingMiddleware struct {
	config     TracingConfig
	tracer     trace.Tracer
	propagator propagation.TextMapPropagator
}

// NewTracingMiddleware creates a new tracing middleware
func NewTracingMiddleware(config TracingConfig) *TracingMiddleware {
	if config.TracerName == "" {
		config.TracerName = "mcp-sdk"
	}

	if config.Propagator == nil {
		config.Propagator = otel.GetTextMapPropagator()
	}

	tracer := otel.Tracer(config.TracerName)

	return &TracingMiddleware{
		config:     config,
		tracer:     tracer,
		propagator: config.Propagator,
	}
}

// HTTPMiddleware returns an HTTP middleware that adds tracing
func (tm *TracingMiddleware) HTTPMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip health checks if configured
			if tm.config.SkipHealthCheck && isHealthCheckRequest(r) {
				next.ServeHTTP(w, r)
				return
			}

			// Extract trace context from headers
			ctx := tm.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Create span
			spanName := tm.generateHTTPSpanName(r)
			ctx, span := tm.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			// Add HTTP attributes
			tm.addHTTPAttributes(span, r)

			// Add custom attributes
			if len(tm.config.CustomAttributes) > 0 {
				span.SetAttributes(tm.config.CustomAttributes...)
			}

			// Create response writer wrapper to capture status code
			wrappedWriter := &responseWriterWrapper{
				ResponseWriter: w,
				statusCode:     200,
			}

			// Set trace context in response headers
			tm.propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			// Continue with request
			start := time.Now()
			next.ServeHTTP(wrappedWriter, r.WithContext(ctx))
			duration := time.Since(start)

			// Add response attributes
			tm.addHTTPResponseAttributes(span, wrappedWriter.statusCode, duration)

			// Set span status based on HTTP status
			tm.setSpanStatusFromHTTP(span, wrappedWriter.statusCode)
		})
	}
}

// WebSocketMiddleware provides tracing for WebSocket connections
func (tm *TracingMiddleware) WebSocketMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract trace context from headers
			ctx := tm.propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

			// Create span for WebSocket upgrade
			spanName := "websocket.upgrade"
			ctx, span := tm.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))
			defer span.End()

			// Add WebSocket attributes
			tm.addWebSocketAttributes(span, r)

			// Set trace context in response headers
			tm.propagator.Inject(ctx, propagation.HeaderCarrier(w.Header()))

			start := time.Now()
			next.ServeHTTP(w, r.WithContext(ctx))
			duration := time.Since(start)

			// Add duration attribute
			span.SetAttributes(
				attribute.String("websocket.upgrade.duration", duration.String()),
			)

			span.SetStatus(codes.Ok, "WebSocket connection established")
		})
	}
}

// TraceJSONRPCRequest traces a JSON-RPC request
func (tm *TracingMiddleware) TraceJSONRPCRequest(ctx context.Context, req *protocol.JSONRPCRequest) (context.Context, trace.Span) {
	spanName := tm.generateJSONRPCSpanName(req)
	ctx, span := tm.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindServer))

	// Add JSON-RPC specific attributes
	tm.addJSONRPCAttributes(span, req)

	// Add correlation data to span
	tm.addCorrelationAttributes(span, ctx)

	return ctx, span
}

// TraceWebSocketMessage traces a WebSocket message
func (tm *TracingMiddleware) TraceWebSocketMessage(ctx context.Context, messageType string, connID string) (context.Context, trace.Span) {
	spanName := "websocket.message." + messageType
	ctx, span := tm.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	span.SetAttributes(
		attribute.String("websocket.message.type", messageType),
		attribute.String("websocket.connection.id", connID),
		attribute.String("component", "websocket"),
	)

	return ctx, span
}

// TraceSSEEvent traces a Server-Sent Event
func (tm *TracingMiddleware) TraceSSEEvent(ctx context.Context, eventType string, clientID string) (context.Context, trace.Span) {
	spanName := "sse.event." + eventType
	ctx, span := tm.tracer.Start(ctx, spanName, trace.WithSpanKind(trace.SpanKindInternal))

	span.SetAttributes(
		attribute.String("sse.event.type", eventType),
		attribute.String("sse.client.id", clientID),
		attribute.String("component", "sse"),
	)

	return ctx, span
}

// Helper methods

func (tm *TracingMiddleware) generateHTTPSpanName(r *http.Request) string {
	return r.Method + " " + r.URL.Path
}

func (tm *TracingMiddleware) generateJSONRPCSpanName(req *protocol.JSONRPCRequest) string {
	if req.Method != "" {
		return "jsonrpc." + req.Method
	}
	return "jsonrpc.request"
}

func (tm *TracingMiddleware) addHTTPAttributes(span trace.Span, r *http.Request) {
	span.SetAttributes(
		semconv.HTTPMethodKey.String(r.Method),
		semconv.HTTPURLKey.String(r.URL.String()),
		semconv.HTTPSchemeKey.String(r.URL.Scheme),
		semconv.NetHostName(r.Host),
		semconv.UserAgentOriginal(r.UserAgent()),
		semconv.HTTPRequestContentLengthKey.Int64(r.ContentLength),
		attribute.String("component", "http"),
	)

	// Add remote address if available
	if r.RemoteAddr != "" {
		span.SetAttributes(attribute.String("http.client.remote_addr", r.RemoteAddr))
	}

	// Add content type if available
	if contentType := r.Header.Get("Content-Type"); contentType != "" {
		span.SetAttributes(attribute.String("http.request.content_type", contentType))
	}
}

func (tm *TracingMiddleware) addHTTPResponseAttributes(span trace.Span, statusCode int, duration time.Duration) {
	span.SetAttributes(
		semconv.HTTPStatusCodeKey.Int(statusCode),
		attribute.String("http.response.duration", duration.String()),
		attribute.Int64("http.response.duration_ms", duration.Milliseconds()),
	)
}

func (tm *TracingMiddleware) addWebSocketAttributes(span trace.Span, r *http.Request) {
	span.SetAttributes(
		attribute.String("websocket.upgrade.protocol", r.Header.Get("Sec-WebSocket-Protocol")),
		attribute.String("websocket.upgrade.key", r.Header.Get("Sec-WebSocket-Key")),
		attribute.String("websocket.upgrade.version", r.Header.Get("Sec-WebSocket-Version")),
		attribute.String("component", "websocket"),
	)
}

func (tm *TracingMiddleware) addJSONRPCAttributes(span trace.Span, req *protocol.JSONRPCRequest) {
	span.SetAttributes(
		attribute.String("jsonrpc.version", req.JSONRPC),
		attribute.String("jsonrpc.method", req.Method),
		attribute.String("component", "jsonrpc"),
	)

	// Add ID if present
	if req.ID != nil {
		span.SetAttributes(attribute.String("jsonrpc.id", fmt.Sprintf("%v", req.ID)))
	}
}

func (tm *TracingMiddleware) setSpanStatusFromHTTP(span trace.Span, statusCode int) {
	if statusCode >= 400 {
		span.SetStatus(codes.Error, http.StatusText(statusCode))
	} else {
		span.SetStatus(codes.Ok, "")
	}
}

func isHealthCheckRequest(r *http.Request) bool {
	healthPaths := []string{"/health", "/healthz", "/ping", "/status"}
	for _, path := range healthPaths {
		if r.URL.Path == path {
			return true
		}
	}
	return false
}

// responseWriterWrapper wraps http.ResponseWriter to capture status code
type responseWriterWrapper struct {
	http.ResponseWriter
	statusCode int
}

func (w *responseWriterWrapper) WriteHeader(statusCode int) {
	w.statusCode = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *responseWriterWrapper) Write(data []byte) (int, error) {
	return w.ResponseWriter.Write(data)
}

// FinishSpanWithError finishes a span with error information
func FinishSpanWithError(span trace.Span, err error) {
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	span.End()
}

// AddCorrelationID adds a correlation ID to the span
func AddCorrelationID(span trace.Span, correlationID string) {
	if correlationID != "" {
		span.SetAttributes(attribute.String("correlation.id", correlationID))
	}
}

// ExtractCorrelationID extracts correlation ID from context
func ExtractCorrelationID(ctx context.Context) string {
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}
	return ""
}

// addCorrelationAttributes adds correlation data to spans
func (tm *TracingMiddleware) addCorrelationAttributes(span trace.Span, ctx context.Context) {
	data := correlation.GetCorrelationData(ctx)

	if data.CorrelationID != "" {
		span.SetAttributes(attribute.String("correlation.id", data.CorrelationID))
	}
	if data.RequestID != "" {
		span.SetAttributes(attribute.String("request.id", data.RequestID))
	}
	if data.SessionID != "" {
		span.SetAttributes(attribute.String("session.id", data.SessionID))
	}
	if data.UserID != "" {
		span.SetAttributes(attribute.String("user.id", data.UserID))
	}
}
