# Discovery Handlers Example

This example demonstrates the gomcp-sdk's plugin discovery and handler loading system.

## Features

- **Embedded Handlers**: Pre-compiled handlers built into the binary
- **Dynamic Discovery**: Automatic discovery of plugins from filesystem
- **Handler Registry**: Centralized management of tool handlers
- **Multiple Handler Types**: Support for embedded, subprocess, and dynamic handlers

## Running the Example

```bash
go run main.go
```

## Handler Types

### 1. Embedded Handlers
Handlers compiled directly into the application:
- Fast execution
- No external dependencies
- Ideal for core functionality

### 2. Subprocess Handlers (Future)
Handlers running as separate processes:
- Language agnostic
- Process isolation
- Communication via JSON-RPC

### 3. Dynamic Handlers (Future)
Handlers loaded at runtime:
- Plugin architecture
- Hot reloading
- Extensibility

## Example Output

```
Discovered tools:
  - calculate: Perform basic calculations
  - echo: Echo a message
  - time: Get current time

Testing calculate handler:
10 + 5 = map[inputs:map[a:10 b:5] operation:add result:15]
sqrt(16) = map[inputs:map[a:16 b:0] operation:sqrt result:4]

Testing echo handler:
Echo result: map[echo:Hello, MCP! timestamp:2024-01-20T10:30:00Z]

Testing time handler:
Current time: map[time:10:30AM zone:Local]
```

## Creating Custom Handlers

1. Implement the `protocol.ToolHandler` interface:
```go
type MyHandler struct{}

func (h *MyHandler) Handle(ctx context.Context, params map[string]interface{}) (interface{}, error) {
    // Handler logic
    return result, nil
}
```

2. Register as an embedded handler:
```go
discovery.RegisterEmbeddedHandler("my-tool", &MyHandler{})
```

3. Or configure in a plugin manifest for dynamic loading.