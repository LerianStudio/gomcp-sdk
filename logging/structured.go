package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"time"

	"go.opentelemetry.io/otel/trace"
)

// LogLevel represents logging levels
type LogLevel string

const (
	LevelDebug LogLevel = "debug"
	LevelInfo  LogLevel = "info"
	LevelWarn  LogLevel = "warn"
	LevelError LogLevel = "error"
)

// TimestampPrecision represents timestamp precision levels
type TimestampPrecision string

const (
	PrecisionSecond      TimestampPrecision = "second"
	PrecisionMillisecond TimestampPrecision = "millisecond"
	PrecisionMicrosecond TimestampPrecision = "microsecond"
	PrecisionNanosecond  TimestampPrecision = "nanosecond"
)

// LogFormat represents supported log formats
type LogFormat string

const (
	FormatJSON   LogFormat = "json"
	FormatText   LogFormat = "text"
	FormatCustom LogFormat = "custom"
)

// FormatterFunc defines custom log formatting function
type FormatterFunc func(ctx context.Context, record slog.Record) map[string]interface{}

// FilterFunc defines log filtering function
type FilterFunc func(ctx context.Context, record slog.Record) bool

// Config holds logger configuration
type Config struct {
	Level              LogLevel
	Format             string // "json", "text", "custom"
	AddSource          bool
	TimeFormat         string
	Output             io.Writer          // Custom output writer (default: os.Stdout)
	CustomFormatter    FormatterFunc      // Custom formatter for "custom" format
	Filters            []FilterFunc       // Log filters
	SamplingRate       float64            // Sampling rate for high-volume logs (0.0-1.0)
	AsyncLogging       bool               // Enable async logging for performance
	BufferSize         int                // Buffer size for async logging
	FieldMask          []string           // Fields to mask for security
	MaxRecordSize      int                // Maximum size of log record in bytes
	EnableCaller       bool               // Include caller information
	EnableStackTrace   bool               // Include stack trace for errors
	TimestampPrecision TimestampPrecision // Timestamp precision
	StructuredTags     map[string]string  // Additional structured tags
}

// DefaultConfig returns a default logging configuration
func DefaultConfig() Config {
	return Config{
		Level:              LevelInfo,
		Format:             string(FormatJSON),
		AddSource:          false,
		TimeFormat:         time.RFC3339,
		Output:             os.Stdout,
		SamplingRate:       1.0,
		AsyncLogging:       false,
		BufferSize:         1000,
		MaxRecordSize:      10240, // 10KB
		EnableCaller:       true,
		EnableStackTrace:   false,
		TimestampPrecision: PrecisionMillisecond,
		StructuredTags:     make(map[string]string),
	}
}

// DevelopmentConfig returns a development-friendly logging configuration
func DevelopmentConfig() Config {
	config := DefaultConfig()
	config.Level = LevelDebug
	config.Format = string(FormatText)
	config.AddSource = true
	config.EnableStackTrace = true
	config.TimestampPrecision = PrecisionMicrosecond
	return config
}

// ProductionConfig returns a production-ready logging configuration
func ProductionConfig() Config {
	config := DefaultConfig()
	config.Level = LevelInfo
	config.Format = string(FormatJSON)
	config.AddSource = false
	config.AsyncLogging = true
	config.SamplingRate = 0.1 // Sample 10% of high-volume logs
	config.EnableStackTrace = false
	config.TimestampPrecision = PrecisionMillisecond
	config.FieldMask = []string{"password", "token", "secret", "key", "authorization"}
	return config
}

// Logger wraps slog.Logger with MCP-specific methods
type Logger struct {
	*slog.Logger
	config Config
}

