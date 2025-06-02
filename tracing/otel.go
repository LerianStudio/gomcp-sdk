package tracing

import (
	"context"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/LerianStudio/gomcp-sdk/protocol"
)

// Config holds the configuration for OpenTelemetry tracing
type Config struct {
	ServiceName           string
	ServiceVersion        string
	Environment           string
	Endpoint              string
	UseHTTP               bool
	Headers               map[string]string
	Insecure              bool
	EnableEnhancedTracing bool    // Enable enhanced span processor with correlation ID and runtime info
	SamplingRatio         float64 // Sampling ratio (0.0 to 1.0), 0 means AlwaysSample
}

// DefaultConfig returns a default tracing configuration
func DefaultConfig(serviceName, serviceVersion string) Config {
	return Config{
		ServiceName:           serviceName,
		ServiceVersion:        serviceVersion,
		Environment:           "development",
		Endpoint:              "http://localhost:4317",
		UseHTTP:               false,
		Headers:               make(map[string]string),
		Insecure:              true,
		EnableEnhancedTracing: true,
		SamplingRatio:         1.0, // Always sample by default
	}
}

// ProductionConfig returns a production-ready tracing configuration
func ProductionConfig(serviceName, serviceVersion, endpoint string) Config {
	return Config{
		ServiceName:           serviceName,
		ServiceVersion:        serviceVersion,
		Environment:           "production",
		Endpoint:              endpoint,
		UseHTTP:               false,
		Headers:               make(map[string]string),
		Insecure:              false,
		EnableEnhancedTracing: true,
		SamplingRatio:         0.1, // 10% sampling for production
	}
}

// Tracer wraps OpenTelemetry tracer with convenience methods
type Tracer struct {
	tracer trace.Tracer
}

// NewTracer initializes OpenTelemetry tracing and returns a configured tracer
func NewTracer(ctx context.Context, config Config) (*Tracer, func(context.Context) error, error) {
	// Create exporter
	var exporter *otlptrace.Exporter
	var err error

	if config.UseHTTP {
		opts := []otlptracehttp.Option{
			otlptracehttp.WithEndpoint(config.Endpoint),
		}
		if config.Insecure {
			opts = append(opts, otlptracehttp.WithInsecure())
		}
		if len(config.Headers) > 0 {
			opts = append(opts, otlptracehttp.WithHeaders(config.Headers))
		}
		exporter, err = otlptracehttp.New(ctx, opts...)
	} else {
		opts := []otlptracegrpc.Option{
			otlptracegrpc.WithEndpoint(config.Endpoint),
		}
		if config.Insecure {
			opts = append(opts, otlptracegrpc.WithInsecure())
		}
		if len(config.Headers) > 0 {
			opts = append(opts, otlptracegrpc.WithHeaders(config.Headers))
		}
		exporter, err = otlptracegrpc.New(ctx, opts...)
	}

	if err != nil {
		return nil, nil, fmt.Errorf("failed to create trace exporter: %w", err)
	}

	// Create resource
	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(config.ServiceName),
			semconv.ServiceVersion(config.ServiceVersion),
			semconv.DeploymentEnvironment(config.Environment),
		),
		resource.WithHost(),
		resource.WithProcess(),
		resource.WithTelemetrySDK(),
	)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create resource: %w", err)
	}

	// Create span processor (enhanced or standard based on config)
	batchProcessor := sdktrace.NewBatchSpanProcessor(exporter)
	var spanProcessor sdktrace.SpanProcessor = batchProcessor

	if config.EnableEnhancedTracing {
		spanProcessor = NewEnhancedSpanProcessor(batchProcessor)
	}

	// Configure sampler based on sampling ratio
	var sampler sdktrace.Sampler = sdktrace.AlwaysSample()
	if config.SamplingRatio > 0 && config.SamplingRatio < 1.0 {
		sampler = sdktrace.TraceIDRatioBased(config.SamplingRatio)
	} else if config.SamplingRatio == 0 {
		sampler = sdktrace.NeverSample()
	}

	// Create tracer provider
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(spanProcessor),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sampler),
	)

	// Set global tracer provider
	otel.SetTracerProvider(tp)

	// Set global propagator
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	// Create tracer
	tracer := tp.Tracer(
		config.ServiceName,
		trace.WithInstrumentationVersion(config.ServiceVersion),
	)

	// Shutdown function
	shutdown := func(ctx context.Context) error {
		return tp.Shutdown(ctx)
	}

	return &Tracer{tracer: tracer}, shutdown, nil
}

