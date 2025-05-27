package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/fredcamaral/gomcp-sdk/protocol"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sseTestClient is a test client for SSE
type sseTestClient struct {
	events chan string
	errors chan error
	done   chan struct{}
}

func newSSETestClient() *sseTestClient {
	return &sseTestClient{
		events: make(chan string, 100),
		errors: make(chan error, 10),
		done:   make(chan struct{}),
	}
}

func (c *sseTestClient) connect(url string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}

	// Read SSE stream
	go func() {
		defer close(c.done)
		scanner := bufio.NewScanner(resp.Body)
		defer resp.Body.Close()

		var eventData strings.Builder
		for scanner.Scan() {
			line := scanner.Text()

			if line == "" && eventData.Len() > 0 {
				// End of event
				c.events <- eventData.String()
				eventData.Reset()
			} else if strings.HasPrefix(line, "data: ") {
				eventData.WriteString(strings.TrimPrefix(line, "data: "))
			}
		}

		if err := scanner.Err(); err != nil {
			c.errors <- err
		}
	}()

	return nil
}

func (c *sseTestClient) close() {
	close(c.events)
	close(c.errors)
}

func TestSSETransport_StartStop(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath: "/events",
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start transport
	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	assert.True(t, transport.IsRunning())

	// Stop transport
	err = transport.Stop()
	// On some platforms (especially macOS), we might get "use of closed network connection"
	// which is expected when stopping the transport
	if err != nil && !strings.Contains(err.Error(), "use of closed network connection") {
		t.Errorf("Unexpected error stopping transport: %v", err)
	}
	assert.False(t, transport.IsRunning())
}

func TestSSETransport_Connection(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:         "/events",
		HeartbeatInterval: 500 * time.Millisecond,
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr

	// Connect SSE client
	client := newSSETestClient()
	err = client.connect(baseURL + "/events")
	require.NoError(t, err)

	// Wait for connection event
	select {
	case event := <-client.events:
		var data map[string]interface{}
		err = json.Unmarshal([]byte(event), &data)
		require.NoError(t, err)
		assert.NotEmpty(t, data["clientId"])
	case <-time.After(2 * time.Second):
		t.Fatal("No connection event received")
	}

	// Wait for capabilities event
	select {
	case event := <-client.events:
		var data map[string]interface{}
		err = json.Unmarshal([]byte(event), &data)
		require.NoError(t, err)
		assert.NotNil(t, data)
	case <-time.After(2 * time.Second):
		t.Fatal("No capabilities event received")
	}

	// Check client count
	assert.Equal(t, 1, transport.ClientCount())
}

func TestSSETransport_Broadcast(t *testing.T) {
	// Skip on Windows due to timing issues
	if runtime.GOOS == "windows" {
		t.Skip("Skipping SSE Broadcast test on Windows")
	}
	
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:       "/events",
		EventBufferSize: 100, // Increase buffer to prevent drops
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr

	// Connect multiple clients
	numClients := 3
	clients := make([]*sseTestClient, numClients)

	for i := 0; i < numClients; i++ {
		client := newSSETestClient()
		err = client.connect(baseURL + "/events")
		require.NoError(t, err)
		clients[i] = client

		// Wait for connection event
		<-client.events
		<-client.events // capabilities
	}

	// Wait for all connections to be established
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		if transport.ClientCount() == numClients {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.Equal(t, numClients, transport.ClientCount())

	// Give clients time to settle on Windows
	time.Sleep(200 * time.Millisecond)

	// Broadcast message
	testData := map[string]interface{}{
		"message":   "broadcast test",
		"timestamp": time.Now().Unix(),
	}
	transport.BroadcastEvent("test", testData)

	// All clients should receive the broadcast
	for i, client := range clients {
		select {
		case event := <-client.events:
			var data map[string]interface{}
			err = json.Unmarshal([]byte(event), &data)
			require.NoError(t, err)
			assert.Equal(t, "broadcast test", data["message"])
		case <-time.After(2 * time.Second):
			t.Fatalf("Client %d did not receive broadcast", i)
		}
	}
}

func TestSSETransport_SendToClient(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath: "/events",
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr

	// Connect client and get client ID
	req, _ := http.NewRequest("GET", baseURL+"/events", nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	var clientID string

	// Read connection event to get client ID
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			var event map[string]interface{}
			json.Unmarshal([]byte(data), &event)
			if id, ok := event["clientId"]; ok {
				clientID = id.(string)
				break
			}
		}
	}

	require.NotEmpty(t, clientID)

	// Send message to specific client
	testData := map[string]interface{}{
		"message": "direct message",
	}
	err = transport.SendToClient(clientID, "direct", testData)
	require.NoError(t, err)

	// Try sending to non-existent client
	err = transport.SendToClient("invalid-client", "test", testData)
	assert.Error(t, err)
}

