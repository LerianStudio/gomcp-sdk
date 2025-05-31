package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"

	"github.com/fredcamaral/gomcp-sdk/protocol"
)

// HandlerType represents the type of handler loading strategy
type HandlerType string

const (
	// HandlerTypeEmbedded represents handlers compiled into the binary
	HandlerTypeEmbedded HandlerType = "embedded"
	// HandlerTypeSubprocess represents handlers running as separate processes
	HandlerTypeSubprocess HandlerType = "subprocess"
	// HandlerTypeDynamic represents dynamically loaded handlers (plugins)
	HandlerTypeDynamic HandlerType = "dynamic"
)

// HandlerConfig configures a handler's loading strategy
type HandlerConfig struct {
	Type       HandlerType            `json:"type"`
	Command    string                 `json:"command,omitempty"`    // For subprocess
	Args       []string               `json:"args,omitempty"`        // For subprocess
	PluginPath string                 `json:"pluginPath,omitempty"`  // For dynamic
	Env        map[string]string      `json:"env,omitempty"`         // Environment variables
	Timeout    time.Duration          `json:"timeout,omitempty"`     // Execution timeout
	Config     map[string]interface{} `json:"config,omitempty"`      // Handler-specific config
}

// HandlerLoader manages the loading and lifecycle of tool handlers
type HandlerLoader struct {
	handlers map[string]*LoadedHandler
	mutex    sync.RWMutex
}

// LoadedHandler represents a loaded handler with its lifecycle management
type LoadedHandler struct {
	Tool     protocol.Tool
	Handler  protocol.ToolHandler
	Type     HandlerType
	Config   *HandlerConfig
	Source   string
	LoadedAt time.Time
	
	// For subprocess handlers
	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
}

// NewHandlerLoader creates a new handler loader
func NewHandlerLoader() *HandlerLoader {
	return &HandlerLoader{
		handlers: make(map[string]*LoadedHandler),
	}
}

// LoadHandler loads a handler based on the configuration
func (l *HandlerLoader) LoadHandler(tool protocol.Tool, config *HandlerConfig, source string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	// Check if handler already exists
	if _, exists := l.handlers[tool.Name]; exists {
		return fmt.Errorf("handler %s already loaded", tool.Name)
	}

	var loadedHandler *LoadedHandler

	switch config.Type {
	case HandlerTypeEmbedded:
		embeddedHandler, err := l.loadEmbeddedHandler(tool.Name, config)
		if err != nil {
			return fmt.Errorf("failed to load embedded handler: %w", err)
		}
		loadedHandler = &LoadedHandler{
			Tool:     tool,
			Handler:  embeddedHandler,
			Type:     HandlerTypeEmbedded,
			Config:   config,
			Source:   source,
			LoadedAt: time.Now(),
		}

	case HandlerTypeSubprocess:
		var err error
		loadedHandler, err = l.loadSubprocessHandler(tool, config)
		if err != nil {
			return fmt.Errorf("failed to load subprocess handler: %w", err)
		}

	case HandlerTypeDynamic:
		return fmt.Errorf("dynamic loading not yet implemented")

	default:
		return fmt.Errorf("unknown handler type: %s", config.Type)
	}

	l.handlers[tool.Name] = loadedHandler
	log.Printf("Loaded %s handler for tool %s from %s", config.Type, tool.Name, source)
	return nil
}

// GetHandler retrieves a loaded handler
func (l *HandlerLoader) GetHandler(toolName string) (protocol.ToolHandler, error) {
	l.mutex.RLock()
	defer l.mutex.RUnlock()

	handler, exists := l.handlers[toolName]
	if !exists {
		return nil, fmt.Errorf("handler %s not found", toolName)
	}

	return handler.Handler, nil
}

