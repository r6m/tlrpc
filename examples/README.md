# Examples

This directory contains example applications and usage patterns for the TLRPC framework.

## Structure

- `basic-server/`: Simple TLRPC server implementation
- `echo-service/`: Example service that echoes requests
- `auth-service/`: Service with authentication middleware
- `websocket-client/`: WebSocket transport client example
- `custom-transport/`: Custom transport implementation example

## Running Examples

Each example includes a README with setup and running instructions:

```bash
cd examples/basic-server
go run .
```

## Learning Path

1. **basic-server**: Start here for fundamental TLRPC concepts
2. **echo-service**: Learn service registration and method handling
3. **auth-service**: Understand interceptors and middleware
4. **websocket-client**: Explore different transport implementations
5. **custom-transport**: Learn to extend TLRPC with custom components

## Testing

Examples include integration tests demonstrating end-to-end functionality:

```bash
cd examples/basic-server
go test ./...
```