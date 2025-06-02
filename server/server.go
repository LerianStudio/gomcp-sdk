// Package server implements the MCP server functionality
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/LerianStudio/gomcp-sdk/correlation"
	"github.com/LerianStudio/gomcp-sdk/protocol"
	"github.com/LerianStudio/gomcp-sdk/transport"
)

// LifecyclePhase represents different phases of server lifecycle
type LifecyclePhase int

const (
	PhaseInitializing LifecyclePhase = iota
	PhaseStarting
	PhaseRunning
	PhaseStopping
	PhaseStopped
)

// String returns the string representation of the lifecycle phase
func (p LifecyclePhase) String() string {
	switch p {
	case PhaseInitializing:
		return "initializing"
	case PhaseStarting:
		return "starting"
	case PhaseRunning:
		return "running"
	case PhaseStopping:
		return "stopping"
	case PhaseStopped:
		return "stopped"
	default:
		return "unknown"
	}
}

// ShutdownHook represents a function to be called during shutdown
type ShutdownHook func(ctx context.Context) error

// LifecycleHook represents a function to be called during lifecycle transitions
type LifecycleHook func(ctx context.Context, phase LifecyclePhase) error

// LifecycleManager manages server lifecycle and shutdown hooks
type LifecycleManager struct {
	phase           LifecyclePhase
	shutdownHooks   []ShutdownHook
	lifecycleHooks  []LifecycleHook
	shutdownTimeout time.Duration
	mu              sync.RWMutex
}

// Server represents an MCP server
type Server struct {
	name             string
	version          string
	capabilities     protocol.ServerCapabilities
	tools            map[string]*ToolRegistration
	resources        map[string]*ResourceRegistration
	prompts          map[string]*PromptRegistration
	transport        transport.Transport
	mutex            sync.RWMutex
	initialized      bool
	lifecycleManager *LifecycleManager
}

// ToolRegistration represents a registered tool
type ToolRegistration struct {
	Tool    protocol.Tool
	Handler protocol.ToolHandler
}

// ResourceRegistration represents a registered resource
type ResourceRegistration struct {
	Resource protocol.Resource
	Handler  ResourceHandler
}

// PromptRegistration represents a registered prompt
type PromptRegistration struct {
	Prompt  protocol.Prompt
	Handler PromptHandler
}

// ResourceHandler defines the interface for resource handlers
type ResourceHandler interface {
	Handle(ctx context.Context, uri string) ([]protocol.Content, error)
}

// PromptHandler defines the interface for prompt handlers
type PromptHandler interface {
	Handle(ctx context.Context, args map[string]interface{}) ([]protocol.Content, error)
}

// NewServer creates a new MCP server
func NewServer(name, version string) *Server {
	return &Server{
		name:      name,
		version:   version,
		tools:     make(map[string]*ToolRegistration),
		resources: make(map[string]*ResourceRegistration),
		prompts:   make(map[string]*PromptRegistration),
		capabilities: protocol.ServerCapabilities{
			Tools: &protocol.ToolCapability{
				ListChanged: false,
			},
			Resources: &protocol.ResourceCapability{
				Subscribe:   false,
				ListChanged: false,
			},
			Prompts: &protocol.PromptCapability{
				ListChanged: false,
			},
		},
		lifecycleManager: &LifecycleManager{
			phase:           PhaseInitializing,
			shutdownTimeout: 30 * time.Second,
		},
	}
}

// AddTool registers a new tool
func (s *Server) AddTool(tool protocol.Tool, handler protocol.ToolHandler) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.tools[tool.Name] = &ToolRegistration{
		Tool:    tool,
		Handler: handler,
	}
}

// AddResource registers a new resource
func (s *Server) AddResource(resource protocol.Resource, handler ResourceHandler) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.resources[resource.URI] = &ResourceRegistration{
		Resource: resource,
		Handler:  handler,
	}
}

