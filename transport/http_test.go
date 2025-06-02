package transport

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockHandler implements RequestHandler for testing
type mockHandler struct {
	handleFunc func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse
}

func (m *mockHandler) HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	if m.handleFunc != nil {
		return m.handleFunc(ctx, req)
	}
	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  map[string]interface{}{"echo": req.Method},
	}
}

func TestHTTPTransport_StartStop(t *testing.T) {
	config := &HTTPConfig{
		Address:      "localhost:0",
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}

	transport := NewHTTPTransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start transport
	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	assert.True(t, transport.IsRunning())

	// Try starting again (should fail)
	err = transport.Start(ctx, handler)
	assert.Error(t, err)

	// Stop transport
	err = transport.Stop()
	require.NoError(t, err)
	assert.False(t, transport.IsRunning())

	// Stop again (should be no-op)
	err = transport.Stop()
	assert.NoError(t, err)
}

func TestHTTPTransport_HandleRequest(t *testing.T) {
	config := &HTTPConfig{
		Address: "localhost:0",
		Path:    "/rpc",
	}

	transport := NewHTTPTransport(config)

	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
			return &protocol.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"method": req.Method,
					"params": req.Params,
				},
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	// Get actual address
	addr := transport.Address()
	baseURL := "http://" + addr

	tests := []struct {
		name           string
		request        *protocol.JSONRPCRequest
		expectedResult map[string]interface{}
		expectedError  bool
	}{
		{
			name: "valid request",
			request: &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "test.method",
				Params:  map[string]interface{}{"key": "value"},
			},
			expectedResult: map[string]interface{}{
				"method": "test.method",
				"params": map[string]interface{}{"key": "value"},
			},
		},
		{
			name: "request without params",
			request: &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      2,
				Method:  "test.noparams",
			},
			expectedResult: map[string]interface{}{
				"method": "test.noparams",
				"params": nil,
			},
		},
	}

	client := &http.Client{Timeout: 5 * time.Second}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			reqBody, err := json.Marshal(tt.request)
			require.NoError(t, err)

			resp, err := client.Post(baseURL+"/rpc", "application/json", bytes.NewReader(reqBody))
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, http.StatusOK, resp.StatusCode)
			assert.Equal(t, "application/json", resp.Header.Get("Content-Type"))

			var jsonResp protocol.JSONRPCResponse
			err = json.NewDecoder(resp.Body).Decode(&jsonResp)
			require.NoError(t, err)

			assert.Equal(t, "2.0", jsonResp.JSONRPC)

			// Compare IDs handling JSON number conversion
			switch expected := tt.request.ID.(type) {
			case int:
				if actual, ok := jsonResp.ID.(float64); ok {
					assert.Equal(t, float64(expected), actual)
				} else {
					assert.Equal(t, tt.request.ID, jsonResp.ID)
				}
			case int64:
				if actual, ok := jsonResp.ID.(float64); ok {
					assert.Equal(t, float64(expected), actual)
				} else {
					assert.Equal(t, tt.request.ID, jsonResp.ID)
				}
			default:
				assert.Equal(t, tt.request.ID, jsonResp.ID)
			}

			if tt.expectedError {
				assert.NotNil(t, jsonResp.Error)
			} else {
				assert.Nil(t, jsonResp.Error)
				assert.Equal(t, tt.expectedResult, jsonResp.Result)
			}
		})
	}
}

