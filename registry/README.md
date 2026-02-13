# Service Registry

This package implements the service registration and method routing system for TLRPC applications.

## Overview

The service registry provides:
- Method-to-handler routing
- Interceptor chain management
- Service descriptor registration
- Request/response serialization coordination

## Key Components

### Service Registration
```go
type ServiceDesc struct {
    ServiceName string
    HandlerType interface{}
    Methods     []MethodDesc
}

type MethodDesc struct {
    MethodName string
    Handler    func(ctx context.Context, req interface{}) (interface{}, error)
}

func (s *Server) RegisterService(sd ServiceDesc, ss interface{})
```

### Routing Flow
1. Extract method name from incoming TL object
2. Find registered handler in service registry
3. Build and execute interceptor chain
4. Call user service implementation
5. Serialize response using the configured codec

## Interceptors

Support for request/response interceptors including:
- Authentication middleware
- Logging and metrics
- Request validation
- Error handling

## Integration

Works with the codec to handle constructor decoding and with the protocol layer for message processing.