// AddPrompt registers a new prompt
func (s *Server) AddPrompt(prompt protocol.Prompt, handler PromptHandler) {
	s.mutex.Lock()
	defer s.mutex.Unlock()

	s.prompts[prompt.Name] = &PromptRegistration{
		Prompt:  prompt,
		Handler: handler,
	}
}

// SetTransport sets the transport layer
func (s *Server) SetTransport(t transport.Transport) {
	s.transport = t
}

// Start starts the server
func (s *Server) Start(ctx context.Context) error {
	// Transition to starting phase
	if err := s.lifecycleManager.SetPhase(ctx, PhaseStarting); err != nil {
		return fmt.Errorf("failed to transition to starting phase: %w", err)
	}

	if s.transport == nil {
		// Transition back to stopped on failure
		_ = s.lifecycleManager.SetPhase(context.Background(), PhaseStopped)
		return fmt.Errorf("no transport configured")
	}

	// Start the transport
	if err := s.transport.Start(ctx, s); err != nil {
		// Transition back to stopped on failure
		_ = s.lifecycleManager.SetPhase(context.Background(), PhaseStopped)
		return fmt.Errorf("failed to start transport: %w", err)
	}

	// Transition to running phase
	if err := s.lifecycleManager.SetPhase(ctx, PhaseRunning); err != nil {
		// Stop transport and transition to stopped
		_ = s.transport.Stop()
		_ = s.lifecycleManager.SetPhase(context.Background(), PhaseStopped)
		return fmt.Errorf("failed to transition to running phase: %w", err)
	}

	fmt.Printf("🚀 Server: %s v%s started successfully\n", s.name, s.version)
	return nil
}

// StartWithTransport starts the server with the specified transport
func (s *Server) StartWithTransport(ctx context.Context, transport transport.Transport) error {
	s.SetTransport(transport)
	return s.Start(ctx)
}

// HandleRequest handles an incoming JSON-RPC request
func (s *Server) HandleRequest(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	// Ensure correlation ID exists in context
	ctx = correlation.EnsureCorrelationID(ctx)
	ctx = correlation.EnsureRequestID(ctx)

	// Log request with correlation data
	fmt.Printf("📨 %s\n", correlation.FormatLogMessage(ctx,
		fmt.Sprintf("Handling JSON-RPC request: %s", req.Method)))

	switch req.Method {
	case "initialize":
		return s.handleInitialize(ctx, req)
	case "tools/list":
		return s.handleToolsList(ctx, req)
	case "tools/call":
		return s.handleToolsCall(ctx, req)
	case "resources/list":
		return s.handleResourcesList(ctx, req)
	case "resources/read":
		return s.handleResourcesRead(ctx, req)
	case "prompts/list":
		return s.handlePromptsList(ctx, req)
	case "prompts/get":
		return s.handlePromptsGet(ctx, req)
	default:
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.MethodNotFound, "Method not found", nil),
		}
	}
}