func TestHTTPTransport_ErrorHandling(t *testing.T) {
	config := &HTTPConfig{
		Address:     "localhost:0",
		MaxBodySize: 100, // Very small for testing
	}

	transport := NewHTTPTransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()
	baseURL := "http://" + addr

	client := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name         string
		method       string
		body         string
		expectedCode int
		checkError   func(t *testing.T, resp *http.Response)
	}{
		{
			name:         "invalid method",
			method:       "GET",
			body:         "",
			expectedCode: http.StatusMethodNotAllowed,
		},
		{
			name:         "invalid json",
			method:       "POST",
			body:         "{invalid json}",
			expectedCode: http.StatusOK, // JSON-RPC error
			checkError: func(t *testing.T, resp *http.Response) {
				var jsonResp protocol.JSONRPCResponse
				err := json.NewDecoder(resp.Body).Decode(&jsonResp)
				require.NoError(t, err)
				assert.NotNil(t, jsonResp.Error)
				assert.Equal(t, protocol.ParseError, jsonResp.Error.Code)
			},
		},
		{
			name:         "body too large",
			method:       "POST",
			body:         string(make([]byte, 200)), // Larger than MaxBodySize
			expectedCode: http.StatusOK,
			checkError: func(t *testing.T, resp *http.Response) {
				var jsonResp protocol.JSONRPCResponse
				err := json.NewDecoder(resp.Body).Decode(&jsonResp)
				require.NoError(t, err)
				assert.NotNil(t, jsonResp.Error)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, baseURL+"/", strings.NewReader(tt.body))
			require.NoError(t, err)

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			assert.Equal(t, tt.expectedCode, resp.StatusCode)

			if tt.checkError != nil {
				tt.checkError(t, resp)
			}
		})
	}
}

func TestHTTPTransport_CORS(t *testing.T) {
	config := &HTTPConfig{
		Address:        "localhost:0",
		EnableCORS:     true,
		AllowedOrigins: []string{"https://example.com", "https://test.com"},
	}

	transport := NewHTTPTransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()
	baseURL := "http://" + addr

	client := &http.Client{Timeout: 5 * time.Second}

	tests := []struct {
		name                string
		origin              string
		method              string
		expectedAllowed     bool
		expectedAllowOrigin string
	}{
		{
			name:                "allowed origin",
			origin:              "https://example.com",
			method:              "OPTIONS",
			expectedAllowed:     true,
			expectedAllowOrigin: "https://example.com",
		},
		{
			name:            "disallowed origin",
			origin:          "https://malicious.com",
			method:          "OPTIONS",
			expectedAllowed: false,
		},
		{
			name:                "preflight request",
			origin:              "https://test.com",
			method:              "OPTIONS",
			expectedAllowed:     true,
			expectedAllowOrigin: "https://test.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, err := http.NewRequest(tt.method, baseURL+"/", nil)
			require.NoError(t, err)
			req.Header.Set("Origin", tt.origin)

			resp, err := client.Do(req)
			require.NoError(t, err)
			defer func() { _ = resp.Body.Close() }()

			if tt.expectedAllowed {
				assert.Equal(t, tt.expectedAllowOrigin, resp.Header.Get("Access-Control-Allow-Origin"))
				assert.Equal(t, "POST, OPTIONS", resp.Header.Get("Access-Control-Allow-Methods"))
			} else {
				assert.Empty(t, resp.Header.Get("Access-Control-Allow-Origin"))
			}

			if tt.method == "OPTIONS" {
				assert.Equal(t, http.StatusNoContent, resp.StatusCode)
			}
		})
	}
}

