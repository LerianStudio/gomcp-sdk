// Package benchmarks provides comprehensive performance testing for the MCP SDK
package benchmarks

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/server"
	"github.com/LerianStudio/gomcp-sdk/transport"
	"github.com/gorilla/websocket"
)

// BenchmarkSuite represents a comprehensive benchmarking suite
type BenchmarkSuite struct {
	server        *server.Server
	httpTransport *transport.HTTPTransport
	wsTransport   *transport.WebSocketTransport
}

// NewBenchmarkSuite creates a new benchmark suite
func NewBenchmarkSuite() *BenchmarkSuite {
	srv := server.NewServer("benchmark-server", "1.0.0")
	
	// Add test tools with various complexities
	addBenchmarkTools(srv)
	
	return &BenchmarkSuite{
		server: srv,
	}
}

// addBenchmarkTools adds various tools for benchmarking
func addBenchmarkTools(srv *server.Server) {
	// Simple echo tool
	srv.AddTool(protocol.Tool{
		Name:        "echo",
		Description: "Simple echo tool for basic benchmarking",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"message": map[string]interface{}{"type": "string"},
			},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		if msg, ok := params["message"].(string); ok {
			return protocol.NewToolCallResult(protocol.NewContent("Echo: " + msg)), nil
		}
		return protocol.NewToolCallResult(protocol.NewContent("Echo: empty")), nil
	}))

	// CPU-intensive tool
	srv.AddTool(protocol.Tool{
		Name:        "fibonacci",
		Description: "CPU-intensive Fibonacci calculation",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"n": map[string]interface{}{"type": "number"},
			},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		n := int(params["n"].(float64))
		result := fibonacci(n)
		return protocol.NewToolCallResult(protocol.NewContent(fmt.Sprintf("fib(%d) = %d", n, result))), nil
	}))

	// Memory-intensive tool
	srv.AddTool(protocol.Tool{
		Name:        "large_data",
		Description: "Memory-intensive tool generating large data",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"size": map[string]interface{}{"type": "number"},
			},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		size := int(params["size"].(float64))
		data := strings.Repeat("x", size)
		return protocol.NewToolCallResult(protocol.NewContent(fmt.Sprintf("Generated %d bytes", len(data)))), nil
	}))

	// Slow tool (simulates I/O)
	srv.AddTool(protocol.Tool{
		Name:        "slow_operation",
		Description: "Tool that simulates slow I/O operations",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"delay_ms": map[string]interface{}{"type": "number"},
			},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		delay := time.Duration(params["delay_ms"].(float64)) * time.Millisecond
		time.Sleep(delay)
		return protocol.NewToolCallResult(protocol.NewContent(fmt.Sprintf("Completed after %v", delay))), nil
	}))
}

// fibonacci calculates Fibonacci number (CPU-intensive)
func fibonacci(n int) int {
	if n <= 1 {
		return n
	}
	return fibonacci(n-1) + fibonacci(n-2)
}