// UnloadHandler unloads a handler and cleans up resources
func (l *HandlerLoader) UnloadHandler(toolName string) error {
	l.mutex.Lock()
	defer l.mutex.Unlock()

	handler, exists := l.handlers[toolName]
	if !exists {
		return fmt.Errorf("handler %s not found", toolName)
	}

	// Cleanup based on handler type
	if handler.Type == HandlerTypeSubprocess && handler.process != nil {
		// Close pipes
		if handler.stdin != nil {
			if err := handler.stdin.Close(); err != nil {
				log.Printf("Failed to close stdin for tool %s: %v", toolName, err)
			}
		}
		if handler.stdout != nil {
			if err := handler.stdout.Close(); err != nil {
				log.Printf("Failed to close stdout for tool %s: %v", toolName, err)
			}
		}
		if handler.stderr != nil {
			if err := handler.stderr.Close(); err != nil {
				log.Printf("Failed to close stderr for tool %s: %v", toolName, err)
			}
		}

		// Terminate process
		pid := handler.process.Process.Pid
		if err := handler.process.Process.Kill(); err != nil {
			return fmt.Errorf("failed to kill subprocess: %w", err)
		}
		log.Printf("Terminated subprocess handler (PID: %d)", pid)
	}

	delete(l.handlers, toolName)
	log.Printf("Unloaded handler for tool %s", toolName)
	return nil
}

// loadEmbeddedHandler loads a handler from the embedded handler registry
func (l *HandlerLoader) loadEmbeddedHandler(name string, config *HandlerConfig) (protocol.ToolHandler, error) {
	// Get handler from embedded registry
	handler, exists := embeddedHandlers[name]
	if !exists {
		return nil, fmt.Errorf("embedded handler %s not found", name)
	}

	// If the handler needs configuration, pass it
	if configurable, ok := handler.(ConfigurableHandler); ok {
		if err := configurable.Configure(config.Config); err != nil {
			return nil, fmt.Errorf("failed to configure handler: %w", err)
		}
	}

	return handler, nil
}

// loadSubprocessHandler loads a handler that runs as a subprocess
func (l *HandlerLoader) loadSubprocessHandler(tool protocol.Tool, config *HandlerConfig) (*LoadedHandler, error) {
	// Security: Validate and sanitize command and arguments
	if err := validateSubprocessConfig(config); err != nil {
		return nil, fmt.Errorf("security validation failed: %w", err)
	}
	
	// Prepare command with security constraints
	cmd := exec.Command(config.Command, config.Args...)
	
	// Security: Set minimal, controlled environment
	cmd.Env = createSecureEnvironment(config.Env)
	
	// Security: Set process group for isolation (only in production)
	if os.Getenv("GO_ENV") == "production" {
		cmd.SysProcAttr = createSecureProcessAttributes()
	}

	// Create pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start subprocess: %w", err)
	}
	log.Printf("Started subprocess handler for %s: %s %v (PID: %d)", tool.Name, config.Command, config.Args, cmd.Process.Pid)

	// Create subprocess handler
	subprocessHandler := &SubprocessHandler{
		tool:    tool,
		process: cmd,
		stdin:   stdin,
		stdout:  stdout,
		stderr:  stderr,
		timeout: config.Timeout,
	}

	return &LoadedHandler{
		Tool:     tool,
		Handler:  subprocessHandler,
		Type:     HandlerTypeSubprocess,
		Config:   config,
		Source:   "",
		LoadedAt: time.Now(),
		process:  cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
	}, nil
}

// SubprocessHandler implements ToolHandler for subprocess-based tools
type SubprocessHandler struct {
	tool    protocol.Tool
	process *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	stderr  io.ReadCloser
	timeout time.Duration
	mutex   sync.Mutex
}

// Handle executes the tool via subprocess communication
func (h *SubprocessHandler) Handle(ctx context.Context, params map[string]interface{}) (interface{}, error) {
	h.mutex.Lock()
	defer h.mutex.Unlock()

	// Create request
	request := map[string]interface{}{
		"tool":   h.tool.Name,
		"params": params,
	}

	// Encode request
	encoder := json.NewEncoder(h.stdin)
	if err := encoder.Encode(request); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}

	// Set timeout if configured
	if h.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, h.timeout)
		defer cancel()
	}

	// Read response with timeout
	responseChan := make(chan map[string]interface{}, 1)
	errorChan := make(chan error, 1)

	go func() {
		decoder := json.NewDecoder(h.stdout)
		var response map[string]interface{}
		if err := decoder.Decode(&response); err != nil {
			errorChan <- fmt.Errorf("failed to read response: %w", err)
			return
		}
		responseChan <- response
	}()

	select {
	case <-ctx.Done():
		return nil, fmt.Errorf("subprocess handler timeout: %w", ctx.Err())
	case err := <-errorChan:
		return nil, err
	case response := <-responseChan:
		// Check for error in response
		if errMsg, ok := response["error"].(string); ok {
			return nil, fmt.Errorf("subprocess error: %s", errMsg)
		}
		return response["result"], nil
	}
}