func TestHTTPTransport_CustomHeaders(t *testing.T) {
	customHeaders := map[string]string{
		"X-Custom-Header": "test-value",
		"X-API-Version":   "1.0",
	}

	config := &HTTPConfig{
		Address:       "localhost:0",
		CustomHeaders: customHeaders,
	}

	transport := NewHTTPTransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()
	client := &http.Client{Timeout: 5 * time.Second}

	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
	}

	reqBody, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := client.Post("http://"+addr+"/", "application/json", bytes.NewReader(reqBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Check custom headers
	for k, v := range customHeaders {
		assert.Equal(t, v, resp.Header.Get(k))
	}
}

func TestHTTPTransport_RecoveryMiddleware(t *testing.T) {
	config := &HTTPConfig{
		Address: "localhost:0",
	}

	transport := NewHTTPTransport(config)

	// Handler that panics
	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
			panic("test panic")
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()
	client := &http.Client{Timeout: 5 * time.Second}

	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
	}

	reqBody, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := client.Post("http://"+addr+"/", "application/json", bytes.NewReader(reqBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	// Should get internal error response instead of crash
	var jsonResp protocol.JSONRPCResponse
	err = json.NewDecoder(resp.Body).Decode(&jsonResp)
	require.NoError(t, err)
	assert.NotNil(t, jsonResp.Error)
	assert.Equal(t, protocol.InternalError, jsonResp.Error.Code)
}

func TestHTTPSTransport(t *testing.T) {
	// Generate test certificates
	certs, err := GenerateTestCertificates([]string{"localhost", "127.0.0.1"})
	require.NoError(t, err)

	// Write certificates to temporary files
	tempDir := t.TempDir()
	_, _, serverCertFile, serverKeyFile, _, _, err := certs.WriteCertificatesToFiles(tempDir)
	require.NoError(t, err)

	// Create TLS config with test CA
	tlsConfig, err := CreateTLSConfig(certs.CAPEMCert, certs.ServerPEMCert, certs.ServerPEMKey, tls.NoClientCert)
	require.NoError(t, err)

	config := &HTTPConfig{
		Address:   "localhost:0",
		TLSConfig: tlsConfig,
	}

	transport := NewHTTPSTransport(config, serverCertFile, serverKeyFile)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()

	// Create HTTPS client with CA cert
	caCertPool := x509.NewCertPool()
	caCertPool.AppendCertsFromPEM(certs.CAPEMCert)

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: caCertPool,
			},
		},
		Timeout: 5 * time.Second,
	}

	// Test basic HTTPS request
	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "test",
	}

	reqBody, err := json.Marshal(req)
	require.NoError(t, err)
	resp, err := client.Post("https://"+addr+"/", "application/json", bytes.NewReader(reqBody))
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()

	var jsonResp protocol.JSONRPCResponse
	err = json.NewDecoder(resp.Body).Decode(&jsonResp)
	require.NoError(t, err)

	// Compare IDs handling JSON number conversion
	switch expected := req.ID.(type) {
	case int:
		if actual, ok := jsonResp.ID.(float64); ok {
			assert.Equal(t, float64(expected), actual)
		} else {
			assert.Equal(t, req.ID, jsonResp.ID)
		}
	default:
		assert.Equal(t, req.ID, jsonResp.ID)
	}

	// Check result
	result := jsonResp.Result.(map[string]interface{})
	assert.Equal(t, "test", result["echo"])
}