// handleInitialize handles the initialize request
func (s *Server) handleInitialize(_ context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	var initReq protocol.InitializeRequest
	if err := parseParams(req.Params, &initReq); err != nil {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "Invalid parameters", err.Error()),
		}
	}

	s.mutex.Lock()
	s.initialized = true
	s.mutex.Unlock()

	// Client-aware capability negotiation
	filteredCapabilities := s.filterCapabilitiesForClient(initReq.ClientInfo)

	result := protocol.InitializeResult{
		ProtocolVersion: protocol.Version,
		Capabilities:    filteredCapabilities,
		ServerInfo: protocol.ServerInfo{
			Name:    s.name,
			Version: s.version,
		},
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsList handles the tools/list request
func (s *Server) handleToolsList(_ context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	tools := make([]protocol.Tool, 0, len(s.tools))
	for _, registration := range s.tools {
		tools = append(tools, registration.Tool)
	}

	result := map[string]interface{}{
		"tools": tools,
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleToolsCall handles the tools/call request
func (s *Server) handleToolsCall(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	var callReq protocol.ToolCallRequest
	if err := parseParams(req.Params, &callReq); err != nil {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "Invalid parameters", err.Error()),
		}
	}

	s.mutex.RLock()
	registration, exists := s.tools[callReq.Name]
	s.mutex.RUnlock()

	if !exists {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.MethodNotFound, "Tool not found", nil),
		}
	}

	result, err := registration.Handler.Handle(ctx, callReq.Arguments)
	if err != nil {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Result:  protocol.NewToolCallError(err.Error()),
		}
	}

	// Convert result to tool call result format
	var toolResult *protocol.ToolCallResult
	switch v := result.(type) {
	case *protocol.ToolCallResult:
		toolResult = v
	case string:
		toolResult = protocol.NewToolCallResult(protocol.NewContent(v))
	default:
		// Try to marshal to JSON and use as text
		if jsonBytes, err := json.Marshal(result); err == nil {
			toolResult = protocol.NewToolCallResult(protocol.NewContent(string(jsonBytes)))
		} else {
			toolResult = protocol.NewToolCallResult(protocol.NewContent(fmt.Sprintf("%v", result)))
		}
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  toolResult,
	}
}

// handleResourcesList handles the resources/list request
func (s *Server) handleResourcesList(_ context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	resources := make([]protocol.Resource, 0, len(s.resources))
	for _, registration := range s.resources {
		resources = append(resources, registration.Resource)
	}

	result := map[string]interface{}{
		"resources": resources,
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handleResourcesRead handles the resources/read request
func (s *Server) handleResourcesRead(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "Invalid parameters", nil),
		}
	}

	uri, ok := params["uri"].(string)
	if !ok {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "URI parameter required", nil),
		}
	}

	s.mutex.RLock()
	registration, exists := s.resources[uri]
	s.mutex.RUnlock()

	if !exists {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.MethodNotFound, "Resource not found", nil),
		}
	}

	content, err := registration.Handler.Handle(ctx, uri)
	if err != nil {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InternalError, err.Error(), nil),
		}
	}

	result := map[string]interface{}{
		"contents": content,
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handlePromptsList handles the prompts/list request
func (s *Server) handlePromptsList(_ context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	s.mutex.RLock()
	defer s.mutex.RUnlock()

	prompts := make([]protocol.Prompt, 0, len(s.prompts))
	for _, registration := range s.prompts {
		prompts = append(prompts, registration.Prompt)
	}

	result := map[string]interface{}{
		"prompts": prompts,
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// handlePromptsGet handles the prompts/get request
func (s *Server) handlePromptsGet(ctx context.Context, req *protocol.JSONRPCRequest) *protocol.JSONRPCResponse {
	params, ok := req.Params.(map[string]interface{})
	if !ok {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "Invalid parameters", nil),
		}
	}

	name, ok := params["name"].(string)
	if !ok {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InvalidParams, "Name parameter required", nil),
		}
	}

	args, _ := params["arguments"].(map[string]interface{})

	s.mutex.RLock()
	registration, exists := s.prompts[name]
	s.mutex.RUnlock()

	if !exists {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.MethodNotFound, "Prompt not found", nil),
		}
	}

	content, err := registration.Handler.Handle(ctx, args)
	if err != nil {
		return &protocol.JSONRPCResponse{
			JSONRPC: "2.0",
			ID:      req.ID,
			Error:   protocol.NewJSONRPCError(protocol.InternalError, err.Error(), nil),
		}
	}

	result := map[string]interface{}{
		"messages": content,
	}

	return &protocol.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      req.ID,
		Result:  result,
	}
}

// parseParams is a helper function to parse JSON-RPC parameters with flexible handling
// This replaces the previous rigid marshal-unmarshal pattern that caused compatibility issues
func parseParams(params interface{}, target interface{}) error {
	return protocol.FlexibleParseParams(params, target)
}