// StartSpan starts a new span with the given name
func (t *Tracer) StartSpan(ctx context.Context, spanName string, opts ...trace.SpanStartOption) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, spanName, opts...)
}

// StartSpanWithKind starts a new span with specific kind
func (t *Tracer) StartSpanWithKind(ctx context.Context, spanName string, kind trace.SpanKind) (context.Context, trace.Span) {
	return t.tracer.Start(ctx, spanName, trace.WithSpanKind(kind))
}

// TraceRequest creates a span for an MCP request
func (t *Tracer) TraceRequest(ctx context.Context, method string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	defaultAttrs := []attribute.KeyValue{
		attribute.String("mcp.method", method),
		attribute.String("mcp.protocol", "2.0"),
	}
	attrs = append(defaultAttrs, attrs...)

	return t.tracer.Start(ctx, fmt.Sprintf("MCP %s", method),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)
}

// TraceToolExecution creates a span for tool execution
func (t *Tracer) TraceToolExecution(ctx context.Context, toolName string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	defaultAttrs := []attribute.KeyValue{
		attribute.String("mcp.tool.name", toolName),
		attribute.String("mcp.operation", "tool_execution"),
	}
	attrs = append(defaultAttrs, attrs...)

	return t.tracer.Start(ctx, fmt.Sprintf("Tool: %s", toolName),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// TraceResourceOperation creates a span for resource operations
func (t *Tracer) TraceResourceOperation(ctx context.Context, operation, resourceType string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	defaultAttrs := []attribute.KeyValue{
		attribute.String("mcp.resource.operation", operation),
		attribute.String("mcp.resource.type", resourceType),
	}
	attrs = append(defaultAttrs, attrs...)

	return t.tracer.Start(ctx, fmt.Sprintf("Resource %s: %s", operation, resourceType),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// TracePromptOperation creates a span for prompt operations
func (t *Tracer) TracePromptOperation(ctx context.Context, operation string, attrs ...attribute.KeyValue) (context.Context, trace.Span) {
	defaultAttrs := []attribute.KeyValue{
		attribute.String("mcp.prompt.operation", operation),
	}
	attrs = append(defaultAttrs, attrs...)

	return t.tracer.Start(ctx, fmt.Sprintf("Prompt: %s", operation),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attrs...),
	)
}

// AddEvent adds an event to the current span
func AddEvent(ctx context.Context, name string, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.AddEvent(name, trace.WithAttributes(attrs...))
	}
}

// SetAttributes sets attributes on the current span
func SetAttributes(ctx context.Context, attrs ...attribute.KeyValue) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetAttributes(attrs...)
	}
}

// SetStatus sets the status of the current span
func SetStatus(ctx context.Context, code codes.Code, description string) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() {
		span.SetStatus(code, description)
	}
}

// RecordError records an error on the current span
func RecordError(ctx context.Context, err error, opts ...trace.EventOption) {
	span := trace.SpanFromContext(ctx)
	if span.IsRecording() && err != nil {
		span.RecordError(err, opts...)
	}
}

// WithSpan executes a function within a span
func (t *Tracer) WithSpan(ctx context.Context, spanName string, fn func(context.Context) error, opts ...trace.SpanStartOption) error {
	ctx, span := t.tracer.Start(ctx, spanName, opts...)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}

	return err
}

// WithRequestSpan executes a function within a request span
func (t *Tracer) WithRequestSpan(ctx context.Context, method string, fn func(context.Context) error) error {
	ctx, span := t.TraceRequest(ctx, method)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		SetAttributes(ctx, attribute.Bool("mcp.request.error", true))
	} else {
		span.SetStatus(codes.Ok, "")
		SetAttributes(ctx, attribute.Bool("mcp.request.success", true))
	}

	return err
}