// NewLogger creates a new structured logger
func NewLogger(config Config) *Logger {
	// Set defaults if not provided
	if config.Output == nil {
		config.Output = os.Stdout
	}
	if config.BufferSize == 0 {
		config.BufferSize = 1000
	}
	if config.MaxRecordSize == 0 {
		config.MaxRecordSize = 10240
	}
	if config.StructuredTags == nil {
		config.StructuredTags = make(map[string]string)
	}

	var handler slog.Handler

	opts := &slog.HandlerOptions{
		Level:     parseLevel(config.Level),
		AddSource: config.AddSource || config.EnableCaller,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			// Custom time format and precision
			if a.Key == slog.TimeKey && config.TimeFormat != "" {
				if t, ok := a.Value.Any().(time.Time); ok {
					formatted := formatTimestamp(t, config.TimestampPrecision, config.TimeFormat)
					a.Value = slog.StringValue(formatted)
				}
			}

			// Field masking for security
			if len(config.FieldMask) > 0 {
				for _, mask := range config.FieldMask {
					if strings.Contains(strings.ToLower(a.Key), strings.ToLower(mask)) {
						a.Value = slog.StringValue("***MASKED***")
					}
				}
			}

			return a
		},
	}

	switch config.Format {
	case string(FormatJSON):
		handler = slog.NewJSONHandler(config.Output, opts)
	case string(FormatCustom):
		if config.CustomFormatter != nil {
			handler = NewCustomHandler(config.Output, opts, config.CustomFormatter)
		} else {
			handler = slog.NewJSONHandler(config.Output, opts)
		}
	default:
		handler = slog.NewTextHandler(config.Output, opts)
	}

	// Wrap with filtering handler if filters are provided
	if len(config.Filters) > 0 {
		handler = NewFilteringHandler(handler, config.Filters)
	}

	// Wrap with sampling handler if sampling rate < 1.0
	if config.SamplingRate < 1.0 && config.SamplingRate > 0 {
		handler = NewSamplingHandler(handler, config.SamplingRate)
	}

	// Create logger with structured tags
	logger := slog.New(handler)
	if len(config.StructuredTags) > 0 {
		attrs := make([]any, 0, len(config.StructuredTags)*2)
		for k, v := range config.StructuredTags {
			attrs = append(attrs, k, v)
		}
		logger = logger.With(attrs...)
	}

	return &Logger{
		Logger: logger,
		config: config,
	}
}

// WithContext returns a logger with context values
func (l *Logger) WithContext(ctx context.Context) *Logger {
	logger := l.Logger

	// Add trace information if available
	if span := trace.SpanFromContext(ctx); span.SpanContext().IsValid() {
		logger = logger.With(
			slog.String("trace_id", span.SpanContext().TraceID().String()),
			slog.String("span_id", span.SpanContext().SpanID().String()),
		)
	}

	return &Logger{
		Logger: logger,
		config: l.config,
	}
}

// WithFields returns a logger with additional fields
func (l *Logger) WithFields(fields map[string]interface{}) *Logger {
	attrs := make([]any, 0, len(fields)*2)
	for k, v := range fields {
		attrs = append(attrs, k, v)
	}

	return &Logger{
		Logger: l.With(attrs...),
		config: l.config,
	}
}

// WithError returns a logger with error field
func (l *Logger) WithError(err error) *Logger {
	return &Logger{
		Logger: l.With(slog.String("error", err.Error())),
		config: l.config,
	}
}

// Request logs MCP request information
func (l *Logger) Request(ctx context.Context, method string, id interface{}, params interface{}) {
	logger := l.WithContext(ctx)
	logger.Info("MCP request received",
		slog.String("method", method),
		slog.Any("id", id),
		slog.Any("params", params),
		slog.String("component", "mcp.server"),
	)
}

// Response logs MCP response information
func (l *Logger) Response(ctx context.Context, method string, id interface{}, result interface{}, duration time.Duration) {
	logger := l.WithContext(ctx)
	logger.Info("MCP response sent",
		slog.String("method", method),
		slog.Any("id", id),
		slog.Duration("duration", duration),
		slog.String("component", "mcp.server"),
	)
}

// ResponseError logs MCP error response
func (l *Logger) ResponseError(ctx context.Context, method string, id interface{}, err error, duration time.Duration) {
	logger := l.WithContext(ctx)
	logger.Error("MCP error response",
		slog.String("method", method),
		slog.Any("id", id),
		slog.String("error", err.Error()),
		slog.Duration("duration", duration),
		slog.String("component", "mcp.server"),
	)
}

// ToolExecution logs tool execution
func (l *Logger) ToolExecution(ctx context.Context, toolName string, args interface{}) {
	logger := l.WithContext(ctx)
	logger.Info("Tool execution started",
		slog.String("tool", toolName),
		slog.Any("args", args),
		slog.String("component", "mcp.tools"),
	)
}

// ToolResult logs tool execution result
func (l *Logger) ToolResult(ctx context.Context, toolName string, result interface{}, duration time.Duration) {
	logger := l.WithContext(ctx)
	logger.Info("Tool execution completed",
		slog.String("tool", toolName),
		slog.Duration("duration", duration),
		slog.String("component", "mcp.tools"),
	)
}

