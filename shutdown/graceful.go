// Package shutdown provides graceful shutdown capabilities for MCP transports
package shutdown

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// ShutdownConfig configures graceful shutdown behavior
type ShutdownConfig struct {
	// Timeout for graceful shutdown
	Timeout time.Duration

	// Grace period to allow in-flight requests to complete
	GracePeriod time.Duration

	// Whether to drain connections during shutdown
	DrainConnections bool

	// Maximum time to wait for connection draining
	DrainTimeout time.Duration

	// Callback functions to execute during shutdown phases
	OnShutdownStart func()
	OnGracePeriod   func()
	OnForceStop     func()
	OnComplete      func()
}

// DefaultShutdownConfig returns default graceful shutdown configuration
func DefaultShutdownConfig() *ShutdownConfig {
	return &ShutdownConfig{
		Timeout:          30 * time.Second,
		GracePeriod:      10 * time.Second,
		DrainConnections: true,
		DrainTimeout:     20 * time.Second,
	}
}

// GracefulShutdown manages graceful shutdown of multiple services
type GracefulShutdown struct {
	config    *ShutdownConfig
	services  []ShutdownHandler
	mu        sync.RWMutex
	isRunning bool
}

// ShutdownHandler defines the interface for services that support graceful shutdown
type ShutdownHandler interface {
	// Stop initiates graceful shutdown and returns when complete
	Stop() error

	// ForceStop forces immediate shutdown
	ForceStop() error

	// IsRunning returns whether the service is currently running
	IsRunning() bool

	// ConnectionCount returns the number of active connections (optional)
	ConnectionCount() int
}

// NewGracefulShutdown creates a new graceful shutdown manager
func NewGracefulShutdown(config *ShutdownConfig) *GracefulShutdown {
	if config == nil {
		config = DefaultShutdownConfig()
	}

	return &GracefulShutdown{
		config:    config,
		services:  make([]ShutdownHandler, 0),
		isRunning: true,
	}
}

// Register adds a service to be managed by graceful shutdown
func (gs *GracefulShutdown) Register(service ShutdownHandler) {
	gs.mu.Lock()
	defer gs.mu.Unlock()
	gs.services = append(gs.services, service)
}

// Shutdown performs graceful shutdown of all registered services
func (gs *GracefulShutdown) Shutdown(ctx context.Context) error {
	gs.mu.Lock()
	defer gs.mu.Unlock()

	if !gs.isRunning {
		return nil
	}

	gs.isRunning = false

	// Execute shutdown start callback
	if gs.config.OnShutdownStart != nil {
		gs.config.OnShutdownStart()
	}

	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(ctx, gs.config.Timeout)
	defer cancel()

	// Phase 1: Stop accepting new connections
	fmt.Println("🛑 Graceful shutdown initiated - stopping new connections...")

	// Phase 2: Grace period for in-flight requests
	if gs.config.GracePeriod > 0 {
		fmt.Printf("⏳ Grace period: allowing %v for in-flight requests...\n", gs.config.GracePeriod)

		if gs.config.OnGracePeriod != nil {
			gs.config.OnGracePeriod()
		}

		gracePeriodCtx, graceCancel := context.WithTimeout(shutdownCtx, gs.config.GracePeriod)
		gs.waitForRequestsToComplete(gracePeriodCtx)
		graceCancel()
	}

	// Phase 3: Drain connections if enabled
	if gs.config.DrainConnections {
		fmt.Printf("🔄 Draining connections (timeout: %v)...\n", gs.config.DrainTimeout)
		drainCtx, drainCancel := context.WithTimeout(shutdownCtx, gs.config.DrainTimeout)
		gs.drainConnections(drainCtx)
		drainCancel()
	}

	// Phase 4: Shutdown services
	fmt.Println("🔌 Shutting down services...")
	errors := gs.shutdownServices(shutdownCtx)

	// Phase 5: Force stop if needed
	if len(errors) > 0 {
		fmt.Println("⚠️  Some services failed to shutdown gracefully, forcing stop...")

		if gs.config.OnForceStop != nil {
			gs.config.OnForceStop()
		}

		forceErrors := gs.forceStopServices()
		errors = append(errors, forceErrors...)
	}

	// Execute completion callback
	if gs.config.OnComplete != nil {
		gs.config.OnComplete()
	}

	if len(errors) > 0 {
		fmt.Printf("❌ Shutdown completed with %d errors\n", len(errors))
		return fmt.Errorf("shutdown completed with errors: %v", errors)
	}

	fmt.Println("✅ Graceful shutdown completed successfully")
	return nil
}

