# GoMCP SDK - API Reference

This document provides comprehensive API reference for the GoMCP SDK, including all enhanced features and their usage.

## Table of Contents

1. [Error Handling Middleware](#error-handling-middleware)
2. [Connection Pooling](#connection-pooling)
3. [API Versioning](#api-versioning)
4. [Transport Layers](#transport-layers)
5. [Contract Testing](#contract-testing)

## Error Handling Middleware

### Package: `middleware`

#### Types

##### `ErrorHandlingConfig`

Configuration for error handling behavior.

```go
type ErrorHandlingConfig struct {
    IncludeStackTrace bool                                      // Include stack traces (development only)
    IncludeDebugInfo  bool                                      // Include debug information
    LogErrors         bool                                      // Log errors to console
    ErrorTransformer  func(error) *protocol.StandardError      // Custom error transformation
}
```

##### `ErrorHandlingMiddleware`

Provides centralized error handling across all transports.

```go
type ErrorHandlingMiddleware struct {
    // Private fields
}
```

#### Functions

##### `NewErrorHandlingMiddleware`

```go
func NewErrorHandlingMiddleware(config *ErrorHandlingConfig) *ErrorHandlingMiddleware
```

Creates a new error handling middleware with the specified configuration. If config is nil, uses production-safe defaults.

**Parameters:**
- `config`: Error handling configuration (optional)

**Returns:**
- `*ErrorHandlingMiddleware`: Configured middleware instance

**Example:**
```go
config := &middleware.ErrorHandlingConfig{
    IncludeStackTrace: false,
    IncludeDebugInfo:  false,
    LogErrors:        true,
    ErrorTransformer: middleware.ProductionErrorTransformer,
}
errorHandler := middleware.NewErrorHandlingMiddleware(config)
```

##### `WrapHTTPHandler`

```go
func (m *ErrorHandlingMiddleware) WrapHTTPHandler(next http.Handler) http.Handler
```

Wraps an HTTP handler with error handling middleware.

**Parameters:**
- `next`: The HTTP handler to wrap

**Returns:**
- `http.Handler`: Wrapped handler with error handling

**Example:**
```go
handler := errorHandler.WrapHTTPHandler(mux)
```

##### `WrapJSONRPCHandler`

```go
func (m *ErrorHandlingMiddleware) WrapJSONRPCHandler(
    next func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse
) func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse
```

Wraps a JSON-RPC handler with error handling middleware.

**Parameters:**
- `next`: The JSON-RPC handler function to wrap

**Returns:**
- JSON-RPC handler function with error handling

**Example:**
```go
wrappedHandler := errorHandler.WrapJSONRPCHandler(originalHandler)
```

#### Error Transformers

##### `DefaultErrorTransformer`

```go
func DefaultErrorTransformer(err error) *protocol.StandardError
```

Provides sensible defaults for error transformation.

##### `DevelopmentErrorTransformer`

```go
func DevelopmentErrorTransformer(err error) *protocol.StandardError
```

Includes additional debug information for development environments.

##### `ProductionErrorTransformer`

```go
func ProductionErrorTransformer(err error) *protocol.StandardError
```

Sanitizes errors for production environments by removing sensitive information.

## Connection Pooling

### Package: `transport`

#### Types

##### `ConnectionPoolConfig`

Configuration for HTTP connection pooling.

```go
type ConnectionPoolConfig struct {
    MaxIdleConns          int           // Maximum idle connections
    MaxIdleConnsPerHost   int           // Maximum idle connections per host
    MaxConnsPerHost       int           // Maximum connections per host
    IdleConnTimeout       time.Duration // Keep-alive timeout for idle connections
    TLSHandshakeTimeout   time.Duration // TLS handshake timeout
    ResponseHeaderTimeout time.Duration // Response header timeout
    ExpectContinueTimeout time.Duration // Expect continue timeout
}
```

#### Methods

##### `ConnectionPoolStats`

```go
func (t *HTTPTransport) ConnectionPoolStats() map[string]interface{}
```

Returns connection pool statistics and configuration.

**Returns:**
- `map[string]interface{}`: Pool statistics including enabled status and configuration

**Example:**
```go
stats := httpTransport.ConnectionPoolStats()
fmt.Printf("Pool enabled: %v\n", stats["enabled"])
fmt.Printf("Max idle connections: %v\n", stats["max_idle_conns"])
```

##### `GetConnectionPool`

```go
func (t *HTTPTransport) GetConnectionPool() *http.Transport
```

Returns the underlying HTTP transport for advanced configuration.

**Returns:**
- `*http.Transport`: The configured HTTP transport

## API Versioning

### Package: `versioning`

#### Types

##### `APIVersion`

Represents an API version using semantic versioning.

```go
type APIVersion struct {
    Major int
    Minor int
    Patch int
}
```

**Methods:**

```go
func (v APIVersion) String() string                         // Returns "v1.0.0"
func (v APIVersion) Short() string                          // Returns "v1.0"
func (v APIVersion) IsCompatibleWith(other APIVersion) bool // Checks compatibility
func (v APIVersion) Compare(other APIVersion) int           // Compares versions (-1, 0, 1)
```

##### `VersioningStrategy`

Defines how API versioning is handled.

```go
type VersioningStrategy string

const (
    VersioningStrategyHeader      VersioningStrategy = "header"       // API-Version header
    VersioningStrategyPath        VersioningStrategy = "path"         // URL path (/v1/tools)
    VersioningStrategyQuery       VersioningStrategy = "query"        // Query parameter (?version=v1.0)
    VersioningStrategyContentType VersioningStrategy = "content-type" // Content-Type versioning
)
```

##### `VersioningConfig`

Configuration for API versioning behavior.

```go
type VersioningConfig struct {
    Strategy               VersioningStrategy // Versioning strategy
    DefaultVersion         APIVersion         // Default version when none specified
    SupportedVersions      []APIVersion       // List of supported versions
    DeprecatedVersions     []APIVersion       // List of deprecated versions
    HeaderName             string             // Header name for header-based versioning
    QueryParam             string             // Query parameter name
    ContentTypeVendor      string             // Content-Type vendor prefix
    IncludeVersionHeaders  bool               // Include version info in responses
    StrictVersioning       bool               // Enforce strict versioning
}
```

##### `VersionManager`

Handles API versioning operations.

```go
type VersionManager struct {
    // Private fields
}
```

#### Functions

##### `NewVersionManager`

```go
func NewVersionManager(config *VersioningConfig) *VersionManager
```

Creates a new version manager with the specified configuration.

**Parameters:**
- `config`: Versioning configuration (uses defaults if nil)

**Returns:**
- `*VersionManager`: Configured version manager

**Example:**
```go
config := &versioning.VersioningConfig{
    Strategy:       versioning.VersioningStrategyHeader,
    DefaultVersion: versioning.APIVersion{Major: 1, Minor: 0, Patch: 0},
    SupportedVersions: []versioning.APIVersion{
        {Major: 1, Minor: 0, Patch: 0},
        {Major: 1, Minor: 1, Patch: 0},
    },
}
manager := versioning.NewVersionManager(config)
```

##### `ExtractVersion`

```go
func (vm *VersionManager) ExtractVersion(r *http.Request) (APIVersion, error)
```

Extracts the API version from an HTTP request based on the configured strategy.

**Parameters:**
- `r`: HTTP request

**Returns:**
- `APIVersion`: Extracted version
- `error`: Error if version extraction fails

##### `ValidateVersion`

```go
func (vm *VersionManager) ValidateVersion(version APIVersion) error
```

Validates if a version is supported.

**Parameters:**
- `version`: Version to validate

**Returns:**
- `error`: Error if version is not supported (nil if valid)

##### `CreateVersioningMiddleware`

```go
func (vm *VersionManager) CreateVersioningMiddleware() func(http.Handler) http.Handler
```

Creates HTTP middleware for version handling.

**Returns:**
- Middleware function that handles version extraction and validation

**Example:**
```go
versionMiddleware := versionManager.CreateVersioningMiddleware()
handler = versionMiddleware(handler)
```

#### Version Routing

##### `VersionRouter`

Routes requests to version-specific handlers.

```go
type VersionRouter struct {
    // Private fields
}
```

##### `NewVersionRouter`

```go
func NewVersionRouter(versionManager *VersionManager) *VersionRouter
```

Creates a new version router.

**Parameters:**
- `versionManager`: Version manager for version handling

**Returns:**
- `*VersionRouter`: New version router

##### `AddHandler`

```go
func (vr *VersionRouter) AddHandler(version APIVersion, handler http.Handler)
```

Adds a version-specific handler.

**Parameters:**
- `version`: API version for this handler
- `handler`: HTTP handler for this version

**Example:**
```go
router.AddHandler(versioning.APIVersion{Major: 1, Minor: 0}, handlerV1_0)
router.AddHandler(versioning.APIVersion{Major: 1, Minor: 1}, handlerV1_1)
```

##### `SetDefaultHandler`

```go
func (vr *VersionRouter) SetDefaultHandler(handler http.Handler)
```

Sets the default handler for unmatched versions.

**Parameters:**
- `handler`: Default HTTP handler

#### Utility Functions

##### `GetVersionFromContext`

```go
func GetVersionFromContext(ctx context.Context) (APIVersion, bool)
```

Extracts API version from request context.

**Parameters:**
- `ctx`: Request context

**Returns:**
- `APIVersion`: Extracted version
- `bool`: True if version was found in context

## Transport Layers

### HTTP Transport Enhancements

#### `ServeHTTP` Method

```go
func (t *RESTTransport) ServeHTTP(w http.ResponseWriter, r *http.Request, handler RequestHandler)
```

Handles direct HTTP requests without starting a server, allowing REST transport to be used as an `http.Handler`.

**Parameters:**
- `w`: HTTP response writer
- `r`: HTTP request
- `handler`: Request handler for processing

**Example:**
```go
restTransport := transport.NewRESTTransport(&transport.RESTConfig{
    APIPrefix:  "/api/v1",
    EnableDocs: true,
})

// Use in HTTP server
http.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
    restTransport.ServeHTTP(w, r, serverHandler)
})
```

### WebSocket Transport

Enhanced WebSocket transport with comprehensive contract testing support.

#### Configuration

```go
type WebSocketConfig struct {
    Address           string        // Listen address
    Path              string        // WebSocket path (default: "/ws")
    MaxMessageSize    int64         // Maximum message size
    PingInterval      time.Duration // Ping interval for heartbeat
    PongTimeout       time.Duration // Pong timeout
    EnableCompression bool          // Enable message compression
    CheckOrigin       func(*http.Request) bool // Origin validation function
}
```

## Contract Testing

### Package: `testing`

#### Types

##### `ContractTestSuite`

Comprehensive test suite for MCP protocol compliance.

```go
type ContractTestSuite struct {
    // Private fields
}
```

#### Functions

##### `NewContractTestSuite`

```go
func NewContractTestSuite(t *testing.T) *ContractTestSuite
```

Creates a new contract test suite with sample tools for testing.

**Parameters:**
- `t`: Testing instance

**Returns:**
- `*ContractTestSuite`: Configured test suite

##### `SetupHTTP`

```go
func (s *ContractTestSuite) SetupHTTP()
```

Sets up HTTP transport for testing.

##### `SetupWebSocket`

```go
func (s *ContractTestSuite) SetupWebSocket()
```

Sets up WebSocket transport for testing with comprehensive configuration.

##### `TearDown`

```go
func (s *ContractTestSuite) TearDown()
```

Cleans up test resources including HTTP and WebSocket connections.

#### Test Methods

##### HTTP Contract Tests

```go
func (s *ContractTestSuite) TestHTTPContractCompliance(t *testing.T)
```

Tests HTTP REST API contract compliance including:
- Tool listing and execution
- Server info and health endpoints
- Error response formats
- Correlation ID handling

##### WebSocket Contract Tests

```go
func (s *ContractTestSuite) TestWebSocketContractCompliance(t *testing.T)
```

Tests WebSocket contract compliance including:
- Connection establishment
- JSON-RPC over WebSocket
- Tool execution via WebSocket
- Error handling
- Ping/Pong heartbeat

##### JSON-RPC Compliance Tests

```go
func (s *ContractTestSuite) TestJSONRPCCompliance(t *testing.T)
```

Tests JSON-RPC 2.0 protocol compliance including:
- Request ID handling
- Error code compliance
- Protocol version validation

#### Running Tests

```bash
# Run all contract tests
go test -v ./testing/...

# Run specific test categories
go test -v -run TestHTTPContractCompliance ./testing/...
go test -v -run TestWebSocketContractCompliance ./testing/...
go test -v -run TestJSONRPCCompliance ./testing/...
```

## Error Codes and Status

### HTTP Status Code Mapping

| JSON-RPC Error Code | HTTP Status Code | Description |
|-------------------|------------------|-------------|
| -32700 (Parse Error) | 400 Bad Request | Invalid JSON |
| -32600 (Invalid Request) | 400 Bad Request | Invalid request format |
| -32601 (Method Not Found) | 404 Not Found | Unknown method |
| -32602 (Invalid Params) | 422 Unprocessable Entity | Invalid parameters |
| -32603 (Internal Error) | 500 Internal Server Error | Server error |

### Standard Error Format

```json
{
    "error": {
        "code": -32603,
        "message": "Internal server error",
        "correlationId": "unique-correlation-id",
        "timestamp": 1640995200,
        "details": {
            "additional": "context"
        },
        "stack": "stack trace (development only)"
    }
}
```

## Best Practices

### Error Handling
1. Always use `ProductionErrorTransformer` in production
2. Enable correlation ID tracking for debugging
3. Configure appropriate log levels
4. Use structured logging for error analysis

### Connection Pooling
1. Tune pool sizes based on expected load
2. Monitor connection pool utilization
3. Set appropriate timeouts for your network
4. Use connection pool statistics for optimization

### API Versioning
1. Plan version migration strategy early
2. Use semantic versioning consistently
3. Provide clear deprecation timelines
4. Monitor version adoption rates

### Testing
1. Run contract tests in CI/CD pipeline
2. Test all supported transport methods
3. Validate error handling scenarios
4. Test version compatibility