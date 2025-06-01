package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/fredcamaral/gomcp-sdk/discovery"
	"github.com/fredcamaral/gomcp-sdk/protocol"
)

func main() {
	// Create a temporary directory for plugin manifests
	pluginDir, err := os.MkdirTemp("", "mcp-plugins")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(pluginDir)

	// Create discovery registry
	registry := discovery.NewRegistry()

	// Create plugin watcher
	watcher, err := discovery.NewPluginWatcher(pluginDir, 5*time.Second, registry)
	if err != nil {
		log.Fatal(err)
	}

	// Start watching
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	if err := watcher.Start(ctx); err != nil {
		log.Fatal(err)
	}
	defer watcher.Stop()

	// Create example plugin manifests
	createExamplePlugins(pluginDir)

	// Wait for plugins to be discovered
	time.Sleep(2 * time.Second)

	// List discovered tools
	info := registry.Discover(nil)
	fmt.Println("Discovered tools:")
	for _, tool := range info.Tools {
		fmt.Printf("  - %s: %s\n", tool.Tool.Name, tool.Tool.Description)
	}

	// Test tool handlers
	testHandlers(registry)
}

func createExamplePlugins(pluginDir string) {
	// Create math plugin
	mathDir := filepath.Join(pluginDir, "math-plugin")
	os.MkdirAll(mathDir, 0755)

	mathManifest := discovery.PluginManifest{
		Name:        "Math Plugin",
		Version:     "1.0.0",
		Description: "Basic mathematical operations",
		Author:      "Example",
		Tools: []protocol.Tool{
			{
				Name:        "calculate",
				Description: "Perform basic calculations",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"operation": map[string]interface{}{
							"type": "string",
							"enum": []string{"add", "subtract", "multiply", "divide", "sqrt", "abs"},
						},
						"a": map[string]interface{}{
							"type": "number",
						},
						"b": map[string]interface{}{
							"type": "number",
						},
					},
					"required": []string{"operation", "a"},
				},
			},
		},
		Tags: []string{"math", "calculation"},
	}

	data, _ := json.MarshalIndent(mathManifest, "", "  ")
	os.WriteFile(filepath.Join(mathDir, "mcp-manifest.json"), data, 0644)

	// Create handler config for embedded handler
	handlerConfig := discovery.HandlerConfig{
		Type: discovery.HandlerTypeEmbedded,
	}
	configData, _ := json.MarshalIndent(handlerConfig, "", "  ")
	os.WriteFile(filepath.Join(mathDir, "handler.json"), configData, 0644)

	// Create utility plugin
	utilDir := filepath.Join(pluginDir, "util-plugin")
	os.MkdirAll(utilDir, 0755)

	utilManifest := discovery.PluginManifest{
		Name:        "Utility Plugin",
		Version:     "1.0.0",
		Description: "Utility tools",
		Author:      "Example",
		Tools: []protocol.Tool{
			{
				Name:        "echo",
				Description: "Echo a message",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"message": map[string]interface{}{
							"type": "string",
						},
					},
					"required": []string{"message"},
				},
			},
			{
				Name:        "time",
				Description: "Get current time",
				InputSchema: map[string]interface{}{
					"type": "object",
					"properties": map[string]interface{}{
						"format": map[string]interface{}{
							"type": "string",
						},
					},
				},
			},
		},
		Tags: []string{"utility"},
	}

	data, _ = json.MarshalIndent(utilManifest, "", "  ")
	os.WriteFile(filepath.Join(utilDir, "mcp-manifest.json"), data, 0644)
}

func testHandlers(registry *discovery.Registry) {
	ctx := context.Background()

	// Test calculate handler
	fmt.Println("\nTesting calculate handler:")
	handler, err := registry.GetToolHandler("calculate")
	if err != nil {
		fmt.Printf("Error getting calculate handler: %v\n", err)
	} else {
		result, err := handler.Handle(ctx, map[string]interface{}{
			"operation": "add",
			"a":         10,
			"b":         5,
		})
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("10 + 5 = %v\n", result)
		}

		result, err = handler.Handle(ctx, map[string]interface{}{
			"operation": "sqrt",
			"a":         16,
		})
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("sqrt(16) = %v\n", result)
		}
	}

	// Test echo handler
	fmt.Println("\nTesting echo handler:")
	handler, err = registry.GetToolHandler("echo")
	if err != nil {
		fmt.Printf("Error getting echo handler: %v\n", err)
	} else {
		result, err := handler.Handle(ctx, map[string]interface{}{
			"message": "Hello, MCP!",
		})
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Echo result: %v\n", result)
		}
	}

	// Test time handler
	fmt.Println("\nTesting time handler:")
	handler, err = registry.GetToolHandler("time")
	if err != nil {
		fmt.Printf("Error getting time handler: %v\n", err)
	} else {
		result, err := handler.Handle(ctx, map[string]interface{}{
			"format": "Kitchen",
		})
		if err != nil {
			fmt.Printf("Error: %v\n", err)
		} else {
			fmt.Printf("Current time: %v\n", result)
		}
	}
}
