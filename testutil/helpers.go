package testutil

import (
	"context"
	"testing"
	"time"
)

// StartServerWithInit starts a test server and initializes it
func StartServerWithInit(t *testing.T, srv *TestServer, clientName, clientVersion string) {
	t.Helper()
	
	// Start the server
	if err := srv.Start(); err != nil {
		t.Fatalf("Failed to start server: %v", err)
	}
	
	// Give server more time to fully start
	time.Sleep(200 * time.Millisecond)
	
	// Initialize the client with retry
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	
	var initErr error
	for i := 0; i < 20; i++ {
		_, initErr = srv.Client().Initialize(ctx, clientName, clientVersion)
		if initErr == nil {
			break
		}
		if ctx.Err() != nil {
			break // Context cancelled or timed out
		}
		time.Sleep(100 * time.Millisecond)
	}
	
	if initErr != nil {
		t.Fatalf("Failed to initialize after retries: %v", initErr)
	}
}