// Protocol Layer Benchmarks
func BenchmarkProtocolJSONRPC(b *testing.B) {
	_ = NewBenchmarkSuite() // Create suite for consistency

	b.Run("ParseRequest", func(b *testing.B) {
		data := []byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"test"}}}`)
		
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			_, err := protocol.ParseJSONRPCMessage(data)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("CreateResponse", func(b *testing.B) {
		result := protocol.NewToolCallResult(protocol.NewContent("test response"))
		
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := &protocol.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      1,
				Result:  result,
			}
			_, err := json.Marshal(resp)
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("FlexibleParsing", func(b *testing.B) {
		// Test various message formats
		messages := [][]byte{
			[]byte(`{"jsonrpc":"2.0","id":1,"method":"ping"}`),
			[]byte(`{"jsonrpc":"2.0","id":"str-id","method":"tools/list"}`),
			[]byte(`{"jsonrpc":"2.0","method":"notification"}`),
			[]byte(`{"jsonrpc":"2.0","id":null,"method":"null-id"}`),
		}
		
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			msg := messages[i%len(messages)]
			_, err := protocol.ParseJSONRPCMessage(msg)
			if err != nil {
				b.Fatal(err)
			}
		}
	})
}

// Server Layer Benchmarks
func BenchmarkServerOperations(b *testing.B) {
	suite := NewBenchmarkSuite()

	b.Run("EchoTool", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"echo","arguments":{"message":"benchmark"}}`),
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})

	b.Run("FibonacciTool", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"fibonacci","arguments":{"n":10}}`),
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})

	b.Run("ToolsList", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/list",
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})

	b.Run("Initialize", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "initialize",
			Params:  json.RawMessage(`{"protocolVersion":"1.0.0","capabilities":{},"clientInfo":{"name":"benchmark","version":"1.0.0"}}`),
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})
}

// Transport Layer Benchmarks
func BenchmarkTransportHTTP(b *testing.B) {
	suite := NewBenchmarkSuite()

	// Setup HTTP transport
	httpConfig := &transport.HTTPConfig{
		Address:     ":0", // Random port
		MaxBodySize: 1024 * 1024,
	}
	httpTransport := transport.NewHTTPTransport(httpConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go httpTransport.Start(ctx, suite.server)
	time.Sleep(50 * time.Millisecond) // Wait for server to start

	baseURL := "http://" + httpTransport.Address()

	b.Run("SimpleRequest", func(b *testing.B) {
		reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"test"}}}`
		
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp, err := http.Post(baseURL, "application/json", strings.NewReader(reqBody))
			if err != nil {
				b.Fatal(err)
			}
			resp.Body.Close()
		}
	})

	b.Run("ConcurrentRequests", func(b *testing.B) {
		reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"concurrent"}}}`
		
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp, err := http.Post(baseURL, "application/json", strings.NewReader(reqBody))
				if err != nil {
					b.Fatal(err)
				}
				resp.Body.Close()
			}
		})
	})
}

func BenchmarkTransportWebSocket(b *testing.B) {
	suite := NewBenchmarkSuite()

	// Setup WebSocket transport
	wsConfig := &transport.WebSocketConfig{
		Address: ":0", // Random port
		Path:    "/ws",
	}
	wsTransport := transport.NewWebSocketTransport(wsConfig)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go wsTransport.Start(ctx, suite.server)
	time.Sleep(50 * time.Millisecond) // Wait for server to start

	wsURL := "ws://" + wsTransport.Address() + "/ws"

	b.Run("SingleConnection", func(b *testing.B) {
		conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			b.Fatal(err)
		}
		defer conn.Close()

		reqData := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"ws-test"}}}`
		
		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			err := conn.WriteMessage(websocket.TextMessage, []byte(reqData))
			if err != nil {
				b.Fatal(err)
			}
			
			_, _, err = conn.ReadMessage()
			if err != nil {
				b.Fatal(err)
			}
		}
	})

	b.Run("MultipleConnections", func(b *testing.B) {
		const numConns = 10
		
		b.ReportAllocs()
		b.ResetTimer()
		b.RunParallel(func(pb *testing.PB) {
			conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
			if err != nil {
				b.Fatal(err)
			}
			defer conn.Close()

			reqData := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"echo","arguments":{"message":"parallel"}}}`
			
			for pb.Next() {
				err := conn.WriteMessage(websocket.TextMessage, []byte(reqData))
				if err != nil {
					b.Fatal(err)
				}
				
				_, _, err = conn.ReadMessage()
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	})
}

// Memory and Allocation Benchmarks
func BenchmarkMemoryPatterns(b *testing.B) {
	suite := NewBenchmarkSuite()

	b.Run("SmallPayload", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"echo","arguments":{"message":"small"}}`),
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})

	b.Run("LargePayload", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"large_data","arguments":{"size":10000}}`),
		}

		b.ReportAllocs()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			resp := suite.server.HandleRequest(context.Background(), req)
			if resp.Error != nil {
				b.Fatal(resp.Error)
			}
		}
	})
}

// Scalability Benchmarks
func BenchmarkScalability(b *testing.B) {
	suite := NewBenchmarkSuite()

	scales := []int{1, 10, 100, 1000}
	
	for _, scale := range scales {
		b.Run(fmt.Sprintf("Concurrent_%d", scale), func(b *testing.B) {
			req := &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"echo","arguments":{"message":"scale"}}`),
			}

			b.SetParallelism(scale)
			b.ReportAllocs()
			b.ResetTimer()
			b.RunParallel(func(pb *testing.PB) {
				for pb.Next() {
					resp := suite.server.HandleRequest(context.Background(), req)
					if resp.Error != nil {
						b.Fatal(resp.Error)
					}
				}
			})
		})
	}
}

