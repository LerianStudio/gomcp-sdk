// Package testutil provides testing utilities for MCP servers
package testutil

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/fredcamaral/gomcp-sdk/protocol"
	"io"
	"sync"
	"sync/atomic"
	"time"
)

// TestClient provides a test client for MCP servers
type TestClient struct {
	// Pipes for bidirectional communication
	clientReader io.ReadCloser  // Client reads from this (server writes to this)
	clientWriter io.WriteCloser // Client writes to this (server reads from this)
	serverReader io.ReadCloser  // Server reads from this (client writes to this)
	serverWriter io.WriteCloser // Server writes to this (client reads from this)

	encoder   *json.Encoder
	decoder   *json.Decoder
	responses chan *protocol.JSONRPCResponse
	errors    chan error
	requestID atomic.Int64
	mu        sync.Mutex
	closed    bool
	done      chan struct{}
	readWg    sync.WaitGroup
}

// NewTestClient creates a new test client
func NewTestClient() *TestClient {
	// Create two pipes for bidirectional communication
	// Pipe 1: Client writes, Server reads
	serverFromClient, clientToServer := io.Pipe()
	// Pipe 2: Server writes, Client reads
	clientFromServer, serverToClient := io.Pipe()

	client := &TestClient{
		clientReader: clientFromServer,
		clientWriter: clientToServer,
		serverReader: serverFromClient,
		serverWriter: serverToClient,
		responses:    make(chan *protocol.JSONRPCResponse, 100),
		errors:       make(chan error, 100),
		done:         make(chan struct{}),
	}

	client.encoder = json.NewEncoder(client.clientWriter)
	client.decoder = json.NewDecoder(client.clientReader)

	return client
}

// GetServerInput returns the input buffer (what the server reads from)
func (c *TestClient) GetServerInput() io.Reader {
	return c.serverReader
}

// GetServerOutput returns the output buffer (what the server writes to)
func (c *TestClient) GetServerOutput() io.Writer {
	return c.serverWriter
}

// SendRequest sends a request to the server
func (c *TestClient) SendRequest(method string, params interface{}) (int64, error) {
	// Check if closed and get next ID under lock
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return 0, fmt.Errorf("client is closed")
	}
	id := c.requestID.Add(1)
	c.mu.Unlock()

	req := protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	// Debug: log the request being sent
	// fmt.Printf("[TestClient] Sending request: id=%d method=%s\n", id, method)

	// Encode without holding the lock to avoid blocking
	if err := c.encoder.Encode(req); err != nil {
		return 0, fmt.Errorf("encoding request: %w", err)
	}

	return id, nil
}

// SendNotification sends a notification (no ID) to the server
func (c *TestClient) SendNotification(method string, params interface{}) error {
	// Check if closed under lock
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return fmt.Errorf("client is closed")
	}
	c.mu.Unlock()

	req := protocol.JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}

	// Encode without holding the lock
	if err := c.encoder.Encode(req); err != nil {
		return fmt.Errorf("encoding notification: %w", err)
	}

	return nil
}

// ReadResponses reads responses from the server output
func (c *TestClient) ReadResponses(ctx context.Context) {
	// Debug: log when reader starts
	// fmt.Println("[TestClient] ReadResponses started")
	c.readWg.Add(1)
	defer func() {
		c.readWg.Done()
		// fmt.Println("[TestClient] ReadResponses stopped")
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-c.done:
			return
		default:
			var resp protocol.JSONRPCResponse
			if err := c.decoder.Decode(&resp); err != nil {
				if err != io.EOF && err != io.ErrClosedPipe {
					// fmt.Printf("[TestClient] Decode error: %v\n", err)
					select {
					case c.errors <- err:
					case <-ctx.Done():
						return
					case <-c.done:
						return
					}
				}
				return
			}

			// Debug: log received response
			// fmt.Printf("[TestClient] Received response: id=%v method result=%v error=%v\n", resp.ID, resp.Result != nil, resp.Error)

			select {
			case c.responses <- &resp:
			case <-ctx.Done():
				return
			case <-c.done:
				return
			}
		}
	}
}