// waitForRequestsToComplete waits for in-flight requests to complete
func (gs *GracefulShutdown) waitForRequestsToComplete(ctx context.Context) {
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			totalConnections := 0
			for _, service := range gs.services {
				totalConnections += service.ConnectionCount()
			}

			if totalConnections == 0 {
				fmt.Println("✅ All in-flight requests completed")
				return
			}

			fmt.Printf("⏳ Waiting for %d active connections...\n", totalConnections)
		}
	}
}

// drainConnections drains existing connections
func (gs *GracefulShutdown) drainConnections(ctx context.Context) {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			fmt.Println("⏰ Connection draining timeout reached")
			return
		case <-ticker.C:
			totalConnections := 0
			for _, service := range gs.services {
				totalConnections += service.ConnectionCount()
			}

			if totalConnections == 0 {
				fmt.Println("✅ All connections drained")
				return
			}

			fmt.Printf("🔄 Draining %d remaining connections...\n", totalConnections)
		}
	}
}

// shutdownServices attempts graceful shutdown of all services
func (gs *GracefulShutdown) shutdownServices(ctx context.Context) []error {
	var errors []error
	var wg sync.WaitGroup
	errorChan := make(chan error, len(gs.services))

	// Shutdown services concurrently
	for i, service := range gs.services {
		wg.Add(1)
		go func(index int, svc ShutdownHandler) {
			defer wg.Done()

			fmt.Printf("🔌 Shutting down service %d...\n", index+1)

			done := make(chan error, 1)
			go func() {
				done <- svc.Stop()
			}()

			select {
			case err := <-done:
				if err != nil {
					fmt.Printf("❌ Service %d shutdown error: %v\n", index+1, err)
					errorChan <- fmt.Errorf("service %d: %w", index+1, err)
				} else {
					fmt.Printf("✅ Service %d shutdown successfully\n", index+1)
				}
			case <-ctx.Done():
				fmt.Printf("⏰ Service %d shutdown timeout\n", index+1)
				errorChan <- fmt.Errorf("service %d: shutdown timeout", index+1)
			}
		}(i, service)
	}

	wg.Wait()
	close(errorChan)

	for err := range errorChan {
		errors = append(errors, err)
	}

	return errors
}

// forceStopServices forces immediate stop of all services
func (gs *GracefulShutdown) forceStopServices() []error {
	var errors []error

	for i, service := range gs.services {
		if service.IsRunning() {
			fmt.Printf("⚡ Force stopping service %d...\n", i+1)
			if err := service.ForceStop(); err != nil {
				fmt.Printf("❌ Force stop failed for service %d: %v\n", i+1, err)
				errors = append(errors, fmt.Errorf("force stop service %d: %w", i+1, err))
			}
		}
	}

	return errors
}

// ConnectionTracker helps track active connections for graceful shutdown
type ConnectionTracker struct {
	mu          sync.RWMutex
	connections map[string]*Connection
	counter     int64
}

// Connection represents an active connection
type Connection struct {
	ID         string
	StartTime  time.Time
	Type       string
	RemoteAddr string
}

// NewConnectionTracker creates a new connection tracker
func NewConnectionTracker() *ConnectionTracker {
	return &ConnectionTracker{
		connections: make(map[string]*Connection),
	}
}

// AddConnection tracks a new connection
func (ct *ConnectionTracker) AddConnection(connType, remoteAddr string) string {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	ct.counter++
	id := fmt.Sprintf("%s-%d", connType, ct.counter)

	ct.connections[id] = &Connection{
		ID:         id,
		StartTime:  time.Now(),
		Type:       connType,
		RemoteAddr: remoteAddr,
	}

	return id
}

// RemoveConnection removes a tracked connection
func (ct *ConnectionTracker) RemoveConnection(id string) {
	ct.mu.Lock()
	defer ct.mu.Unlock()
	delete(ct.connections, id)
}

// Count returns the number of active connections
func (ct *ConnectionTracker) Count() int {
	ct.mu.RLock()
	defer ct.mu.RUnlock()
	return len(ct.connections)
}

// GetConnections returns a copy of all active connections
func (ct *ConnectionTracker) GetConnections() map[string]*Connection {
	ct.mu.RLock()
	defer ct.mu.RUnlock()

	connections := make(map[string]*Connection, len(ct.connections))
	for id, conn := range ct.connections {
		connections[id] = &Connection{
			ID:         conn.ID,
			StartTime:  conn.StartTime,
			Type:       conn.Type,
			RemoteAddr: conn.RemoteAddr,
		}
	}

	return connections
}

// CloseAll closes all tracked connections
func (ct *ConnectionTracker) CloseAll() {
	ct.mu.Lock()
	defer ct.mu.Unlock()

	for id := range ct.connections {
		delete(ct.connections, id)
	}
}