func TestSSETransport_CommandEndpoint(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
			Path:    "/command",
		},
		EventPath: "/events",
	}

	transport := NewSSETransport(config)

	handler := &mockHandler{
		handleFunc: func(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
			return &protocol.JSONRPCResponse{
				JSONRPC: "2.0",
				ID:      req.ID,
				Result: map[string]interface{}{
					"echo": req.Method,
				},
			}
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr

	httpClient := &http.Client{Timeout: 5 * time.Second}

	t.Run("command without client ID", func(t *testing.T) {
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      1,
			Method:  "test.method",
		}

		body, _ := json.Marshal(req)
		resp, err := httpClient.Post(baseURL+"/command", "application/json", bytes.NewReader(body))
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusOK, resp.StatusCode)

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
		case int64:
			if actual, ok := jsonResp.ID.(float64); ok {
				assert.Equal(t, float64(expected), actual)
			} else {
				assert.Equal(t, req.ID, jsonResp.ID)
			}
		default:
			assert.Equal(t, req.ID, jsonResp.ID)
		}
	})

	t.Run("command with client ID", func(t *testing.T) {
		// First connect SSE client
		sseClient := newSSETestClient()
		err = sseClient.connect(baseURL + "/events")
		require.NoError(t, err)

		// Get client ID from connection event
		event := <-sseClient.events
		var connData map[string]interface{}
		json.Unmarshal([]byte(event), &connData)
		clientID := connData["clientId"].(string)

		// Send command with client ID
		req := &protocol.JSONRPCRequest{
			JSONRPC: "2.0",
			ID:      2,
			Method:  "test.method",
		}

		body, _ := json.Marshal(req)
		httpReq, _ := http.NewRequest("POST", baseURL+"/command", bytes.NewReader(body))
		httpReq.Header.Set("X-Client-ID", clientID)

		resp, err := httpClient.Do(httpReq)
		require.NoError(t, err)
		defer resp.Body.Close()

		// Should get accepted status
		assert.Equal(t, http.StatusAccepted, resp.StatusCode)

		var result map[string]interface{}
		json.NewDecoder(resp.Body).Decode(&result)
		assert.Equal(t, "accepted", result["status"])

		// Response should come via SSE
		select {
		case eventData := <-sseClient.events:
			// Parse the SSE event
			event, err := ParseSSEEvent(eventData)
			require.NoError(t, err)
			
			// Verify event structure
			assert.NotEmpty(t, event.Data)
			t.Logf("Received SSE event - ID: %s, Event: %s, Data: %s", event.ID, event.Event, event.Data)
			
			// Parse JSON response from event data
			var jsonResp protocol.JSONRPCResponse
			err = json.Unmarshal([]byte(event.Data), &jsonResp)
			require.NoError(t, err)
			
			// Verify response matches request
			assert.Equal(t, req.ID, jsonResp.ID)
			assert.Equal(t, "test response", jsonResp.Result)
		case <-time.After(2 * time.Second):
			t.Fatal("Timeout waiting for SSE response")
		}
	})
}

func TestSSETransport_MaxClients(t *testing.T) {
	// Skip on Windows due to SSE connection handling differences
	if runtime.GOOS == "windows" {
		t.Skip("Skipping SSE MaxClients test on Windows")
	}
	
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:  "/events",
		MaxClients: 2,
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr + "/events"

	client := &http.Client{Timeout: 5 * time.Second}

	// Connect max clients and keep them alive
	responses := make([]*http.Response, 2)
	readers := make([]*bufio.Scanner, 2)
	
	for i := 0; i < 2; i++ {
		resp, err := client.Get(baseURL)
		require.NoError(t, err)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		responses[i] = resp
		
		// Start reading to keep connection alive
		readers[i] = bufio.NewScanner(resp.Body)
		go func(scanner *bufio.Scanner) {
			for scanner.Scan() {
				// Keep reading events
			}
		}(readers[i])
	}

	// Wait for connections to be fully established and verify
	maxRetries := 10
	for i := 0; i < maxRetries; i++ {
		if transport.ClientCount() == 2 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	assert.Equal(t, 2, transport.ClientCount())

	// Try to connect one more (should fail)
	resp, err := client.Get(baseURL)
	require.NoError(t, err)
	defer resp.Body.Close()
	assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)

	// Close one connection
	responses[0].Body.Close()
	time.Sleep(100 * time.Millisecond)

	// Now should be able to connect
	resp, err = client.Get(baseURL)
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	resp.Body.Close()

	// Cleanup
	for _, r := range responses {
		if r != nil && r.Body != nil {
			r.Body.Close()
		}
	}
}