// ConfigurableHandler interface for handlers that can be configured
type ConfigurableHandler interface {
	protocol.ToolHandler
	Configure(config map[string]interface{}) error
}

// embeddedHandlers is the registry of built-in handlers
var embeddedHandlers = make(map[string]protocol.ToolHandler)

// RegisterEmbeddedHandler registers a built-in handler
func RegisterEmbeddedHandler(name string, handler protocol.ToolHandler) {
	embeddedHandlers[name] = handler
}

// LoadHandlerFromManifest loads a handler based on manifest configuration
func (l *HandlerLoader) LoadHandlerFromManifest(manifestPath string, tool protocol.Tool, source string) error {
	// Look for handler configuration file
	handlerConfigPath := filepath.Join(filepath.Dir(manifestPath), fmt.Sprintf("%s.handler.json", tool.Name))
	
	// If no specific handler config, try common handler config
	if _, err := os.Stat(handlerConfigPath); os.IsNotExist(err) {
		handlerConfigPath = filepath.Join(filepath.Dir(manifestPath), "handler.json")
	}

	// Load handler configuration
	data, err := os.ReadFile(handlerConfigPath)
	if err != nil {
		// If no config file, try to use embedded handler
		return l.LoadHandler(tool, &HandlerConfig{Type: HandlerTypeEmbedded}, source)
	}

	var config HandlerConfig
	if err := protocol.FlexibleUnmarshal(data, &config); err != nil {
		return fmt.Errorf("failed to parse handler config: %w", err)
	}

	return l.LoadHandler(tool, &config, source)
}

// validateSubprocessConfig validates subprocess configuration for security
func validateSubprocessConfig(config *HandlerConfig) error {
	if config.Command == "" {
		return fmt.Errorf("command cannot be empty")
	}
	
	// Security: Only allow specific whitelisted commands
	allowedCommands := map[string]bool{
		"/usr/bin/python3":        true,
		"/usr/bin/python":         true,
		"/opt/homebrew/bin/python3": true, // Homebrew Python on macOS
		"/usr/local/bin/python3":  true,  // Local Python installations
		"/bin/bash":               true,
		"/bin/sh":                 true,
		"/usr/bin/node":           true,
		"/usr/local/bin/node":     true,  // Local Node installations
	}
	
	if !allowedCommands[config.Command] {
		return fmt.Errorf("command not in whitelist: %s", config.Command)
	}
	
	// Security: Validate arguments don't contain dangerous patterns
	for _, arg := range config.Args {
		if containsDangerousPatterns(arg) {
			return fmt.Errorf("argument contains dangerous patterns: %s", arg)
		}
	}
	
	return nil
}

// containsDangerousPatterns checks for command injection patterns
func containsDangerousPatterns(arg string) bool {
	dangerous := []string{
		";", "&", "|", "`", "$", "$(", "`", 
		"&&", "||", ">>", "<<", ">", "<",
		"\n", "\r", "\t",
	}
	
	for _, pattern := range dangerous {
		if len(pattern) > 0 && len(arg) > 0 && 
		   (pattern[0] == arg[0] || 
		    (len(arg) > 1 && pattern == arg[:len(pattern)])) {
			return true
		}
	}
	return false
}

// createSecureEnvironment creates a minimal, secure environment
func createSecureEnvironment(customEnv map[string]string) []string {
	// Start with minimal secure environment
	secureEnv := []string{
		"PATH=/usr/bin:/bin",
		"HOME=/tmp",
		"USER=mcp-handler",
	}
	
	// Add only safe custom environment variables
	for k, v := range customEnv {
		if isSafeEnvVar(k, v) {
			secureEnv = append(secureEnv, fmt.Sprintf("%s=%s", k, v))
		}
	}
	
	return secureEnv
}

// isSafeEnvVar validates environment variable safety
func isSafeEnvVar(key, value string) bool {
	// Disallow dangerous environment variables
	dangerousKeys := map[string]bool{
		"LD_PRELOAD":     true,
		"LD_LIBRARY_PATH": true,
		"DYLD_INSERT_LIBRARIES": true,
		"PYTHONPATH":     false, // Allow but validate
		"NODE_PATH":      false, // Allow but validate
	}
	
	if dangerous, exists := dangerousKeys[key]; exists && dangerous {
		return false
	}
	
	// Check for dangerous patterns in values
	return !containsDangerousPatterns(value)
}