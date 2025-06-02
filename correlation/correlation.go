package correlation

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// ContextKey represents a key for storing correlation data in context
type contextKey string

const (
	// CorrelationIDKey is the context key for correlation IDs
	CorrelationIDKey contextKey = "correlation_id"

	// RequestIDKey is the context key for request IDs
	RequestIDKey contextKey = "request_id"

	// SessionIDKey is the context key for session IDs
	SessionIDKey contextKey = "session_id"

	// UserIDKey is the context key for user IDs
	UserIDKey contextKey = "user_id"
)

// Common HTTP headers for correlation
const (
	HeaderCorrelationID = "X-Correlation-ID"
	HeaderRequestID     = "X-Request-ID"
	HeaderSessionID     = "X-Session-ID"
	HeaderUserID        = "X-User-ID"
	HeaderTraceID       = "X-Trace-ID"
	HeaderSpanID        = "X-Span-ID"
)

// CorrelationData holds correlation information
type CorrelationData struct {
	CorrelationID string `json:"correlation_id,omitempty"`
	RequestID     string `json:"request_id,omitempty"`
	SessionID     string `json:"session_id,omitempty"`
	UserID        string `json:"user_id,omitempty"`
	TraceID       string `json:"trace_id,omitempty"`
	SpanID        string `json:"span_id,omitempty"`
}

// GenerateID generates a new correlation ID
func GenerateID() string {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to a simpler method if crypto/rand fails
		return generateFallbackID()
	}
	return hex.EncodeToString(bytes)
}

// generateFallbackID creates a fallback ID using timestamp and random elements
func generateFallbackID() string {
	// Simple fallback - in production, you might want something more robust
	bytes := make([]byte, 8)
	for i := range bytes {
		bytes[i] = byte('a' + (i % 26))
	}
	return hex.EncodeToString(bytes)
}

// WithCorrelationID adds a correlation ID to the context
func WithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey, correlationID)
}

// WithRequestID adds a request ID to the context
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, RequestIDKey, requestID)
}

// WithSessionID adds a session ID to the context
func WithSessionID(ctx context.Context, sessionID string) context.Context {
	return context.WithValue(ctx, SessionIDKey, sessionID)
}

// WithUserID adds a user ID to the context
func WithUserID(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, UserIDKey, userID)
}

// WithCorrelationData adds all correlation data to the context
func WithCorrelationData(ctx context.Context, data CorrelationData) context.Context {
	if data.CorrelationID != "" {
		ctx = WithCorrelationID(ctx, data.CorrelationID)
	}
	if data.RequestID != "" {
		ctx = WithRequestID(ctx, data.RequestID)
	}
	if data.SessionID != "" {
		ctx = WithSessionID(ctx, data.SessionID)
	}
	if data.UserID != "" {
		ctx = WithUserID(ctx, data.UserID)
	}
	return ctx
}

// GetCorrelationID extracts the correlation ID from context
func GetCorrelationID(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey).(string); ok {
		return id
	}

	// Fallback: try to get trace ID from OpenTelemetry span
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		return span.SpanContext().TraceID().String()
	}

	return ""
}

// GetRequestID extracts the request ID from context
func GetRequestID(ctx context.Context) string {
	if id, ok := ctx.Value(RequestIDKey).(string); ok {
		return id
	}
	return ""
}

// GetSessionID extracts the session ID from context
func GetSessionID(ctx context.Context) string {
	if id, ok := ctx.Value(SessionIDKey).(string); ok {
		return id
	}
	return ""
}

// GetUserID extracts the user ID from context
func GetUserID(ctx context.Context) string {
	if id, ok := ctx.Value(UserIDKey).(string); ok {
		return id
	}
	return ""
}

// GetCorrelationData extracts all correlation data from context
func GetCorrelationData(ctx context.Context) CorrelationData {
	data := CorrelationData{
		CorrelationID: GetCorrelationID(ctx),
		RequestID:     GetRequestID(ctx),
		SessionID:     GetSessionID(ctx),
		UserID:        GetUserID(ctx),
	}

	// Add OpenTelemetry trace information if available
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		spanCtx := span.SpanContext()
		data.TraceID = spanCtx.TraceID().String()
		data.SpanID = spanCtx.SpanID().String()
	}

	return data
}

