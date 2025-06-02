// Package transport implements MCP transport layers
package transport

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"io"
	"os"
	"sync"
)

// StdioTransport implements the Transport interface using stdio
type StdioTransport struct {
	input   io.Reader
	output  io.Writer
	scanner *bufio.Scanner
	encoder *json.Encoder
	mutex   sync.Mutex
	running bool
}

// NewStdioTransport creates a new stdio transport
func NewStdioTransport() *StdioTransport {
	return &StdioTransport{
		input:   os.Stdin,
		output:  os.Stdout,
		scanner: bufio.NewScanner(os.Stdin),
		encoder: json.NewEncoder(os.Stdout),
	}
}

// NewStdioTransportWithIO creates a new stdio transport with custom IO
func NewStdioTransportWithIO(input io.Reader, output io.Writer) *StdioTransport {
	return &StdioTransport{
		input:   input,
		output:  output,
		scanner: bufio.NewScanner(input),
		encoder: json.NewEncoder(output),
	}
}

// Start starts the stdio transport
func (t *StdioTransport) Start(ctx context.Context, handler RequestHandler) error {
	t.mutex.Lock()
	if t.running {
		t.mutex.Unlock()
		return fmt.Errorf("transport already running")
	}
	t.running = true
	t.mutex.Unlock()

	defer func() {
		t.mutex.Lock()
		t.running = false
		t.mutex.Unlock()
	}()

	// Create channels for communication
	type scanResult struct {
		line string
		err  error
	}
	scanChan := make(chan scanResult, 10) // Buffer to prevent blocking

	// Start scanner in a goroutine
	go func() {
		for t.scanner.Scan() {
			select {
			case scanChan <- scanResult{line: t.scanner.Text()}:
			case <-ctx.Done():
				return
			}
		}
		if err := t.scanner.Err(); err != nil {
			scanChan <- scanResult{err: err}
		} else {
			// EOF
			close(scanChan)
		}
	}()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case result, ok := <-scanChan:
			if !ok {
				// Channel closed, EOF reached
				return nil
			}

			if result.err != nil {
				return fmt.Errorf("scanning input: %w", result.err)
			}

			line := result.line
			if line == "" {
				continue
			}

			// Parse message flexibly to handle both requests and error responses
			parsed, parseErr := protocol.ParseJSONRPCMessage([]byte(line))
			if parseErr != nil {
				// Send error response for parse failures
				if jsonRPCErr, ok := parseErr.(*protocol.JSONRPCError); ok {
					errResp := &protocol.JSONRPCResponse{
						JSONRPC: "2.0",
						Error:   jsonRPCErr,
					}
					if err := t.sendResponse(errResp); err != nil {
						return fmt.Errorf("failed to send error response: %w", err)
					}
				}
				continue
			}

			// Handle requests normally
			if parsed.Request != nil {
				resp := handler.HandleRequest(ctx, parsed.Request)
				if resp != nil {
					if err := t.sendResponse(resp); err != nil {
						return fmt.Errorf("sending response: %w", err)
					}
				}
			} else if parsed.Response != nil && parsed.IsError {
				// Claude Desktop sent an error response - log it for debugging but don't crash
				// This might be a protocol negotiation error or a client-side error
				// In a real implementation, you might want to handle this differently
				continue
			}
		}
	}
}

// Stop stops the stdio transport
func (t *StdioTransport) Stop() error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	t.running = false
	return nil
}

// sendResponse sends a JSON-RPC response
func (t *StdioTransport) sendResponse(resp *protocol.JSONRPCResponse) error {
	t.mutex.Lock()
	defer t.mutex.Unlock()

	if err := t.encoder.Encode(resp); err != nil {
		return fmt.Errorf("encoding response: %w", err)
	}

	return nil
}

// IsRunning returns whether the transport is running
func (t *StdioTransport) IsRunning() bool {
	t.mutex.Lock()
	defer t.mutex.Unlock()
	return t.running
}