// IsInitialized returns whether the server has been initialized
func (s *Server) IsInitialized() bool {
	s.mutex.RLock()
	defer s.mutex.RUnlock()
	return s.initialized
}

// filterCapabilitiesForClient filters server capabilities based on client compatibility
func (s *Server) filterCapabilitiesForClient(clientInfo protocol.ClientInfo) protocol.ServerCapabilities {
	s.mutex.RLock()
	baseCapabilities := s.capabilities
	s.mutex.RUnlock()

	// Start with base capabilities
	filtered := baseCapabilities

	// Detect client type and apply compatibility filters
	clientType := detectClientType(clientInfo)

	switch clientType {
	case "claude-desktop":
		// Claude Desktop has strict TypeScript validation
		// Don't advertise advanced features it might not support
		filtered.Sampling = nil // Most clients don't support sampling
		filtered.Roots = nil    // Most clients don't support roots

	case "vscode":
		// VS Code extension clients
		filtered.Sampling = nil
		filtered.Roots = nil

	case "openai":
		// OpenAI clients (like ChatGPT plugins)
		filtered.Sampling = nil
		filtered.Roots = nil

	case "anthropic":
		// Native Anthropic clients might support more features
		// Keep all capabilities

	default:
		// Unknown clients - be conservative
		filtered.Sampling = nil
		filtered.Roots = nil
	}

	return filtered
}

// detectClientType attempts to identify the client type from ClientInfo
func detectClientType(clientInfo protocol.ClientInfo) string {
	name := strings.ToLower(clientInfo.Name)

	// Known client patterns
	if strings.Contains(name, "claude") && strings.Contains(name, "desktop") {
		return "claude-desktop"
	}
	if strings.Contains(name, "vscode") || strings.Contains(name, "visual studio code") {
		return "vscode"
	}
	if strings.Contains(name, "openai") || strings.Contains(name, "chatgpt") {
		return "openai"
	}
	if strings.Contains(name, "anthropic") {
		return "anthropic"
	}
	if strings.Contains(name, "rest") || strings.Contains(name, "curl") {
		return "rest-client"
	}

	return "unknown"
}

// LifecycleManager methods

// NewLifecycleManager creates a new lifecycle manager
func NewLifecycleManager() *LifecycleManager {
	return &LifecycleManager{
		phase:           PhaseInitializing,
		shutdownTimeout: 30 * time.Second,
	}
}

// GetPhase returns the current lifecycle phase
func (lm *LifecycleManager) GetPhase() LifecyclePhase {
	lm.mu.RLock()
	defer lm.mu.RUnlock()
	return lm.phase
}

// SetPhase sets the current lifecycle phase and notifies hooks
func (lm *LifecycleManager) SetPhase(ctx context.Context, phase LifecyclePhase) error {
	lm.mu.Lock()
	oldPhase := lm.phase
	lm.phase = phase
	hooks := make([]LifecycleHook, len(lm.lifecycleHooks))
	copy(hooks, lm.lifecycleHooks)
	lm.mu.Unlock()

	if oldPhase != phase {
		fmt.Printf("🔄 Server: Lifecycle transition %s → %s\n", oldPhase, phase)

		// Execute lifecycle hooks
		for _, hook := range hooks {
			if err := hook(ctx, phase); err != nil {
				fmt.Printf("❌ Lifecycle hook failed for phase %s: %v\n", phase, err)
				return err
			}
		}
	}

	return nil
}

// AddShutdownHook adds a function to be called during shutdown
func (lm *LifecycleManager) AddShutdownHook(hook ShutdownHook) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.shutdownHooks = append(lm.shutdownHooks, hook)
}

// AddLifecycleHook adds a function to be called during lifecycle transitions
func (lm *LifecycleManager) AddLifecycleHook(hook LifecycleHook) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.lifecycleHooks = append(lm.lifecycleHooks, hook)
}

