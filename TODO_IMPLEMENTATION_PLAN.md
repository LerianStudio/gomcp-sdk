# TODO Implementation Plan

This document provides a detailed implementation plan for all pending TODO items in the gomcp-sdk project.

## High Priority

### 1. Sampling Implementation (`sampling/handler.go:34`)

#### Current State
- Returns mock response: `{"sampled_text": "Sample of: [input]"}`
- No actual LLM integration

#### Implementation Steps
1. **Define Sampling Interface**
   - Create `sampling/types.go` with sampling request/response types
   - Define sampling parameters (temperature, max_tokens, etc.)
   - Support multiple sampling strategies

2. **Create LLM Provider Interface**
   ```go
   type LLMProvider interface {
       Sample(ctx context.Context, req SamplingRequest) (*SamplingResponse, error)
       ValidateConfig() error
   }
   ```

3. **Implement Provider Adapters**
   - OpenAI adapter (`sampling/providers/openai.go`)
   - Anthropic adapter (`sampling/providers/anthropic.go`)
   - Local model adapter (`sampling/providers/local.go`)

4. **Configuration System**
   - Add environment variables for API keys
   - Support provider selection via config
   - Add retry logic with exponential backoff

5. **Update Handler**
   - Replace mock with actual provider calls
   - Add proper error handling
   - Implement request validation
   - Add metrics/logging

6. **Testing**
   - Create mock provider for tests
   - Add integration tests (with test API keys)
   - Add benchmarks for performance

#### Files to Create/Modify
- `sampling/types.go` - Type definitions
- `sampling/providers/interface.go` - Provider interface
- `sampling/providers/openai.go` - OpenAI implementation
- `sampling/providers/anthropic.go` - Anthropic implementation
- `sampling/config.go` - Configuration management
- `sampling/handler_test.go` - Comprehensive tests

## Medium Priority

### 2. Resource List Parsing and Verification (`testutil/scenarios.go:336`)

#### Current State
- Empty verification function
- No parsing of resource list responses

#### Implementation Steps
1. **Define Expected Resource Structure**
   ```go
   type ResourceListResponse struct {
       Resources []Resource `json:"resources"`
       NextCursor string   `json:"nextCursor,omitempty"`
   }
   ```

2. **Implement Parser**
   - Parse JSON response into struct
   - Validate required fields
   - Check resource URIs format

3. **Add Verification Logic**
   - Verify resource count
   - Check resource attributes (name, uri, mimeType)
   - Validate pagination if present

4. **Create Helper Functions**
   - `parseResourceList(data []byte) (*ResourceListResponse, error)`
   - `validateResource(r Resource) error`
   - `assertResourcesMatch(expected, actual []Resource) error`

#### Code Example
```go
func(t *testing.T, resp []byte) error {
    var listResp ResourceListResponse
    if err := json.Unmarshal(resp, &listResp); err != nil {
        return fmt.Errorf("failed to parse resource list: %w", err)
    }
    
    // Verify we got resources
    if len(listResp.Resources) == 0 {
        return errors.New("expected at least one resource")
    }
    
    // Validate each resource
    for _, res := range listResp.Resources {
        if err := validateResource(res); err != nil {
            return fmt.Errorf("invalid resource %s: %w", res.URI, err)
        }
    }
    return nil
}
```

### 3. Content Verification (`testutil/scenarios.go:353`)

#### Current State
- Empty verification function
- No content validation

#### Implementation Steps
1. **Parse Content Response**
   ```go
   type ContentResponse struct {
       URI      string `json:"uri"`
       MimeType string `json:"mimeType"`
       Content  string `json:"content"`
   }
   ```

2. **Implement Verification**
   - Verify content matches expected format
   - Check MIME type consistency
   - Validate content encoding (base64 for binary)

3. **Add Content-Type Specific Validation**
   - Text files: Check encoding, line endings
   - JSON files: Validate structure
   - Binary files: Verify base64 encoding

#### Code Example
```go
func(t *testing.T, resp []byte) error {
    var contentResp ContentResponse
    if err := json.Unmarshal(resp, &contentResp); err != nil {
        return fmt.Errorf("failed to parse content response: %w", err)
    }
    
    // Verify content based on MIME type
    switch contentResp.MimeType {
    case "application/json":
        var js json.RawMessage
        if err := json.Unmarshal([]byte(contentResp.Content), &js); err != nil {
            return fmt.Errorf("invalid JSON content: %w", err)
        }
    case "text/plain":
        if contentResp.Content == "" {
            return errors.New("empty text content")
        }
    default:
        // For binary, verify base64
        if _, err := base64.StdEncoding.DecodeString(contentResp.Content); err != nil {
            return fmt.Errorf("invalid base64 content: %w", err)
        }
    }
    return nil
}
```

### 4. Tool Handler Loading (`discovery/watcher.go:203`)

#### Current State
- Comment indicates need for actual implementation
- No dynamic loading mechanism

#### Implementation Steps
1. **Define Plugin Interface**
   ```go
   type ToolPlugin interface {
       GetManifest() Manifest
       CreateHandler() Handler
       Close() error
   }
   ```

2. **Implement Plugin Loading**
   - **Option A: Subprocess Communication**
     - Fork process with tool implementation
     - Communicate via stdin/stdout or gRPC
     - Monitor process health
   
   - **Option B: Shared Library (.so/.dll)**
     - Use plugin package for Go plugins
     - Load .so/.dll files dynamically
     - Handle platform differences

   - **Option C: Embedded Handlers**
     - Registry of built-in handlers
     - Factory pattern for creation
     - Easier to test and deploy