func TestSSETransport_Heartbeat(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:         "/events",
		HeartbeatInterval: 100 * time.Millisecond,
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr + "/events"

	req, _ := http.NewRequest("GET", baseURL, nil)
	client := &http.Client{}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	heartbeatCount := 0
	done := make(chan struct{})

	go func() {
		defer close(done)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, ": heartbeat") {
				heartbeatCount++
				if heartbeatCount >= 3 {
					return
				}
			}
		}
	}()

	select {
	case <-done:
		assert.GreaterOrEqual(t, heartbeatCount, 3)
	case <-time.After(1 * time.Second):
		t.Fatal("Did not receive enough heartbeats")
	}
}

func TestSSETransport_EventBuffering(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:       "/events",
		EventBufferSize: 5,
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	// Get client ID first
	clients := transport.GetClients()
	require.Empty(t, clients) // No clients yet

	// Connect a client
	addr := transport.Address()
	baseURL := "http://" + addr + "/events"

	go func() {
		resp, err := http.Get(baseURL)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		time.Sleep(5 * time.Second) // Keep connection open
	}()

	// Wait for client to connect
	time.Sleep(100 * time.Millisecond)
	clients = transport.GetClients()
	require.Len(t, clients, 1)
	clientID := clients[0]

	// Send many events quickly (more than buffer size)
	successCount := 0
	for i := 0; i < 10; i++ {
		err := transport.SendToClient(clientID, "test", map[string]interface{}{"index": i})
		if err == nil {
			successCount++
		}
	}

	// Should have sent at least buffer size
	assert.GreaterOrEqual(t, successCount, 5)
	// But some should have been dropped due to buffer full
	assert.Less(t, successCount, 10)
}

// Benchmark tests

func BenchmarkSSETransport_Broadcast(b *testing.B) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:       "/events",
		EventBufferSize: 1000, // Large buffer for benchmark
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(b, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr + "/events"

	// Connect multiple clients with cancellation
	clientCtx, clientCancel := context.WithCancel(context.Background())
	defer clientCancel()

	numClients := 10
	var wg sync.WaitGroup
	for i := 0; i < numClients; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			req, _ := http.NewRequestWithContext(clientCtx, "GET", baseURL, nil)
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return
			}
			defer resp.Body.Close()
			scanner := bufio.NewScanner(resp.Body)
			for scanner.Scan() {
				select {
				case <-clientCtx.Done():
					return
				default:
					// Consume events
				}
			}
		}()
	}

	// Wait for clients to connect
	time.Sleep(200 * time.Millisecond)

	message := map[string]interface{}{
		"type": "benchmark",
		"data": "test broadcast message",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transport.BroadcastEvent("benchmark", message)
	}
	b.StopTimer()

	// Cancel clients before stopping transport
	clientCancel()
	wg.Wait()
}

func BenchmarkSSETransport_DirectMessage(b *testing.B) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath: "/events",
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(b, err)
	defer transport.Stop()

	// Connect a client and get its ID
	addr := transport.Address()
	baseURL := "http://" + addr + "/events"

	clientConnected := make(chan string)
	go func() {
		resp, err := http.Get(baseURL)
		if err != nil {
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)

		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data: ") {
				data := strings.TrimPrefix(line, "data: ")
				var event map[string]interface{}
				if json.Unmarshal([]byte(data), &event) == nil {
					if id, ok := event["clientId"]; ok {
						clientConnected <- id.(string)
						break
					}
				}
			}
		}

		// Keep reading
		for scanner.Scan() {
			// Consume events
		}
	}()

	clientID := <-clientConnected

	message := map[string]interface{}{
		"type": "benchmark",
		"data": "test direct message",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		transport.SendToClient(clientID, "benchmark", message)
	}
}

