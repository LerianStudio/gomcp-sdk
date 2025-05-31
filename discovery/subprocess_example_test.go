package discovery

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"
	
	"github.com/fredcamaral/gomcp-sdk/protocol"
)

// CreateExampleSubprocessHandler creates an example subprocess handler script
func CreateExampleSubprocessHandler(dir string, toolName string) (string, error) {
	scriptPath := filepath.Join(dir, fmt.Sprintf("%s_handler.py", toolName))
	
	// Create a simple Python handler script
	scriptContent := `#!/usr/bin/env python3
import json
import sys
import os

def handle_request(request):
    """Handle incoming tool request"""
    tool = request.get('tool')
    params = request.get('params', {})
    
    if tool == 'python_echo':
        message = params.get('message', '')
        return {
            'result': {
                'echo': f"Python says: {message}",
                'pid': os.getpid()
            }
        }
    elif tool == 'python_calc':
        operation = params.get('operation')
        a = params.get('a', 0)
        b = params.get('b', 0)
        
        result = 0
        if operation == 'add':
            result = a + b
        elif operation == 'multiply':
            result = a * b
        
        return {
            'result': {
                'calculation': result,
                'expression': f"{a} {operation} {b}"
            }
        }
    else:
        return {
            'error': f"Unknown tool: {tool}"
        }

# Main loop
while True:
    try:
        # Read request from stdin
        line = sys.stdin.readline()
        if not line:
            break
            
        request = json.loads(line)
        response = handle_request(request)
        
        # Write response to stdout
        print(json.dumps(response))
        sys.stdout.flush()
        
    except Exception as e:
        error_response = {'error': str(e)}
        print(json.dumps(error_response))
        sys.stdout.flush()
`

	if err := os.WriteFile(scriptPath, []byte(scriptContent), 0755); err != nil {
		return "", err
	}
	
	// Make it executable
	if err := os.Chmod(scriptPath, 0755); err != nil {
		return "", err
	}
	
	return scriptPath, nil
}

// CreateSubprocessHandlerConfig creates a configuration for subprocess handler
func CreateSubprocessHandlerConfig(scriptPath string) *HandlerConfig {
	return &HandlerConfig{
		Type:    HandlerTypeSubprocess,
		Command: "/opt/homebrew/bin/python3", // Use actual system Python path
		Args:    []string{scriptPath},
		Timeout: 30 * time.Second,
	}
}

// RunSubprocessHandlerExample demonstrates a subprocess handler
func RunSubprocessHandlerExample(ctx context.Context) error {
	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "subprocess-test")
	if err != nil {
		return err
	}
	defer func() {
		if err := os.RemoveAll(tmpDir); err != nil {
			fmt.Printf("Failed to remove temporary directory: %v\n", err)
		}
	}()
	
	// Create handler script
	scriptPath, err := CreateExampleSubprocessHandler(tmpDir, "python")
	if err != nil {
		return fmt.Errorf("failed to create handler script: %w", err)
	}
	
	// Create handler configuration
	config := CreateSubprocessHandlerConfig(scriptPath)
	
	// Create loader and load handler
	loader := NewHandlerLoader()
	tool := protocol.Tool{
		Name:        "python_echo",
		Description: "Python subprocess echo handler",
	}
	
	if err := loader.LoadHandler(tool, config, "test"); err != nil {
		return fmt.Errorf("failed to load handler: %w", err)
	}
	defer func() {
		if err := loader.UnloadHandler(tool.Name); err != nil {
			fmt.Printf("Failed to unload handler: %v\n", err)
		}
	}()
	
	// Get and test handler
	handler, err := loader.GetHandler(tool.Name)
	if err != nil {
		return fmt.Errorf("failed to get handler: %w", err)
	}
	
	// Test the handler
	result, err := handler.Handle(ctx, map[string]interface{}{
		"message": "Hello from Go!",
	})
	if err != nil {
		return fmt.Errorf("handler execution failed: %w", err)
	}
	
	fmt.Printf("Subprocess handler result: %v\n", result)
	return nil
}

// IsPythonAvailable checks if Python is available
func IsPythonAvailable() bool {
	// Use absolute path and sanitized args to prevent command injection
	cmd := exec.Command("/opt/homebrew/bin/python3", "--version")
	cmd.Env = []string{} // Clear environment for security
	return cmd.Run() == nil
}