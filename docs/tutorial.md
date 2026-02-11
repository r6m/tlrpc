# TLRPC Tutorial

This tutorial will guide you through building a Telegram server using TLRPC.

## Prerequisites

- Go 1.21+
- Basic understanding of Telegram's MTProto protocol

## Step 1: Set up your project

Create a new Go module:

```bash
mkdir my-telegram-server
cd my-telegram-server
go mod init my-telegram-server
```

## Step 2: Get TLRPC

Add TLRPC to your dependencies:

```bash
go get github.com/r6m/tlrpc
```

## Step 3: Create your TL schema

Create a `schema.tl` file with your service definitions:

```
---types---
user#8f97c628 id:long first_name:string = User;

---functions---
getUser#12345678 user_id:long = User;
```

## Step 4: Generate code

Install the code generator and generate Go code:

```bash
go install github.com/r6m/tlrpc/cmd/tlrpc-gen@latest
tlrpc-gen --schema=schema.tl --out=./gen
```

## Step 5: Implement your service

Create a service implementation:

```go
package main

import (
    "context"
    "my-telegram-server/gen"
)

type UserService struct {
    gen.UnimplementedUserServer
}

func (s *UserService) GetUser(ctx context.Context, req *gen.GetUserRequest) (*gen.User, error) {
    // Your implementation here
    return &gen.User{
        ID:        req.UserID,
        FirstName: "John Doe",
    }, nil
}
```

## Step 6: Start the server

Create the main server:

```go
package main

import (
    "log"
    "net"

    "github.com/r6m/tlrpc"
    "my-telegram-server/gen"
)

func main() {
    server := tlrpc.NewServer()

    gen.RegisterUserServer(server, &UserService{})

    lis, err := net.Listen("tcp", ":443")
    if err != nil {
        log.Fatal(err)
    }

    log.Println("Server listening on :443")
    server.Serve(lis)
}
```

## Step 7: Test your server

Use a Telegram client or write a test client to connect to your server.

## Next Steps

- Add authentication
- Implement more services
- Add middleware for logging and metrics
- Deploy to production