# Tutorial

## 1) Define schema

```tl
---types---
user#8f97c628 id:long first_name:string = User;

---functions---
auth.sendCode#a677244f phone_number:string api_id:int api_hash:string = auth.SentCode;
```

## 2) Generate code

```bash
tlrpc-gen --schema=./schema.tl --out=./gen --package=gen
```

## 3) Implement service

```go
type AuthService struct {
    gen.UnimplementedAuthServer
}

func (s *AuthService) SendCode(ctx context.Context, req *gen.AuthSendCodeRequest) (*gen.AuthSentCode, error) {
    if req.PhoneNumber == "" {
        return nil, tlrpc.NewBadRequestError("PHONE_NUMBER_EMPTY")
    }
    return &gen.AuthSentCode{PhoneCodeHash: "abc123"}, nil
}
```

## 4) Register and serve

```go
srv := tlrpc.NewServer(tlrpc.WithUnaryInterceptor(tlrpc.LoggingInterceptor(logger)))
gen.RegisterAuthServer(srv, &AuthService{})
log.Fatal(srv.Serve(listener))
```

Then read [Dispatch Pipeline](../concepts/dispatch-pipeline.md) to understand wire-to-handler flow.