// WithToolSpan executes a function within a tool execution span
func (t *Tracer) WithToolSpan(ctx context.Context, toolName string, fn func(context.Context) error) error {
	ctx, span := t.TraceToolExecution(ctx, toolName)
	defer span.End()

	err := fn(ctx)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		SetAttributes(ctx, attribute.Bool("mcp.tool.error", true))
	} else {
		span.SetStatus(codes.Ok, "")
		SetAttributes(ctx, attribute.Bool("mcp.tool.success", true))
	}

	return err
}

// Extract extracts trace context from a carrier
func Extract(ctx context.Context, carrier propagation.TextMapCarrier) context.Context {
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// Inject injects trace context into a carrier
func Inject(ctx context.Context, carrier propagation.TextMapCarrier) {
	otel.GetTextMapPropagator().Inject(ctx, carrier)
}

// HTTPCarrier adapts http.Header to propagation.TextMapCarrier
type HTTPCarrier http.Header

// Get returns the value associated with the passed key.
func (h HTTPCarrier) Get(key string) string {
	return http.Header(h).Get(key)
}

// Set stores the key-value pair.
func (h HTTPCarrier) Set(key string, value string) {
	http.Header(h).Set(key, value)
}

// Keys lists the keys stored in this carrier.
func (h HTTPCarrier) Keys() []string {
	keys := make([]string, 0, len(h))
	for key := range h {
		keys = append(keys, key)
	}
	return keys
}

// EnhancedSpanProcessor adds correlation ID and enrichment to spans
type EnhancedSpanProcessor struct {
	next sdktrace.SpanProcessor
}

// NewEnhancedSpanProcessor creates a new enhanced span processor
func NewEnhancedSpanProcessor(next sdktrace.SpanProcessor) *EnhancedSpanProcessor {
	return &EnhancedSpanProcessor{next: next}
}

// OnStart is called when a span is started
func (p *EnhancedSpanProcessor) OnStart(parent context.Context, s sdktrace.ReadWriteSpan) {
	// Add correlation ID from context
	if correlationID := protocol.GetCorrelationID(parent); correlationID != "" {
		s.SetAttributes(attribute.String("mcp.correlation_id", correlationID))
	}

	// Add runtime information
	s.SetAttributes(
		attribute.Int("runtime.goroutines", runtime.NumGoroutine()),
		attribute.String("runtime.version", runtime.Version()),
		attribute.String("runtime.arch", runtime.GOARCH),
		attribute.String("runtime.os", runtime.GOOS),
	)

	// Add timestamp
	s.SetAttributes(attribute.Int64("mcp.start_time", time.Now().Unix()))

	p.next.OnStart(parent, s)
}

// OnEnd is called when a span is ended
func (p *EnhancedSpanProcessor) OnEnd(s sdktrace.ReadOnlySpan) {
	p.next.OnEnd(s)
}

// Shutdown is called when the processor is shutdown
func (p *EnhancedSpanProcessor) Shutdown(ctx context.Context) error {
	return p.next.Shutdown(ctx)
}

// ForceFlush forces the processor to flush
func (p *EnhancedSpanProcessor) ForceFlush(ctx context.Context) error {
	return p.next.ForceFlush(ctx)
}

// TraceHTTPRequest creates a span for HTTP requests with correlation ID propagation
func (t *Tracer) TraceHTTPRequest(ctx context.Context, r *http.Request, w http.ResponseWriter) (context.Context, trace.Span) {
	// Extract correlation ID from headers
	correlationID := r.Header.Get("X-Correlation-ID")
	if correlationID == "" {
		correlationID = protocol.GenerateCorrelationID()
	}

	// Add correlation ID to context
	ctx = protocol.WithCorrelationID(ctx, correlationID)

	// Set correlation ID in response header
	w.Header().Set("X-Correlation-ID", correlationID)

	// Extract trace context from headers
	ctx = Extract(ctx, HTTPCarrier(r.Header))

	// Start span with HTTP attributes
	attrs := []attribute.KeyValue{
		semconv.HTTPMethod(r.Method),
		semconv.HTTPRoute(r.URL.Path),
		semconv.HTTPScheme(r.URL.Scheme),
		semconv.NetHostName(r.Host),
		semconv.UserAgentOriginal(r.UserAgent()),
		attribute.String("mcp.correlation_id", correlationID),
		attribute.String("http.request_id", correlationID),
	}

	// Add query parameters
	if r.URL.RawQuery != "" {
		attrs = append(attrs, attribute.String("http.query", r.URL.RawQuery))
	}

	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("%s %s", r.Method, r.URL.Path),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	return ctx, span
}

// TraceJSONRPCRequest creates a span for JSON-RPC requests with correlation ID
func (t *Tracer) TraceJSONRPCRequest(ctx context.Context, req *protocol.JSONRPCRequest) (context.Context, trace.Span) {
	// Ensure correlation ID exists
	correlationID := protocol.GetCorrelationID(ctx)
	if correlationID == "" {
		correlationID = protocol.GenerateCorrelationID()
		ctx = protocol.WithCorrelationID(ctx, correlationID)
	}

	attrs := []attribute.KeyValue{
		attribute.String("jsonrpc.version", "2.0"),
		attribute.String("jsonrpc.method", req.Method),
		attribute.String("mcp.correlation_id", correlationID),
	}

	// Add request ID if present
	if req.ID != nil {
		attrs = append(attrs, attribute.String("jsonrpc.id", fmt.Sprintf("%v", req.ID)))
	}

	ctx, span := t.tracer.Start(ctx, fmt.Sprintf("JSON-RPC %s", req.Method),
		trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attrs...),
	)

	return ctx, span
}

