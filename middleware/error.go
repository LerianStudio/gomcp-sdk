package middleware

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
)

// ErrorHandlingConfig configures error handling behavior
type ErrorHandlingConfig struct {
	// Include stack traces in error responses (development only)
	IncludeStackTrace bool

	// Include debug information in error responses
	IncludeDebugInfo bool

	// Log errors to console
	LogErrors bool

	// Custom error transformation function
	ErrorTransformer func(error) *protocol.StandardError
}

// ErrorHandlingMiddleware provides centralized error handling across all transports
type ErrorHandlingMiddleware struct {
	config *ErrorHandlingConfig
}

// NewErrorHandlingMiddleware creates a new error handling middleware
func NewErrorHandlingMiddleware(config *ErrorHandlingConfig) *ErrorHandlingMiddleware {
	if config == nil {
		config = &ErrorHandlingConfig{
			IncludeStackTrace: false,
			IncludeDebugInfo:  false,
			LogErrors:         true,
		}
	}

	return &ErrorHandlingMiddleware{
		config: config,
	}
}

// WrapHTTPHandler wraps an HTTP handler with error handling
func (m *ErrorHandlingMiddleware) WrapHTTPHandler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Create error recovery wrapper
		defer func() {
			if recovered := recover(); recovered != nil {
				var err error
				switch v := recovered.(type) {
				case error:
					err = v
				case string:
					err = fmt.Errorf(recovered.(string))
				default:
					err = fmt.Errorf("unexpected panic: %v", recovered)
				}

				m.handleHTTPError(w, r, err, http.StatusInternalServerError)
			}
		}()

		// Create response writer wrapper to capture errors
		wrapper := &errorCapturingWriter{
			ResponseWriter: w,
			middleware:     m,
			request:        r,
		}

		next.ServeHTTP(wrapper, r)
	})
}

// WrapJSONRPCHandler wraps a JSON-RPC handler with error handling
func (m *ErrorHandlingMiddleware) WrapJSONRPCHandler(next func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse) func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	return func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
		defer func() {
			if recovered := recover(); recovered != nil {
				var err error
				switch v := recovered.(type) {
				case error:
					err = v
				case string:
					err = fmt.Errorf(recovered.(string))
				default:
					err = fmt.Errorf("unexpected panic: %v", recovered)
				}

				if m.config.LogErrors {
					fmt.Printf("[ERROR] Panic in JSON-RPC handler: %v\n", err)
					if m.config.IncludeStackTrace {
						buf := make([]byte, 4096)
						n := runtime.Stack(buf, false)
						fmt.Printf("[STACK] %s\n", buf[:n])
					}
				}
			}
		}()

		resp := next(ctx, req)

		// Transform errors if needed
		if resp.Error != nil && m.config.ErrorTransformer != nil {
			if standardErr := m.config.ErrorTransformer(resp.Error); standardErr != nil {
				resp.Error = standardErr.ToJSONRPCError()
			}
		}

		// Log errors
		if resp.Error != nil && m.config.LogErrors {
			correlationID := protocol.GetCorrelationID(ctx)
			fmt.Printf("[ERROR] JSON-RPC error [%s]: %s (code: %d)\n",
				correlationID, resp.Error.Message, resp.Error.Code)
		}

		return resp
	}
}