func TestHTTPSTransport_CertificateScenarios(t *testing.T) {
	tests := []struct {
		name          string
		setupCert     func(*TestCertificates, string) (string, string, error)
		clientConfig  func(*TestCertificates) *tls.Config
		expectError   bool
		errorContains string
	}{
		{
			name: "valid certificate",
			setupCert: func(certs *TestCertificates, dir string) (string, string, error) {
				_, _, serverCertFile, serverKeyFile, _, _, err := certs.WriteCertificatesToFiles(dir)
				return serverCertFile, serverKeyFile, err
			},
			clientConfig: func(certs *TestCertificates) *tls.Config {
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(certs.CAPEMCert)
				return &tls.Config{RootCAs: caCertPool}
			},
			expectError: false,
		},
		{
			name: "expired certificate",
			setupCert: func(certs *TestCertificates, dir string) (string, string, error) {
				expiredCert, expiredKey, err := GenerateExpiredCertificate(certs.CACert, certs.CAKey, []string{"localhost", "127.0.0.1"})
				if err != nil {
					return "", "", err
				}
				certFile := dir + "/expired-cert.pem"
				keyFile := dir + "/expired-key.pem"
				if err := os.WriteFile(certFile, expiredCert, 0644); err != nil {
					return "", "", err
				}
				if err := os.WriteFile(keyFile, expiredKey, 0600); err != nil {
					return "", "", err
				}
				return certFile, keyFile, nil
			},
			clientConfig: func(certs *TestCertificates) *tls.Config {
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(certs.CAPEMCert)
				return &tls.Config{RootCAs: caCertPool}
			},
			expectError:   true,
			errorContains: "certificate has expired",
		},
		{
			name: "wrong hostname",
			setupCert: func(certs *TestCertificates, dir string) (string, string, error) {
				wrongCert, wrongKey, err := GenerateWrongHostnameCertificate(certs.CACert, certs.CAKey)
				if err != nil {
					return "", "", err
				}
				certFile := dir + "/wrong-cert.pem"
				keyFile := dir + "/wrong-key.pem"
				if err := os.WriteFile(certFile, wrongCert, 0644); err != nil {
					return "", "", err
				}
				if err := os.WriteFile(keyFile, wrongKey, 0600); err != nil {
					return "", "", err
				}
				return certFile, keyFile, nil
			},
			clientConfig: func(certs *TestCertificates) *tls.Config {
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(certs.CAPEMCert)
				return &tls.Config{RootCAs: caCertPool}
			},
			expectError:   true,
			errorContains: "certificate",
		},
		{
			name: "untrusted certificate",
			setupCert: func(certs *TestCertificates, dir string) (string, string, error) {
				// Create a new self-signed certificate not signed by our CA
				untrustedCerts, err := GenerateTestCertificates([]string{"localhost", "127.0.0.1"})
				if err != nil {
					return "", "", err
				}
				certFile := dir + "/untrusted-cert.pem"
				keyFile := dir + "/untrusted-key.pem"
				if err := os.WriteFile(certFile, untrustedCerts.ServerPEMCert, 0644); err != nil {
					return "", "", err
				}
				if err := os.WriteFile(keyFile, untrustedCerts.ServerPEMKey, 0600); err != nil {
					return "", "", err
				}
				return certFile, keyFile, nil
			},
			clientConfig: func(certs *TestCertificates) *tls.Config {
				caCertPool := x509.NewCertPool()
				caCertPool.AppendCertsFromPEM(certs.CAPEMCert)
				return &tls.Config{RootCAs: caCertPool}
			},
			expectError:   true,
			errorContains: "certificate signed by unknown authority",
		},
		{
			name: "skip verify",
			setupCert: func(certs *TestCertificates, dir string) (string, string, error) {
				// Use any certificate - client will skip verification
				_, _, serverCertFile, serverKeyFile, _, _, err := certs.WriteCertificatesToFiles(dir)
				return serverCertFile, serverKeyFile, err
			},
			clientConfig: func(certs *TestCertificates) *tls.Config {
				return &tls.Config{InsecureSkipVerify: true}
			},
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Generate base test certificates
			certs, err := GenerateTestCertificates([]string{"localhost", "127.0.0.1"})
			require.NoError(t, err)

			// Setup specific certificate for this test
			tempDir := t.TempDir()
			certFile, keyFile, err := tt.setupCert(certs, tempDir)
			require.NoError(t, err)

			// Create server with certificate
			config := &HTTPConfig{
				Address: "localhost:0",
			}

			transport := NewHTTPSTransport(config, certFile, keyFile)
			handler := &mockHandler{}

			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			err = transport.Start(ctx, handler)
			require.NoError(t, err)
			defer func() { _ = transport.Stop() }()

			addr := transport.Address()

			// Create client with specific TLS config
			client := &http.Client{
				Transport: &http.Transport{
					TLSClientConfig: tt.clientConfig(certs),
				},
				Timeout: 5 * time.Second,
			}

			// Make request
			req := &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      1,
				Method:  "test",
			}

			reqBody, err := json.Marshal(req)
			require.NoError(t, err)
			resp, err := client.Post("https://"+addr+"/", "application/json", bytes.NewReader(reqBody))

			if tt.expectError {
				require.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				require.NoError(t, err)
				defer func() { _ = resp.Body.Close() }()

				var jsonResp protocol.JSONRPCResponse
				err = json.NewDecoder(resp.Body).Decode(&jsonResp)
				require.NoError(t, err)

				// Compare IDs handling JSON number conversion
				switch expected := req.ID.(type) {
				case int:
					if actual, ok := jsonResp.ID.(float64); ok {
						assert.Equal(t, float64(expected), actual)
					} else {
						assert.Equal(t, req.ID, jsonResp.ID)
					}
				default:
					assert.Equal(t, req.ID, jsonResp.ID)
				}
			}
		})
	}
}

