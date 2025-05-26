package testutil

import (
	"context"
	"github.com/fredcamaral/gomcp-sdk/protocol"
	"github.com/fredcamaral/gomcp-sdk/server"
)

// ServerBuilder helps build test servers
type ServerBuilder struct {
	server *TestServer
	autoStart bool
}

// NewServerBuilder creates a new server builder
func NewServerBuilder(name, version string) *ServerBuilder {
	return &ServerBuilder{
		server: NewTestServer(name, version),
	}
}

// WithSimpleTool adds a simple tool that returns a fixed result
func (b *ServerBuilder) WithSimpleTool(name, result string) *ServerBuilder {
	b.server.AddTool(protocol.Tool{
		Name:        name,
		Description: "Test tool",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{},
		},
	}, protocol.ToolHandlerFunc(func(ctx context.Context, params map[string]interface{}) (interface{}, error) {
		return result, nil
	}))
	return b
}

// WithResource adds a test resource
func (b *ServerBuilder) WithResource(uri, name, content string) *ServerBuilder {
	b.server.AddResource(protocol.Resource{
		URI:  uri,
		Name: name,
	}, server.ResourceHandlerFunc(func(ctx context.Context, uri string) ([]protocol.Content, error) {
		return []protocol.Content{{
			Type: "text",
			Text: content,
		}}, nil
	}))
	return b
}

// WithPrompt adds a test prompt
func (b *ServerBuilder) WithPrompt(name, content string) *ServerBuilder {
	b.server.AddPrompt(protocol.Prompt{
		Name:        name,
		Description: "Test prompt",
		Arguments: []protocol.PromptArgument{},
	}, server.PromptHandlerFunc(func(ctx context.Context, args map[string]string) (*protocol.GetPromptResult, error) {
		return &protocol.GetPromptResult{
			Messages: []protocol.PromptMessage{{
				Role: "user",
				Content: []protocol.Content{{
					Type: "text",
					Text: content,
				}},
			}},
		}, nil
	}))
	return b
}

// WithAutoStart marks the server to start automatically
func (b *ServerBuilder) WithAutoStart() *ServerBuilder {
	b.autoStart = true
	return b
}

// Build builds and optionally starts the test server
func (b *ServerBuilder) Build() *TestServer {
	if b.autoStart {
		b.server.Start()
	}
	return b.server
}