// WaitForResponse waits for a response with the given ID
func (c *TestClient) WaitForResponse(ctx context.Context, id int64) (*protocol.JSONRPCResponse, error) {
	timeout := time.After(5 * time.Second)

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-c.done:
			return nil, fmt.Errorf("client is closing")
		case <-timeout:
			return nil, fmt.Errorf("timeout waiting for response")
		case err := <-c.errors:
			return nil, err
		case resp := <-c.responses:
			if resp.ID == id || resp.ID == float64(id) { // Handle both int and float64
				return resp, nil
			}
			// Put it back for other waiters
			c.mu.Lock()
			if !c.closed {
				select {
				case c.responses <- resp:
				default:
				}
			}
			c.mu.Unlock()
		}
	}
}

// GetNextResponse gets the next response without waiting for a specific ID
func (c *TestClient) GetNextResponse(ctx context.Context) (*protocol.JSONRPCResponse, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case err := <-c.errors:
		return nil, err
	case resp := <-c.responses:
		return resp, nil
	case <-time.After(5 * time.Second):
		return nil, fmt.Errorf("timeout waiting for response")
	}
}

// Initialize sends an initialization request and waits for response
func (c *TestClient) Initialize(ctx context.Context, clientName, clientVersion string) (*protocol.InitializeResult, error) {
	params := protocol.InitializeRequest{
		ProtocolVersion: protocol.Version,
		Capabilities:    protocol.ClientCapabilities{},
		ClientInfo: protocol.ClientInfo{
			Name:    clientName,
			Version: clientVersion,
		},
	}

	id, err := c.SendRequest("initialize", params)
	if err != nil {
		return nil, err
	}

	resp, err := c.WaitForResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	var result protocol.InitializeResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling result: %w", err)
	}

	return &result, nil
}

// ListTools sends a tools/list request
func (c *TestClient) ListTools(ctx context.Context) ([]protocol.Tool, error) {
	id, err := c.SendRequest("tools/list", nil)
	if err != nil {
		return nil, err
	}

	resp, err := c.WaitForResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/list error: %s", resp.Error.Message)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	var result struct {
		Tools []protocol.Tool `json:"tools"`
	}
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling result: %w", err)
	}

	return result.Tools, nil
}

// CallTool sends a tools/call request
func (c *TestClient) CallTool(ctx context.Context, name string, args map[string]interface{}) (*protocol.ToolCallResult, error) {
	params := protocol.ToolCallRequest{
		Name:      name,
		Arguments: args,
	}

	id, err := c.SendRequest("tools/call", params)
	if err != nil {
		return nil, err
	}

	resp, err := c.WaitForResponse(ctx, id)
	if err != nil {
		return nil, err
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("tools/call error: %s", resp.Error.Message)
	}

	// Parse result
	resultBytes, err := json.Marshal(resp.Result)
	if err != nil {
		return nil, fmt.Errorf("marshaling result: %w", err)
	}

	var result protocol.ToolCallResult
	if err := json.Unmarshal(resultBytes, &result); err != nil {
		return nil, fmt.Errorf("unmarshaling result: %w", err)
	}

	return &result, nil
}

// Close closes the client
func (c *TestClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	close(c.done)
	c.mu.Unlock()

	// Close all pipes to trigger EOF in ReadResponses
	c.clientReader.Close()
	c.clientWriter.Close()
	c.serverReader.Close()
	c.serverWriter.Close()

	// Wait for ReadResponses to finish
	c.readWg.Wait()

	// Now safe to close the channels
	close(c.responses)
	close(c.errors)

	return nil
}

// ClientBuilder provides a fluent API for building test scenarios
type ClientBuilder struct {
	client  *TestClient
	context context.Context
}

// NewClientBuilder creates a new client builder
func NewClientBuilder(ctx context.Context) *ClientBuilder {
	return &ClientBuilder{
		client:  NewTestClient(),
		context: ctx,
	}
}

// WithInitialization adds initialization to the scenario
func (b *ClientBuilder) WithInitialization(name, version string) *ClientBuilder {
	go func() {
		if _, err := b.client.Initialize(b.context, name, version); err != nil {
			b.client.errors <- err
		}
	}()
	return b
}

// SendRequest adds a request to the scenario
func (b *ClientBuilder) SendRequest(method string, params interface{}) *ClientBuilder {
	go func() {
		if _, err := b.client.SendRequest(method, params); err != nil {
			b.client.errors <- err
		}
	}()
	return b
}

// Build returns the configured client
func (b *ClientBuilder) Build() *TestClient {
	return b.client
}