// SetShutdownTimeout configures the shutdown timeout
func (lm *LifecycleManager) SetShutdownTimeout(timeout time.Duration) {
	lm.mu.Lock()
	defer lm.mu.Unlock()
	lm.shutdownTimeout = timeout
}

// ExecuteShutdownHooks executes all registered shutdown hooks
func (lm *LifecycleManager) ExecuteShutdownHooks(ctx context.Context) error {
	lm.mu.RLock()
	hooks := make([]ShutdownHook, len(lm.shutdownHooks))
	copy(hooks, lm.shutdownHooks)
	timeout := lm.shutdownTimeout
	lm.mu.RUnlock()

	if len(hooks) == 0 {
		return nil
	}

	fmt.Printf("🪝 Server: Executing %d shutdown hooks...\n", len(hooks))

	// Create context with timeout for shutdown hooks
	hookCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	// Execute hooks in reverse order (LIFO)
	var errors []error
	for i := len(hooks) - 1; i >= 0; i-- {
		hook := hooks[i]
		if err := hook(hookCtx); err != nil {
			fmt.Printf("❌ Shutdown hook %d failed: %v\n", i, err)
			errors = append(errors, err)
		} else {
			fmt.Printf("✅ Shutdown hook %d completed\n", i)
		}
	}

	if len(errors) > 0 {
		return fmt.Errorf("shutdown hooks failed: %v", errors)
	}

	fmt.Println("✅ All shutdown hooks completed successfully")
	return nil
}

// Server lifecycle methods

// AddShutdownHook adds a function to be called during shutdown
func (s *Server) AddShutdownHook(hook ShutdownHook) {
	s.lifecycleManager.AddShutdownHook(hook)
}

// AddLifecycleHook adds a function to be called during lifecycle transitions
func (s *Server) AddLifecycleHook(hook LifecycleHook) {
	s.lifecycleManager.AddLifecycleHook(hook)
}

// GetLifecyclePhase returns the current lifecycle phase
func (s *Server) GetLifecyclePhase() LifecyclePhase {
	return s.lifecycleManager.GetPhase()
}

// SetShutdownTimeout configures the shutdown timeout
func (s *Server) SetShutdownTimeout(timeout time.Duration) {
	s.lifecycleManager.SetShutdownTimeout(timeout)
}

// Stop stops the server with lifecycle management
func (s *Server) Stop(ctx context.Context) error {
	// Transition to stopping phase
	if err := s.lifecycleManager.SetPhase(ctx, PhaseStopping); err != nil {
		fmt.Printf("⚠️ Failed to transition to stopping phase: %v\n", err)
	}

	fmt.Printf("🛑 Server: Stopping %s v%s...\n", s.name, s.version)

	// Execute shutdown hooks
	if err := s.lifecycleManager.ExecuteShutdownHooks(ctx); err != nil {
		fmt.Printf("⚠️ Shutdown hooks failed: %v\n", err)
	}

	// Stop the transport
	s.mutex.RLock()
	transport := s.transport
	s.mutex.RUnlock()

	if transport != nil {
		if err := transport.Stop(); err != nil {
			fmt.Printf("⚠️ Failed to stop transport: %v\n", err)
		}
	}

	// Transition to stopped phase
	if err := s.lifecycleManager.SetPhase(ctx, PhaseStopped); err != nil {
		fmt.Printf("⚠️ Failed to transition to stopped phase: %v\n", err)
	}

	fmt.Printf("✅ Server: %s v%s stopped\n", s.name, s.version)
	return nil
}

// Shutdown performs graceful shutdown with timeout
func (s *Server) Shutdown(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	return s.Stop(ctx)
}

// IsRunning returns true if the server is in running phase
func (s *Server) IsRunning() bool {
	return s.lifecycleManager.GetPhase() == PhaseRunning
}

// WaitForShutdown waits for the server to be stopped
func (s *Server) WaitForShutdown(ctx context.Context) error {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			if s.lifecycleManager.GetPhase() == PhaseStopped {
				return nil
			}
		}
	}
}