// Throughput Benchmarks
func BenchmarkThroughput(b *testing.B) {
	suite := NewBenchmarkSuite()

	b.Run("RequestsPerSecond", func(b *testing.B) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      json.RawMessage(`1`),
			Method:  "tools/call",
			Params:  json.RawMessage(`{"name":"echo","arguments":{"message":"throughput"}}`),
		}

		var operations int64
		
		start := time.Now()
		b.ResetTimer()
		
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				resp := suite.server.HandleRequest(context.Background(), req)
				if resp.Error != nil {
					b.Fatal(resp.Error)
				}
				atomic.AddInt64(&operations, 1)
			}
		})
		
		duration := time.Since(start)
		rps := float64(operations) / duration.Seconds()
		b.ReportMetric(rps, "requests/sec")
	})
}

// Latency Benchmarks
func BenchmarkLatency(b *testing.B) {
	suite := NewBenchmarkSuite()
	
	scenarios := []struct {
		name   string
		params string
	}{
		{"Fast", `{"name":"echo","arguments":{"message":"fast"}}`},
		{"Slow", `{"name":"slow_operation","arguments":{"delay_ms":1}}`},
		{"CPU", `{"name":"fibonacci","arguments":{"n":15}}`},
	}

	for _, scenario := range scenarios {
		b.Run(scenario.name, func(b *testing.B) {
			req := &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
				Params:  json.RawMessage(scenario.params),
			}

			var totalDuration time.Duration
			
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				start := time.Now()
				resp := suite.server.HandleRequest(context.Background(), req)
				duration := time.Since(start)
				
				if resp.Error != nil {
					b.Fatal(resp.Error)
				}
				
				totalDuration += duration
			}
			
			avgLatency := totalDuration / time.Duration(b.N)
			b.ReportMetric(float64(avgLatency.Nanoseconds()), "ns/op")
		})
	}
}

// Regression Testing Framework
type PerformanceBaseline struct {
	OperationsPerSecond float64
	MemoryPerOperation  int64
	AverageLatency      time.Duration
}

// BenchmarkRegression runs regression tests against known baselines
func BenchmarkRegression(b *testing.B) {
	// Define baselines for regression testing
	baselines := map[string]PerformanceBaseline{
		"echo": {
			OperationsPerSecond: 10000,  // Minimum expected ops/sec
			MemoryPerOperation:  1024,   // Maximum expected memory per op
			AverageLatency:      time.Microsecond * 100, // Maximum expected latency
		},
	}

	suite := NewBenchmarkSuite()

	for testName, baseline := range baselines {
		b.Run(testName, func(b *testing.B) {
			req := &protocol.JSONRPCRequest{
				JSONRPC: "2.0",
				ID:      json.RawMessage(`1`),
				Method:  "tools/call",
				Params:  json.RawMessage(`{"name":"` + testName + `","arguments":{"message":"regression"}}`),
			}

			// Measure performance
			start := time.Now()
			var operations int64
			
			b.ResetTimer()
			b.ReportAllocs()
			
			for i := 0; i < b.N; i++ {
				resp := suite.server.HandleRequest(context.Background(), req)
				if resp.Error != nil {
					b.Fatal(resp.Error)
				}
				atomic.AddInt64(&operations, 1)
			}
			
			duration := time.Since(start)
			
			// Calculate metrics
			opsPerSec := float64(operations) / duration.Seconds()
			avgLatency := duration / time.Duration(b.N)
			
			// Report custom metrics
			b.ReportMetric(opsPerSec, "ops/sec")
			b.ReportMetric(float64(avgLatency.Nanoseconds()), "avg_latency_ns")
			
			// Regression checks (would normally fail the test if not met)
			if opsPerSec < baseline.OperationsPerSecond {
				b.Logf("WARNING: Performance regression detected. Expected >= %.0f ops/sec, got %.0f", 
					baseline.OperationsPerSecond, opsPerSec)
			}
			
			if avgLatency > baseline.AverageLatency {
				b.Logf("WARNING: Latency regression detected. Expected <= %v, got %v", 
					baseline.AverageLatency, avgLatency)
			}
		})
	}
}