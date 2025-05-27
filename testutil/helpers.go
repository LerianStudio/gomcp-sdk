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
	
	// Initialize the client with retry
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	
	var initErr error
	for i := 0; i < 10; i++ {
		_, initErr = srv.Client().Initialize(ctx, clientName, clientVersion)
		if initErr == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	
	if initErr != nil {
		t.Fatalf("Failed to initialize after retries: %v", initErr)
	}
}