// handleHTTPError handles HTTP errors with standardized format
func (m *ErrorHandlingMiddleware) handleHTTPError(w http.ResponseWriter, r *http.Request, err error, statusCode int) {
	// Get correlation ID from context or generate one
	ctx := r.Context()
	correlationID := protocol.GetCorrelationID(ctx)
	if correlationID == "" {
		correlationID = protocol.GenerateCorrelationID()
		ctx = protocol.WithCorrelationID(ctx, correlationID)
	}

	// Create standardized error
	var standardErr *protocol.StandardError
	if m.config.ErrorTransformer != nil {
		standardErr = m.config.ErrorTransformer(err)
	}

	if standardErr == nil {
		// Create default standard error
		code := m.httpStatusToJSONRPCCode(statusCode)
		standardErr = protocol.NewStandardError(ctx, code, err.Error(), nil)
	}

	// Add debug information if enabled
	if m.config.IncludeDebugInfo {
		standardErr.Details["request_method"] = r.Method
		standardErr.Details["request_path"] = r.URL.Path
		standardErr.Details["user_agent"] = r.UserAgent()
		standardErr.Details["remote_addr"] = r.RemoteAddr
	}

	// Add stack trace if enabled
	if m.config.IncludeStackTrace {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		standardErr.Stack = string(buf[:n])
	}

	// Log error
	if m.config.LogErrors {
		fmt.Printf("[ERROR] HTTP error [%s]: %s (status: %d)\n",
			correlationID, err.Error(), statusCode)
		if m.config.IncludeStackTrace {
			fmt.Printf("[STACK] %s\n", standardErr.Stack)
		}
	}

	// Set response headers
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("X-Correlation-ID", correlationID)
	w.WriteHeader(statusCode)

	// Write error response
	errorResponse := map[string]interface{}{
		"error": map[string]interface{}{
			"code":          standardErr.Code,
			"message":       standardErr.Message,
			"correlationId": standardErr.CorrelationID,
			"timestamp":     standardErr.Timestamp,
		},
	}

	// Add optional fields
	if standardErr.Data != nil {
		errorResponse["error"].(map[string]interface{})["data"] = standardErr.Data
	}

	if len(standardErr.Details) > 0 {
		errorResponse["error"].(map[string]interface{})["details"] = standardErr.Details
	}

	if standardErr.Stack != "" && m.config.IncludeStackTrace {
		errorResponse["error"].(map[string]interface{})["stack"] = standardErr.Stack
	}

	if err := json.NewEncoder(w).Encode(errorResponse); err != nil {
		fmt.Printf("[ERROR] Failed to encode error response: %v\n", err)
	}
}

// httpStatusToJSONRPCCode converts HTTP status codes to JSON-RPC error codes
func (m *ErrorHandlingMiddleware) httpStatusToJSONRPCCode(statusCode int) int {
	switch statusCode {
	case http.StatusBadRequest:
		return protocol.InvalidRequest
	case http.StatusNotFound:
		return protocol.MethodNotFound
	case http.StatusUnprocessableEntity:
		return protocol.InvalidParams
	case http.StatusInternalServerError:
		return protocol.InternalError
	default:
		return protocol.InternalError
	}
}

// errorCapturingWriter wraps http.ResponseWriter to capture and handle errors
type errorCapturingWriter struct {
	http.ResponseWriter
	middleware *ErrorHandlingMiddleware
	request    *http.Request
	written    bool
}

func (w *errorCapturingWriter) WriteHeader(statusCode int) {
	if w.written {
		return
	}
	w.written = true

	// If it's an error status code, handle it
	if statusCode >= 400 {
		err := fmt.Errorf("HTTP %d error", statusCode)
		w.middleware.handleHTTPError(w.ResponseWriter, w.request, err, statusCode)
		return
	}

	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *errorCapturingWriter) Write(data []byte) (int, error) {
	if !w.written {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(data)
}

// Common error transformers

// DefaultErrorTransformer provides sensible defaults for error transformation
func DefaultErrorTransformer(err error) *protocol.StandardError {
	ctx := context.Background()

	switch e := err.(type) {
	case *protocol.JSONRPCError:
		return protocol.NewStandardErrorFromJSONRPC(ctx, e)
	case *protocol.StandardError:
		return e
	default:
		return protocol.NewStandardError(ctx, protocol.InternalError, err.Error(), nil)
	}
}

// DevelopmentErrorTransformer includes more debug information for development
func DevelopmentErrorTransformer(err error) *protocol.StandardError {
	standardErr := DefaultErrorTransformer(err)

	// Add more debug information in development
	standardErr.Details["error_type"] = fmt.Sprintf("%T", err)
	standardErr.Details["timestamp_iso"] = time.Unix(standardErr.Timestamp, 0).Format(time.RFC3339)

	return standardErr
}

// ProductionErrorTransformer sanitizes errors for production
func ProductionErrorTransformer(err error) *protocol.StandardError {
	ctx := context.Background()

	switch e := err.(type) {
	case *protocol.JSONRPCError:
		// Keep JSON-RPC errors as they are part of the protocol
		return protocol.NewStandardErrorFromJSONRPC(ctx, e)
	case *protocol.StandardError:
		// Keep standard errors but sanitize sensitive information
		sanitized := *e
		sanitized.Stack = "" // Remove stack traces in production
		return &sanitized
	default:
		// Generic error message for production
		return protocol.NewStandardError(ctx, protocol.InternalError, "An internal error occurred", nil)
	}
}