3. **Create Handler Registry**
   ```go
   type HandlerRegistry struct {
       mu       sync.RWMutex
       handlers map[string]HandlerFactory
   }
   ```

4. **Implement Loading Logic**
   ```go
   func (w *Watcher) loadToolHandler(toolPath string, tool Tool) error {
       // Try embedded first
       if factory, ok := w.registry.GetEmbedded(tool.Name); ok {
           return w.registry.RegisterTool(tool, factory())
       }
       
       // Try subprocess
       handler, err := subprocess.NewHandler(toolPath, tool)
       if err != nil {
           return fmt.Errorf("failed to load tool %s: %w", tool.Name, err)
       }
       
       return w.registry.RegisterTool(tool, handler)
   }
   ```

## Low Priority

### 5. SSE Event Parsing (`transport/sse_test.go:413`)

#### Current State
- Logs placeholder message
- No actual SSE event parsing

#### Implementation Steps
1. **Create SSE Parser**
   ```go
   type SSEEvent struct {
       ID    string
       Event string
       Data  string
       Retry int
   }
   
   func ParseSSEEvent(data string) (*SSEEvent, error)
   ```

2. **Implement Event Stream Reader**
   - Handle multi-line data
   - Parse event fields correctly
   - Handle reconnection logic

3. **Update Test**
   ```go
   events := make(chan SSEEvent)
   go func() {
       scanner := bufio.NewScanner(resp.Body)
       var event SSEEvent
       for scanner.Scan() {
           line := scanner.Text()
           if line == "" && event.Data != "" {
               events <- event
               event = SSEEvent{}
               continue
           }
           // Parse line into event fields
           parseSSELine(line, &event)
       }
   }()
   ```

### 6. HTTPS Certificate Testing (`transport/http_test.go:419`)

#### Current State
- Uses self-signed certificates
- Basic test implementation

#### Implementation Steps
1. **Create Test Certificate Authority**
   - Generate CA certificate
   - Generate server certificate signed by CA
   - Create client certificate for mTLS testing

2. **Implement Certificate Helpers**
   ```go
   type TestCertificates struct {
       CACert     *x509.Certificate
       CAKey      *rsa.PrivateKey
       ServerCert tls.Certificate
       ClientCert tls.Certificate
   }
   
   func GenerateTestCertificates(domains []string) (*TestCertificates, error)
   ```

3. **Add Comprehensive Tests**
   - Certificate validation
   - mTLS authentication
   - Certificate rotation
   - Invalid certificate handling

4. **Create Test Scenarios**
   - Valid certificate chain
   - Expired certificates
   - Wrong hostname
   - Untrusted CA
   - Client certificate required

## Implementation Order

1. **Week 1**: Sampling Implementation (High Priority)
   - Days 1-2: Design interfaces and types
   - Days 3-4: Implement OpenAI provider
   - Day 5: Testing and documentation

2. **Week 2**: Test Improvements (Medium Priority)
   - Days 1-2: Resource verification
   - Days 3-4: Content verification
   - Day 5: Integration with test suite

3. **Week 3**: Discovery Implementation (Medium Priority)
   - Days 1-2: Design plugin system
   - Days 3-4: Implement subprocess handler
   - Day 5: Testing and examples

4. **Week 4**: Test Enhancements (Low Priority)
   - Days 1-2: SSE event parsing
   - Days 3-4: HTTPS certificate testing
   - Day 5: Documentation and cleanup

## Testing Strategy

### Unit Tests
- Mock all external dependencies
- Test error conditions
- Verify edge cases

### Integration Tests
- Use test containers for services
- Test with real protocols
- Verify end-to-end flows

### Performance Tests
- Benchmark critical paths
- Test under load
- Memory leak detection

## Documentation Updates

1. **API Documentation**
   - Update godoc comments
   - Add usage examples
   - Document configuration

2. **README Updates**
   - Add sampling configuration section
   - Update feature list
   - Add troubleshooting guide

3. **Example Applications**
   - Create sampling example
   - Update file browser with real handlers
   - Add discovery example

## Dependencies

### New Dependencies Needed
```go
// go.mod additions
require (
    github.com/sashabaranov/go-openai v1.20.0  // For OpenAI integration
    golang.org/x/time v0.5.0                    // For rate limiting
    github.com/cenkalti/backoff/v4 v4.2.1       // For retry logic
)
```

### Optional Dependencies
```go
// For specific features
require (
    github.com/hashicorp/go-plugin v1.6.0      // For plugin system
    google.golang.org/grpc v1.60.0              // For gRPC communication
)
```

## Success Criteria

1. **Sampling**: Successfully integrates with at least one LLM provider
2. **Tests**: All test scenarios pass with proper verification
3. **Discovery**: Can load and execute external tool handlers
4. **Performance**: No regression in existing benchmarks
5. **Documentation**: All changes documented with examples

## Risks and Mitigations

1. **LLM API Changes**
   - Risk: Provider APIs change
   - Mitigation: Abstract behind interfaces, version lock SDKs

2. **Platform Compatibility**
   - Risk: Plugin system doesn't work on all platforms
   - Mitigation: Provide subprocess fallback

3. **Security Concerns**
   - Risk: Loading untrusted code
   - Mitigation: Sandbox execution, capability restrictions

4. **Performance Impact**
   - Risk: New features slow down core
   - Mitigation: Lazy loading, caching, benchmarks

## Notes

- Keep backward compatibility
- Follow existing code style
- Add comprehensive tests for new features
- Update CHANGELOG.md with all changes
- Consider feature flags for experimental features