// ToolError logs tool execution error
func (l *Logger) ToolError(ctx context.Context, toolName string, err error, duration time.Duration) {
	logger := l.WithContext(ctx)
	logger.Error("Tool execution failed",
		slog.String("tool", toolName),
		slog.String("error", err.Error()),
		slog.Duration("duration", duration),
		slog.String("component", "mcp.tools"),
	)
}

// ResourceOperation logs resource operations
func (l *Logger) ResourceOperation(ctx context.Context, operation, resourceType, uri string) {
	logger := l.WithContext(ctx)
	logger.Info("Resource operation",
		slog.String("operation", operation),
		slog.String("resource_type", resourceType),
		slog.String("uri", uri),
		slog.String("component", "mcp.resources"),
	)
}

// PromptOperation logs prompt operations
func (l *Logger) PromptOperation(ctx context.Context, operation, promptName string) {
	logger := l.WithContext(ctx)
	logger.Info("Prompt operation",
		slog.String("operation", operation),
		slog.String("prompt", promptName),
		slog.String("component", "mcp.prompts"),
	)
}

// WebSocketConnection logs WebSocket connection events
func (l *Logger) WebSocketConnection(event string, remoteAddr string) {
	l.Info("WebSocket connection event",
		slog.String("event", event),
		slog.String("remote_addr", remoteAddr),
		slog.String("component", "mcp.transport.websocket"),
	)
}

// parseLevel converts LogLevel to slog.Level
func parseLevel(level LogLevel) slog.Level {
	switch level {
	case LevelDebug:
		return slog.LevelDebug
	case LevelInfo:
		return slog.LevelInfo
	case LevelWarn:
		return slog.LevelWarn
	case LevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

// Middleware provides logging middleware functionality
type Middleware struct {
	logger *Logger
}

// NewMiddleware creates a new logging middleware
func NewMiddleware(logger *Logger) *Middleware {
	return &Middleware{logger: logger}
}

// LogRequest logs incoming requests with recovery
func (m *Middleware) LogRequest(ctx context.Context, method string, id interface{}, fn func() (interface{}, error)) (result interface{}, err error) {
	start := time.Now()

	// Log request
	m.logger.Request(ctx, method, id, nil)

	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered: %v", r)

			// Log stack trace
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			m.logger.WithContext(ctx).Error("Panic in request handler",
				slog.String("method", method),
				slog.Any("panic", r),
				slog.String("stack", string(buf[:n])),
			)
		}
	}()

	// Execute function
	result, err = fn()

	// Log response
	duration := time.Since(start)
	if err != nil {
		m.logger.ResponseError(ctx, method, id, err, duration)
	} else {
		m.logger.Response(ctx, method, id, result, duration)
	}

	return result, err
}

// LogTool logs tool execution with recovery
func (m *Middleware) LogTool(ctx context.Context, toolName string, args interface{}, fn func() (interface{}, error)) (result interface{}, err error) {
	start := time.Now()

	// Log execution start
	m.logger.ToolExecution(ctx, toolName, args)

	// Recover from panics
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic recovered: %v", r)

			// Log stack trace
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			m.logger.WithContext(ctx).Error("Panic in tool execution",
				slog.String("tool", toolName),
				slog.Any("panic", r),
				slog.String("stack", string(buf[:n])),
			)
		}
	}()

	// Execute function
	result, err = fn()

	// Log result
	duration := time.Since(start)
	if err != nil {
		m.logger.ToolError(ctx, toolName, err, duration)
	} else {
		m.logger.ToolResult(ctx, toolName, result, duration)
	}

	return result, err
}

// Global logger instance
var defaultLogger = NewLogger(Config{
	Level:  LevelInfo,
	Format: "json",
})

// SetDefault sets the default logger
func SetDefault(logger *Logger) {
	defaultLogger = logger
}

// Default returns the default logger
func Default() *Logger {
	return defaultLogger
}

// formatTimestamp formats timestamp based on precision
func formatTimestamp(t time.Time, precision TimestampPrecision, format string) string {
	switch precision {
	case PrecisionSecond:
		return t.Truncate(time.Second).Format(format)
	case PrecisionMillisecond:
		return t.Truncate(time.Millisecond).Format(format)
	case PrecisionMicrosecond:
		return t.Truncate(time.Microsecond).Format(format)
	case PrecisionNanosecond:
		return t.Format(format)
	default:
		return t.Format(format)
	}
}

// CustomHandler implements a custom slog.Handler
type CustomHandler struct {
	output    io.Writer
	opts      *slog.HandlerOptions
	formatter FormatterFunc
}

