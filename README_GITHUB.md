# GoMCP SDK - Universal Model Context Protocol for Go

[![Go Version](https://img.shields.io/badge/Go-1.21+-00ADD8?style=flat&logo=go)](https://go.dev)
[![MCP Version](https://img.shields.io/badge/MCP-2024--11--05-blue?style=flat)](https://modelcontextprotocol.io)
[![License](https://img.shields.io/badge/License-MIT-yellow.svg?style=flat)](LICENSE)
[![Go Reference](https://pkg.go.dev/badge/github.com/fredcamaral/gomcp-sdk.svg)](https://pkg.go.dev/github.com/fredcamaral/gomcp-sdk)

A comprehensive, production-ready Go SDK for the [Model Context Protocol](https://modelcontextprotocol.io) (MCP) that works with ANY MCP-compatible client.

## 🌟 Highlights

- **🌍 Universal Compatibility**: Works with Claude, VS Code, Cursor, Continue, and ANY MCP client
- **🚀 Zero Dependencies**: Pure Go implementation
- **⚡ High Performance**: < 1ms latency, production-tested
- **🎯 100% MCP Compliant**: Full protocol implementation
- **🔌 Extensible**: Plugin system with hot-reloading
- **🤝 Client Adaptive**: Auto-detects and adapts to client capabilities

## 📦 Installation

```bash
go get github.com/fredcamaral/gomcp-sdk
```

## 🚀 Quick Start

```go
package main

import (
    "context"
    "log"
    
    "github.com/fredcamaral/gomcp-sdk/server"
    "github.com/fredcamaral/gomcp-sdk/transport"
)

func main() {
    // Create a server that works with ANY MCP client
    srv := server.NewServer("my-app", "1.0.0")
    
    // Add a tool
    srv.AddTool(/* tool definition */)
    
    // Start with stdio transport (for desktop clients)
    srv.SetTransport(transport.NewStdioTransport())
    
    if err := srv.Start(context.Background()); err != nil {
        log.Fatal(err)
    }
}
```

## 📚 Features

### Core MCP Features
- **Tools**: Execute custom functions with JSON schema validation
- **Resources**: Expose data and files via URI
- **Prompts**: Template-based content generation

### Advanced Features
- **Sampling**: LLM integration for AI responses
- **Roots**: File system access points
- **Discovery**: Dynamic plugin registration
- **Subscriptions**: Real-time updates
- **Notifications**: Event system

### Production Features
- Multiple transports (HTTP, WebSocket, SSE, stdio)
- Middleware support (auth, rate limiting, logging)
- Health checks and metrics
- Kubernetes ready

## 🤝 Client Compatibility

| Client | Supported Features |
|--------|-------------------|
| Claude Desktop | Tools, Resources, Prompts |
| VS Code Copilot | Tools, Discovery, Roots |
| Cursor | Tools |
| Continue | Tools, Resources, Prompts |
| Cline | Tools, Resources |
| **Any MCP Client** | Auto-detection & adaptation |

## 📖 Documentation

- [Getting Started Guide](docs/guides/TUTORIAL.md)
- [API Reference](https://pkg.go.dev/github.com/fredcamaral/gomcp-sdk)
- [Integration Guide](docs/INTEGRATION_GUIDE.md)
- [Examples](examples/)

## 🛠️ Examples

See the [examples](examples/) directory for:
- Basic tool server
- File browser with roots
- LLM integration with sampling
- Plugin system demo
- Full-featured server

## 🤝 Contributing

We welcome contributions! Please see [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## 📄 License

MIT License - see [LICENSE](LICENSE) for details.

## 🙏 Acknowledgments

- [Anthropic](https://anthropic.com) for creating the Model Context Protocol
- The Go community for excellent tooling
- All contributors and early adopters

---

Built with ❤️ for the MCP community