// TraceError enriches error spans with additional context
func (t *Tracer) TraceError(ctx context.Context, err error, operation string) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())

	// Add error details
	span.SetAttributes(
		attribute.String("error.type", fmt.Sprintf("%T", err)),
		attribute.String("error.operation", operation),
		attribute.Bool("mcp.error", true),
	)

	// Add correlation ID for error tracking
	if correlationID := protocol.GetCorrelationID(ctx); correlationID != "" {
		span.SetAttributes(attribute.String("error.correlation_id", correlationID))
	}
}

// TraceSuccess marks a span as successful with enriched context
func (t *Tracer) TraceSuccess(ctx context.Context, operation string, details map[string]interface{}) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}

	span.SetStatus(codes.Ok, "Success")
	span.SetAttributes(
		attribute.String("operation.status", "success"),
		attribute.String("operation.name", operation),
		attribute.Bool("mcp.success", true),
	)

	// Add custom details
	for key, value := range details {
		span.SetAttributes(attribute.String(fmt.Sprintf("operation.%s", key), fmt.Sprintf("%v", value)))
	}
}

// WithCorrelationPropagation wraps a function with correlation ID propagation
func (t *Tracer) WithCorrelationPropagation(ctx context.Context, fn func(context.Context) error) error {
	// Ensure correlation ID exists
	correlationID := protocol.GetCorrelationID(ctx)
	if correlationID == "" {
		correlationID = protocol.GenerateCorrelationID()
		ctx = protocol.WithCorrelationID(ctx, correlationID)
	}

	// Add correlation ID to span attributes
	SetAttributes(ctx, attribute.String("mcp.correlation_id", correlationID))

	return fn(ctx)
}

// CreateTracingMiddleware creates HTTP middleware for tracing
func (t *Tracer) CreateTracingMiddleware() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, span := t.TraceHTTPRequest(r.Context(), r, w)
			defer span.End()

			// Wrap response writer to capture status code
			wrapped := &responseWriter{ResponseWriter: w, statusCode: http.StatusOK}

			// Execute next handler
			next.ServeHTTP(wrapped, r.WithContext(ctx))

			// Set response attributes
			span.SetAttributes(
				semconv.HTTPStatusCode(wrapped.statusCode),
				attribute.Int("http.response.size", wrapped.bytesWritten),
			)

			if wrapped.statusCode >= 400 {
				span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", wrapped.statusCode))
			} else {
				span.SetStatus(codes.Ok, "")
			}
		})
	}
}

// responseWriter wraps http.ResponseWriter to capture response details
type responseWriter struct {
	http.ResponseWriter
	statusCode   int
	bytesWritten int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.statusCode = code
	rw.ResponseWriter.WriteHeader(code)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	rw.bytesWritten += len(b)
	return rw.ResponseWriter.Write(b)
}