func TestHTTPSTransport_MutualTLS(t *testing.T) {
	// Generate test certificates
	certs, err := GenerateTestCertificates([]string{"localhost", "127.0.0.1"})
	require.NoError(t, err)

	// Write certificates to temporary files
	tempDir := t.TempDir()
	_, _, serverCertFile, serverKeyFile, clientCertFile, clientKeyFile, err := certs.WriteCertificatesToFiles(tempDir)
	require.NoError(t, err)

	// Create server TLS config requiring client certificates
	serverTLSConfig, err := CreateTLSConfig(certs.CAPEMCert, certs.ServerPEMCert, certs.ServerPEMKey, tls.RequireAndVerifyClientCert)
	require.NoError(t, err)

	config := &HTTPConfig{
		Address:   "localhost:0",
		TLSConfig: serverTLSConfig,
	}

	transport := NewHTTPSTransport(config, serverCertFile, serverKeyFile)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = transport.Start(ctx, handler)
	require.NoError(t, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()

	t.Run("with client certificate", func(t *testing.T) {
		// Load client certificate
		clientCert, err := tls.LoadX509KeyPair(clientCertFile, clientKeyFile)
		require.NoError(t, err)

		// Create client with client certificate
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(certs.CAPEMCert)

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs:      caCertPool,
					Certificates: []tls.Certificate{clientCert},
				},
			},
			Timeout: 5 * time.Second,
		}

		// Make request
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test",
		}

		reqBody, err := json.Marshal(req)
		require.NoError(t, err)
		resp, err := client.Post("https://"+addr+"/", "application/json", bytes.NewReader(reqBody))
		require.NoError(t, err)
		defer func() { _ = resp.Body.Close() }()

		var jsonResp protocol.JSONRPCResponse
		err = json.NewDecoder(resp.Body).Decode(&jsonResp)
		require.NoError(t, err)

		// Compare IDs handling JSON number conversion
		switch expected := req.ID.(type) {
		case int:
			if actual, ok := jsonResp.ID.(float64); ok {
				assert.Equal(t, float64(expected), actual)
			} else {
				assert.Equal(t, req.ID, jsonResp.ID)
			}
		default:
			assert.Equal(t, req.ID, jsonResp.ID)
		}
	})

	t.Run("without client certificate", func(t *testing.T) {
		// Create client without client certificate
		caCertPool := x509.NewCertPool()
		caCertPool.AppendCertsFromPEM(certs.CAPEMCert)

		client := &http.Client{
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: caCertPool,
				},
			},
			Timeout: 5 * time.Second,
		}

		// Make request - should fail
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test",
		}

		reqBody, err := json.Marshal(req)
		require.NoError(t, err)
		_, err = client.Post("https://"+addr+"/", "application/json", bytes.NewReader(reqBody))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "certificate required")
	})
}

// Benchmark tests

func BenchmarkHTTPTransport_HandleRequest(b *testing.B) {
	config := &HTTPConfig{
		Address: "localhost:0",
	}

	transport := NewHTTPTransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(b, err)
	defer func() { _ = transport.Stop() }()

	addr := transport.Address()
	client := &http.Client{
		Timeout: 5 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 100,
		},
	}

	req := &protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "benchmark.test",
		Params:  map[string]interface{}{"data": "test"},
	}
	reqBody, err := json.Marshal(req)
	require.NoError(b, err)

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			resp, err := client.Post("http://"+addr+"/", "application/json", bytes.NewReader(reqBody))
			if err != nil {
				b.Fatal(err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
		}
	})
}