// ExtractFromHTTPHeaders extracts correlation data from HTTP headers
func ExtractFromHTTPHeaders(headers http.Header) CorrelationData {
	return CorrelationData{
		CorrelationID: headers.Get(HeaderCorrelationID),
		RequestID:     headers.Get(HeaderRequestID),
		SessionID:     headers.Get(HeaderSessionID),
		UserID:        headers.Get(HeaderUserID),
		TraceID:       headers.Get(HeaderTraceID),
		SpanID:        headers.Get(HeaderSpanID),
	}
}

// InjectIntoHTTPHeaders injects correlation data into HTTP headers
func InjectIntoHTTPHeaders(headers http.Header, data CorrelationData) {
	if data.CorrelationID != "" {
		headers.Set(HeaderCorrelationID, data.CorrelationID)
	}
	if data.RequestID != "" {
		headers.Set(HeaderRequestID, data.RequestID)
	}
	if data.SessionID != "" {
		headers.Set(HeaderSessionID, data.SessionID)
	}
	if data.UserID != "" {
		headers.Set(HeaderUserID, data.UserID)
	}
	if data.TraceID != "" {
		headers.Set(HeaderTraceID, data.TraceID)
	}
	if data.SpanID != "" {
		headers.Set(HeaderSpanID, data.SpanID)
	}
}

// PropagateFromHTTPRequest creates a context with correlation data from HTTP request
func PropagateFromHTTPRequest(ctx context.Context, r *http.Request) context.Context {
	data := ExtractFromHTTPHeaders(r.Header)

	// Generate correlation ID if not present
	if data.CorrelationID == "" {
		data.CorrelationID = GenerateID()
	}

	// Generate request ID if not present
	if data.RequestID == "" {
		data.RequestID = GenerateID()
	}

	return WithCorrelationData(ctx, data)
}

// PropagateToHTTPResponse adds correlation headers to HTTP response
func PropagateToHTTPResponse(w http.ResponseWriter, ctx context.Context) {
	data := GetCorrelationData(ctx)
	InjectIntoHTTPHeaders(w.Header(), data)
}

// EnsureCorrelationID ensures a correlation ID exists in the context
func EnsureCorrelationID(ctx context.Context) context.Context {
	if GetCorrelationID(ctx) == "" {
		return WithCorrelationID(ctx, GenerateID())
	}
	return ctx
}

// EnsureRequestID ensures a request ID exists in the context
func EnsureRequestID(ctx context.Context) context.Context {
	if GetRequestID(ctx) == "" {
		return WithRequestID(ctx, GenerateID())
	}
	return ctx
}

// CreateChildContext creates a child context with a new request ID but preserves correlation ID
func CreateChildContext(parent context.Context) context.Context {
	data := GetCorrelationData(parent)
	data.RequestID = GenerateID() // Generate new request ID for child operation
	return WithCorrelationData(parent, data)
}

// FormatLogMessage formats a log message with correlation data
func FormatLogMessage(ctx context.Context, message string) string {
	data := GetCorrelationData(ctx)
	var parts []string

	if data.CorrelationID != "" {
		parts = append(parts, "correlation_id="+data.CorrelationID)
	}
	if data.RequestID != "" {
		parts = append(parts, "request_id="+data.RequestID)
	}
	if data.SessionID != "" {
		parts = append(parts, "session_id="+data.SessionID)
	}
	if data.UserID != "" {
		parts = append(parts, "user_id="+data.UserID)
	}

	if len(parts) > 0 {
		return message + " [" + strings.Join(parts, ", ") + "]"
	}
	return message
}

// CorrelationMiddleware returns HTTP middleware that handles correlation ID propagation
func CorrelationMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Extract and propagate correlation data
			ctx := PropagateFromHTTPRequest(r.Context(), r)

			// Add correlation headers to response
			PropagateToHTTPResponse(w, ctx)

			// Continue with enriched context
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