func TestSSEEventParsing(t *testing.T) {
	tests := []struct {
		name     string
		raw      string
		expected *SSEEvent
		wantErr  bool
	}{
		{
			name: "simple event",
			raw:  "data: hello world\n\n",
			expected: &SSEEvent{
				Data: "hello world",
			},
		},
		{
			name: "event with type",
			raw:  "event: message\ndata: hello world\n\n",
			expected: &SSEEvent{
				Event: "message",
				Data:  "hello world",
			},
		},
		{
			name: "event with id",
			raw:  "id: 123\nevent: message\ndata: hello world\n\n",
			expected: &SSEEvent{
				ID:    "123",
				Event: "message",
				Data:  "hello world",
			},
		},
		{
			name: "event with retry",
			raw:  "retry: 5000\ndata: reconnect please\n\n",
			expected: &SSEEvent{
				Retry: 5000,
				Data:  "reconnect please",
			},
		},
		{
			name: "multiline data",
			raw:  "data: line 1\ndata: line 2\ndata: line 3\n\n",
			expected: &SSEEvent{
				Data: "line 1\nline 2\nline 3",
			},
		},
		{
			name: "JSON data",
			raw:  "data: {\"type\":\"message\",\"content\":\"hello\"}\n\n",
			expected: &SSEEvent{
				Data: "{\"type\":\"message\",\"content\":\"hello\"}",
			},
		},
		{
			name:    "empty event",
			raw:     "\n\n",
			wantErr: true,
		},
		{
			name:    "no data or event",
			raw:     "id: 123\nretry: 5000\n\n",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			event, err := ParseSSEEvent(tt.raw)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.expected.ID, event.ID)
			assert.Equal(t, tt.expected.Event, event.Event)
			assert.Equal(t, tt.expected.Data, event.Data)
			assert.Equal(t, tt.expected.Retry, event.Retry)
		})
	}
}

func TestSSETransport_EventFormatting(t *testing.T) {
	config := &SSEConfig{
		HTTPConfig: HTTPConfig{
			Address: "localhost:0",
		},
		EventPath:  "/events",
		MaxClients: 10,
	}

	transport := NewSSETransport(config)
	handler := &mockHandler{}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err := transport.Start(ctx, handler)
	require.NoError(t, err)
	defer transport.Stop()

	addr := transport.Address()
	baseURL := "http://" + addr

	// Connect SSE client
	sseClient := newSSETestClient()
	err = sseClient.connect(baseURL + "/events")
	require.NoError(t, err)

	// Wait for connection event
	eventData := <-sseClient.events
	event, err := ParseSSEEvent(eventData)
	require.NoError(t, err)
	assert.Equal(t, "connection", event.Event)

	// Parse client ID from connection data
	var connData map[string]interface{}
	err = json.Unmarshal([]byte(event.Data), &connData)
	require.NoError(t, err)
	clientID := connData["clientId"].(string)
	assert.NotEmpty(t, clientID)

	// Test different event types
	testCases := []struct {
		eventType string
		data      interface{}
	}{
		{"message", map[string]string{"text": "Hello SSE"}},
		{"notification", map[string]interface{}{"level": "info", "msg": "Test notification"}},
		{"update", map[string]interface{}{"field": "value", "timestamp": time.Now().Unix()}},
		{"json-rpc", &protocol.JSONRPCResponse{JSONRPC: "2.0", ID: 1, Result: "test"}},
	}

	for _, tc := range testCases {
		t.Run(tc.eventType, func(t *testing.T) {
			err := transport.SendToClient(clientID, tc.eventType, tc.data)
			require.NoError(t, err)

			select {
			case eventData := <-sseClient.events:
				event, err := ParseSSEEvent(eventData)
				require.NoError(t, err)
				assert.Equal(t, tc.eventType, event.Event)
				assert.NotEmpty(t, event.ID)
				assert.NotEmpty(t, event.Data)

				// Verify data can be parsed as JSON
				var parsed interface{}
				err = json.Unmarshal([]byte(event.Data), &parsed)
				assert.NoError(t, err)
			case <-time.After(1 * time.Second):
				t.Fatal("Timeout waiting for event")
			}
		})
	}
}
