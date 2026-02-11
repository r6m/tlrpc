# Best Practices

This document outlines best practices for building servers with TLRPC.

## Code Organization

### Service Structure

Organize your services by domain:

```
pkg/
├── auth/
│   ├── service.go
│   └── models.go
├── users/
│   ├── service.go
│   └── models.go
└── messages/
    ├── service.go
    └── models.go
```

### Error Handling

Use consistent error handling patterns:

```go
func (s *UserService) GetUser(ctx context.Context, req *gen.GetUserRequest) (*gen.User, error) {
    if req.UserID <= 0 {
        return nil, tlrpc.NewRPCError(400, "invalid user ID")
    }

    user, err := s.db.GetUser(req.UserID)
    if err != nil {
        if errors.Is(err, sql.ErrNoRows) {
            return nil, tlrpc.NewRPCError(404, "user not found")
        }
        return nil, tlrpc.ErrInternalError
    }

    return user, nil
}
```

## Performance

### Connection Pooling

Reuse database connections:

```go
type Service struct {
    db *sql.DB
}

func NewService(db *sql.DB) *Service {
    return &Service{db: db}
}
```

### Caching

Cache frequently accessed data:

```go
func (s *Service) GetUser(ctx context.Context, req *gen.GetUserRequest) (*gen.User, error) {
    // Check cache first
    if user := s.cache.Get(req.UserID); user != nil {
        return user, nil
    }

    // Fetch from database
    user, err := s.db.GetUser(req.UserID)
    if err != nil {
        return nil, err
    }

    // Cache the result
    s.cache.Set(req.UserID, user)
    return user, nil
}
```

## Security

### Input Validation

Always validate input:

```go
func (s *Service) UpdateProfile(ctx context.Context, req *gen.UpdateProfileRequest) (*gen.User, error) {
    if len(req.FirstName) > 100 {
        return nil, tlrpc.NewRPCError(400, "first name too long")
    }

    if len(req.Bio) > 500 {
        return nil, tlrpc.NewRPCError(400, "bio too long")
    }

    // ... rest of implementation
}
```

### Authentication

Check authentication in interceptors:

```go
func authInterceptor(next tlrpc.Handler) tlrpc.Handler {
    return func(ctx context.Context, req interface{}) (interface{}, error) {
        userID := tlrpc.UserIDFromContext(ctx)
        if userID == 0 {
            return nil, tlrpc.ErrUnauthorized
        }

        // Check permissions based on the request
        if requiresAuth(req) && !isAuthenticated(ctx) {
            return nil, tlrpc.ErrUnauthorized
        }

        return next(ctx, req)
    }
}
```

## Testing

### Unit Tests

Test your business logic:

```go
func TestUserService_GetUser(t *testing.T) {
    mockDB := &mockUserDB{}
    service := NewUserService(mockDB)

    req := &gen.GetUserRequest{UserID: 123}
    user, err := service.GetUser(context.Background(), req)

    assert.NoError(t, err)
    assert.Equal(t, int64(123), user.ID)
    assert.Equal(t, "John Doe", user.FirstName)
}
```

### Integration Tests

Test full request/response cycles:

```go
func TestServer_GetUser(t *testing.T) {
    server := testutil.TestServer(t)
    gen.RegisterUserServer(server, &UserService{})

    client := testutil.TestClient(t, server)

    resp, err := client.Call(context.Background(), &gen.GetUserRequest{UserID: 123})

    testutil.AssertNoError(t, err)
    user := resp.(*gen.User)
    assert.Equal(t, int64(123), user.ID)
}
```

## Deployment

### Health Checks

Implement health check endpoints:

```go
func (s *Service) Health(ctx context.Context, req *gen.HealthRequest) (*gen.HealthResponse, error) {
    // Check database connectivity
    if err := s.db.Ping(); err != nil {
        return &gen.HealthResponse{Status: "unhealthy"}, nil
    }

    // Check other dependencies
    if err := s.cache.Ping(); err != nil {
        return &gen.HealthResponse{Status: "unhealthy"}, nil
    }

    return &gen.HealthResponse{Status: "healthy"}, nil
}
```

### Metrics

Add metrics collection:

```go
func metricsInterceptor(next tlrpc.Handler) tlrpc.Handler {
    return func(ctx context.Context, req interface{}) (interface{}, error) {
        start := time.Now()
        resp, err := next(ctx, req)
        duration := time.Since(start)

        // Record metrics
        metrics.RequestDuration.WithLabelValues(reflect.TypeOf(req).Name()).Observe(duration.Seconds())
        if err != nil {
            metrics.RequestErrors.WithLabelValues(reflect.TypeOf(req).Name()).Inc()
        }

        return resp, err
    }
}
```

### Logging

Use structured logging:

```go
func loggingInterceptor(logger *zap.Logger) tlrpc.Interceptor {
    return func(next tlrpc.Handler) tlrpc.Handler {
        return func(ctx context.Context, req interface{}) (interface{}, error) {
            logger.Info("handling request",
                zap.String("method", reflect.TypeOf(req).Name()),
                zap.Int64("user_id", tlrpc.UserIDFromContext(ctx)),
            )

            resp, err := next(ctx, req)

            if err != nil {
                logger.Error("request failed",
                    zap.Error(err),
                    zap.String("method", reflect.TypeOf(req).Name()),
                )
            }

            return resp, err
        }
    }
}
```