# FAQ

## General

### What is TLRPC?

TLRPC is a framework for building Telegram-compatible servers in Go. It handles the complexities of MTProto protocol, multi-layer support, and encryption, allowing you to focus on implementing business logic.

### Is TLRPC production-ready?

TLRPC is currently in alpha. The API may change, and it's not recommended for production use yet.

### How does TLRPC differ from the official Telegram server implementations?

TLRPC is a framework that provides building blocks for creating Telegram-compatible servers. It's not a complete server implementation like the official ones.

## Technical

### Can I use TLRPC with existing Telegram clients?

Yes, TLRPC implements the MTProto protocol that Telegram clients use. However, you'll need to implement the full Telegram API surface.

### What layers does TLRPC support?

TLRPC supports multiple layers and can handle clients from different Telegram versions automatically.

### How do I handle different client layer versions?

TLRPC handles layer negotiation automatically. You can implement different logic based on the client's layer version in your service methods.

### Is TLRPC thread-safe?

Yes, TLRPC is designed to be thread-safe. Each connection gets its own goroutine, and the framework handles concurrency appropriately.

## Development

### How do I add a new service?

1. Define your service in a TL schema file
2. Run `tlrpc-gen` to generate Go code
3. Implement the generated interface
4. Register the service with the server

### How do I add middleware?

Use interceptors:

```go
server := tlrpc.NewServer(
    tlrpc.WithInterceptor(loggingInterceptor),
    tlrpc.WithInterceptor(authInterceptor),
)
```

### How do I handle authentication?

Implement authentication logic in interceptors and store user information in the context.

### How do I test my services?

Use the built-in testing utilities and write both unit tests for your business logic and integration tests for full request/response cycles.

## Troubleshooting

### I'm getting "method not found" errors

Make sure you've registered your service with the server:

```go
gen.RegisterMyServiceServer(server, &MyService{})
```

### My generated code doesn't compile

Check your TL schema for syntax errors. Make sure all types are properly defined.

### Performance is slow

- Use connection pooling for databases
- Implement caching for frequently accessed data
- Use the built-in metrics to identify bottlenecks
- Consider using a faster transport if appropriate

### Memory usage is high

- Check for goroutine leaks
- Use object pooling where appropriate
- Monitor the session store size
- Implement proper cleanup for expired sessions

## Contributing

### How can I contribute?

See the [Contributing Guide](../CONTRIBUTING.md) for details.

### I found a bug

Please file an issue on GitHub with a detailed description and steps to reproduce.

### I want to add a feature

Open an issue describing the feature you'd like to add. We'll discuss the design and implementation approach.