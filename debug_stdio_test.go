package mcp_test

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"testing"
	"time"
)

// TestStdioPipeCommunication tests basic pipe communication
func TestStdioPipeCommunication(t *testing.T) {
	// Create pipes
	serverFromClient, clientToServer := io.Pipe()
	clientFromServer, serverToClient := io.Pipe()

	// Test data
	testMessage := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  "test",
	}

	// Server side - read and echo back
	serverDone := make(chan error, 1)
	go func() {
		decoder := json.NewDecoder(serverFromClient)
		encoder := json.NewEncoder(serverToClient)

		var msg map[string]interface{}
		if err := decoder.Decode(&msg); err != nil {
			serverDone <- fmt.Errorf("server decode error: %w", err)
			return
		}

		// Echo back with result
		response := map[string]interface{}{
			"jsonrpc": "2.0",
			"id":      msg["id"],
			"result":  "echo: " + msg["method"].(string),
		}

		if err := encoder.Encode(response); err != nil {
			serverDone <- fmt.Errorf("server encode error: %w", err)
			return
		}

		serverDone <- nil
	}()

	// Client side - send and receive
	clientDone := make(chan error, 1)
	go func() {
		encoder := json.NewEncoder(clientToServer)
		decoder := json.NewDecoder(clientFromServer)

		// Send message
		if err := encoder.Encode(testMessage); err != nil {
			clientDone <- fmt.Errorf("client encode error: %w", err)
			return
		}

		// Receive response
		var response map[string]interface{}
		if err := decoder.Decode(&response); err != nil {
			clientDone <- fmt.Errorf("client decode error: %w", err)
			return
		}

		// Verify response
		if response["id"] != float64(1) { // JSON numbers decode as float64
			clientDone <- fmt.Errorf("wrong response ID: %v", response["id"])
			return
		}

		if response["result"] != "echo: test" {
			clientDone <- fmt.Errorf("wrong response result: %v", response["result"])
			return
		}

		clientDone <- nil
	}()

	// Wait for both sides to complete
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	select {
	case err := <-serverDone:
		if err != nil {
			t.Fatalf("Server error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Server timeout")
	}

	select {
	case err := <-clientDone:
		if err != nil {
			t.Fatalf("Client error: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Client timeout")
	}

	// Clean up
	if err := clientToServer.Close(); err != nil {
		t.Errorf("Failed to close clientToServer pipe: %v", err)
	}
	if err := serverToClient.Close(); err != nil {
		t.Errorf("Failed to close serverToClient pipe: %v", err)
	}
	if err := serverFromClient.Close(); err != nil {
		t.Errorf("Failed to close serverFromClient pipe: %v", err)
	}
	if err := clientFromServer.Close(); err != nil {
		t.Errorf("Failed to close clientFromServer pipe: %v", err)
	}
}
