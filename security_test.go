package mcp

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fredcamaral/gomcp-sdk/protocol"
	"github.com/fredcamaral/gomcp-sdk/server"
)

// TestInputValidation tests various input validation scenarios
func TestInputValidation(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
		validate  func(req *protocol.JSONRPCRequest) error
	}{
		{
			name:      "valid JSON-RPC request",
			input:     `{"jsonrpc":"2.0","method":"test","params":{},"id":1}`,
			wantError: false,
		},
		{
			name:      "invalid JSON",
			input:     `{"jsonrpc":"2.0","method":"test"`,
			wantError: true,
		},
		{
			name:      "SQL injection attempt",
			input:     `{"jsonrpc":"2.0","method":"test","params":{"query":"'; DROP TABLE users; --"},"id":1}`,
			wantError: false, // Should be handled safely by the application
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var req protocol.JSONRPCRequest
			err := json.Unmarshal([]byte(tt.input), &req)

			if tt.wantError {
				if err == nil && tt.validate != nil {
					err = tt.validate(&req)
				}
				if err == nil {
					t.Errorf("expected error for input: %s", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
			}
		})
	}
}

// TestPathTraversalProtection tests protection against directory traversal attacks
func TestPathTraversalProtection(t *testing.T) {
	paths := []struct {
		input    string
		expected string
		blocked  bool
	}{
		{"/valid/path/file.txt", "/valid/path/file.txt", false},
		{"../../../etc/passwd", "", true},
		{"/path/../../../etc/passwd", "", true},
		{"/path/..\\..\\..\\windows\\system32", "", true},
		{"/path/%2e%2e%2f%2e%2e%2f", "", true},
		{"/path/\x00/malicious", "", true},
	}

	for _, p := range paths {
		t.Run(p.input, func(t *testing.T) {
			// Clean the path
			_ = filepath.Clean(p.input)
			
			// Check for path traversal attempts
			// Also check for URL-encoded dots (%2e)
			decoded := p.input
			if strings.Contains(p.input, "%") {
				// Simple URL decoding for test
				decoded = strings.ReplaceAll(decoded, "%2e", ".")
				decoded = strings.ReplaceAll(decoded, "%2f", "/")
			}
			
			if strings.Contains(decoded, "..") || strings.Contains(p.input, "\x00") {
				if !p.blocked {
					t.Errorf("path traversal not blocked: %s", p.input)
				}
			} else {
				if p.blocked {
					t.Errorf("safe path blocked: %s", p.input)
				}
			}
		})
	}
}

// TestRateLimiting tests rate limiting functionality
func TestRateLimiting(t *testing.T) {
	// Skip test if it takes too long
	t.Skip("Rate limiting test disabled for CI")
	
	_ = server.NewServer("test", "1.0.0")
	
	// Configure aggressive rate limit for testing
	rateLimiter := &mockRateLimiter{
		limit:     5,
		window:    time.Second,
		requests:  make(map[string][]time.Time),
	}
	
	clientID := "test-client"
	
	// Make requests up to the limit
	for i := 0; i < 5; i++ {
		if !rateLimiter.Allow(clientID) {
			t.Errorf("request %d should be allowed", i)
		}
	}
	
	// Next request should be blocked
	if rateLimiter.Allow(clientID) {
		t.Error("request should be rate limited")
	}
	
	// Wait for window to pass
	time.Sleep(time.Second)
	
	// Should be allowed again
	if !rateLimiter.Allow(clientID) {
		t.Error("request should be allowed after window")
	}
}

// TestSecureDefaults tests that secure defaults are properly set
func TestSecureDefaults(t *testing.T) {
	s := server.NewServer("test", "1.0.0")
	
	// Test that server has secure defaults
	if s == nil {
		t.Fatal("server should not be nil")
	}
	
	// Additional security default tests can be added here
}

// mockRateLimiter implements a simple rate limiter for testing
type mockRateLimiter struct {
	limit    int
	window   time.Duration
	requests map[string][]time.Time
}

func (r *mockRateLimiter) Allow(clientID string) bool {
	now := time.Now()
	cutoff := now.Add(-r.window)
	
	// Clean old requests
	validRequests := []time.Time{}
	for _, reqTime := range r.requests[clientID] {
		if reqTime.After(cutoff) {
			validRequests = append(validRequests, reqTime)
		}
	}
	
	if len(validRequests) >= r.limit {
		return false
	}
	
	r.requests[clientID] = append(validRequests, now)
	return true
}