// NewCustomHandler creates a new custom handler
func NewCustomHandler(output io.Writer, opts *slog.HandlerOptions, formatter FormatterFunc) *CustomHandler {
	return &CustomHandler{
		output:    output,
		opts:      opts,
		formatter: formatter,
	}
}

func (h *CustomHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *CustomHandler) Handle(ctx context.Context, record slog.Record) error {
	if h.formatter != nil {
		formatted := h.formatter(ctx, record)
		// Write formatted output
		_, err := fmt.Fprintln(h.output, formatOutput(formatted))
		return err
	}
	return nil
}

func (h *CustomHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	// Return a new handler with attributes
	return h
}

func (h *CustomHandler) WithGroup(name string) slog.Handler {
	// Return a new handler with group
	return h
}

// FilteringHandler wraps a handler with filtering capability
type FilteringHandler struct {
	handler slog.Handler
	filters []FilterFunc
}

// NewFilteringHandler creates a new filtering handler
func NewFilteringHandler(handler slog.Handler, filters []FilterFunc) *FilteringHandler {
	return &FilteringHandler{
		handler: handler,
		filters: filters,
	}
}

func (h *FilteringHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *FilteringHandler) Handle(ctx context.Context, record slog.Record) error {
	// Apply filters
	for _, filter := range h.filters {
		if !filter(ctx, record) {
			return nil // Skip this record
		}
	}
	return h.handler.Handle(ctx, record)
}

func (h *FilteringHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &FilteringHandler{
		handler: h.handler.WithAttrs(attrs),
		filters: h.filters,
	}
}

func (h *FilteringHandler) WithGroup(name string) slog.Handler {
	return &FilteringHandler{
		handler: h.handler.WithGroup(name),
		filters: h.filters,
	}
}

// SamplingHandler implements log sampling
type SamplingHandler struct {
	handler slog.Handler
	rate    float64
	counter uint64
}

// NewSamplingHandler creates a new sampling handler
func NewSamplingHandler(handler slog.Handler, rate float64) *SamplingHandler {
	return &SamplingHandler{
		handler: handler,
		rate:    rate,
	}
}

func (h *SamplingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.handler.Enabled(ctx, level)
}

func (h *SamplingHandler) Handle(ctx context.Context, record slog.Record) error {
	// Simple sampling based on rate
	h.counter++
	if float64(h.counter%100) < h.rate*100 {
		return h.handler.Handle(ctx, record)
	}
	return nil
}

func (h *SamplingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &SamplingHandler{
		handler: h.handler.WithAttrs(attrs),
		rate:    h.rate,
		counter: h.counter,
	}
}

func (h *SamplingHandler) WithGroup(name string) slog.Handler {
	return &SamplingHandler{
		handler: h.handler.WithGroup(name),
		rate:    h.rate,
		counter: h.counter,
	}
}

// formatOutput formats map to string
func formatOutput(data map[string]interface{}) string {
	// Simple key=value format
	var parts []string
	for k, v := range data {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, " ")
}

// Common filter functions
func NoStackTraceFilter() FilterFunc {
	return func(ctx context.Context, record slog.Record) bool {
		// Skip if contains stack trace
		hasStack := false
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "stack" {
				hasStack = true
				return false
			}
			return true
		})
		return !hasStack
	}
}

func ComponentFilter(allowedComponents ...string) FilterFunc {
	allowed := make(map[string]bool)
	for _, comp := range allowedComponents {
		allowed[comp] = true
	}

	return func(ctx context.Context, record slog.Record) bool {
		component := ""
		record.Attrs(func(attr slog.Attr) bool {
			if attr.Key == "component" {
				component = attr.Value.String()
				return false
			}
			return true
		})

		if component == "" {
			return true // Allow if no component specified
		}

		return allowed[component]
	}
}

// WithCorrelationID adds correlation ID to logger
func (l *Logger) WithCorrelationID(correlationID string) *Logger {
	return &Logger{
		Logger: l.With(slog.String("correlation_id", correlationID)),
		config: l.config,
	}
}

// WithComponent adds component tag to logger
func (l *Logger) WithComponent(component string) *Logger {
	return &Logger{
		Logger: l.With(slog.String("component", component)),
		config: l.config,
	}
}

// WithDuration adds duration to logger
func (l *Logger) WithDuration(duration time.Duration) *Logger {
	return &Logger{
		Logger: l.With(slog.Duration("duration", duration)),
		config: l.config,
	}
}
