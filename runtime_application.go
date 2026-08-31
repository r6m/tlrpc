package tlrpc

import (
	"context"
	"fmt"
	"sync"

	runtimev2 "github.com/r6m/tlrpc/internal/runtime"
)

type runtimeApplicationMethod struct {
	handler    func(context.Context, TLObject) (interface{}, error)
	fullMethod string
}

// runtimeApplicationDispatcher is the root-package boundary between generated
// service registrations and Runtime v2. Its method table is an immutable
// snapshot containing only methods declared by per-server ServiceDesc values.
type runtimeApplicationDispatcher struct {
	server   *Server
	methods  map[uint32]runtimeApplicationMethod
	decoder  *dispatcher
	setupErr error
}

func newRuntimeApplicationDispatcher(server *Server) *runtimeApplicationDispatcher {
	adapter := &runtimeApplicationDispatcher{
		server:  server,
		methods: make(map[uint32]runtimeApplicationMethod),
		decoder: newDispatcher(),
	}
	if server == nil {
		adapter.setupErr = fmt.Errorf("tlrpc: Runtime v2 application dispatcher requires a server")
		return adapter
	}

	for serviceName, service := range server.services {
		if service == nil {
			adapter.setupErr = fmt.Errorf("tlrpc: generated service %q has no registration", serviceName)
			return adapter
		}
		for _, method := range service.desc.Methods {
			constructor, constructorOK := server.dispatcher.LookupConstructor(method.ConstructorID)
			handler, handlerOK := server.dispatcher.LookupMethod(method.ConstructorID)
			if !constructorOK || !handlerOK {
				adapter.setupErr = fmt.Errorf("tlrpc: generated method %q is not fully registered", method.MethodName)
				return adapter
			}
			if _, duplicate := adapter.methods[method.ConstructorID]; duplicate {
				adapter.setupErr = fmt.Errorf("tlrpc: duplicate generated method constructor 0x%08x", method.ConstructorID)
				return adapter
			}
			adapter.methods[method.ConstructorID] = runtimeApplicationMethod{
				handler:    handler,
				fullMethod: "/" + service.desc.ServiceName + "/" + method.MethodName,
			}
			adapter.decoder.RegisterConstructor(method.ConstructorID, constructor)
		}
	}
	return adapter
}

func (a *runtimeApplicationDispatcher) DispatchApplication(ctx context.Context, request runtimev2.Request) (outcome runtimev2.Outcome, err error) {
	requestMessageID := request.Message.MessageID
	defer func() {
		if recover() != nil {
			outcome = runtimeApplicationFailure(requestMessageID, genericInternalError())
			err = nil
		}
	}()

	if a == nil {
		return runtimev2.Outcome{}, fmt.Errorf("tlrpc: nil Runtime v2 application dispatcher")
	}
	if a.setupErr != nil {
		return runtimev2.Outcome{}, a.setupErr
	}
	method, ok := a.methods[request.Message.ConstructorID]
	if !ok {
		return runtimeApplicationFailure(requestMessageID, NewNotFoundError("METHOD_NOT_FOUND")), nil
	}

	decoded, remaining, err := decodeTLObjectWithBudget(a.decoder, request.Message.Body, request.Message.DecodeBudget)
	if err != nil {
		return runtimeApplicationFailure(requestMessageID, NewBadRequestError("REQUEST_DECODE_FAILED")), nil
	}
	if decoded == nil || decoded.ConstructorID() != request.Message.ConstructorID {
		return runtimeApplicationFailure(requestMessageID, NewBadRequestError("REQUEST_DECODE_FAILED")), nil
	}
	if remaining.Len() != 0 {
		return runtimeApplicationFailure(requestMessageID, NewBadRequestError("REQUEST_TRAILING_BYTES")), nil
	}

	collector := &runtimeMutationCollector{}
	ctx = runtimeApplicationHandlerContext(ctx, request, collector)
	if err := a.server.acquireHandler(ctx); err != nil {
		return runtimeApplicationFailure(requestMessageID, err), nil
	}
	defer a.server.releaseHandler()

	handler := func(callCtx context.Context, value interface{}) (response interface{}, err error) {
		defer func() {
			if recover() != nil {
				response = nil
				err = genericInternalError()
			}
		}()
		object, valid := value.(TLObject)
		if !valid {
			return nil, NewInternalError("INVALID_INTERCEPTOR_REQUEST")
		}
		return method.handler(callCtx, object)
	}

	var (
		response interface{}
		callErr  error
	)
	if len(a.server.unaryInterceptors) == 0 {
		response, callErr = handler(ctx, decoded)
	} else {
		response, callErr = ChainUnaryInterceptors(a.server.unaryInterceptors...)(
			ctx,
			decoded,
			&UnaryServerInfo{FullMethod: method.fullMethod},
			handler,
		)
	}
	if callErr != nil {
		return runtimeApplicationFailure(requestMessageID, callErr), nil
	}
	if response == nil {
		return runtimeApplicationFailure(requestMessageID, NewInternalError("NIL_RESPONSE")), nil
	}

	object, err := normalizeResponse(response)
	if err != nil {
		return runtimeApplicationFailure(requestMessageID, err), nil
	}
	if object == nil {
		return runtimeApplicationFailure(requestMessageID, NewInternalError("NIL_RESPONSE")), nil
	}
	body, err := encodeTLObjectWithLimits(object, EncodeLimits{MaxEncodedBytes: a.server.maxEncodedResponseBytes})
	if err != nil {
		return runtimeApplicationFailure(requestMessageID, NewInternalError("RESPONSE_ENCODE_FAILED")), nil
	}
	return runtimev2.Outcome{
		Intents: []runtimev2.Intent{runtimev2.RPCResult{
			RequestMessageID: requestMessageID,
			Body:             body,
		}},
		Mutations: collector.snapshot(),
	}, nil
}

func runtimeApplicationFailure(requestMessageID int64, err error) runtimev2.Outcome {
	rpcErr := FromError(err)
	return runtimev2.Outcome{Intents: []runtimev2.Intent{runtimev2.RPCError{
		RequestMessageID: requestMessageID,
		Code:             rpcErr.ErrorCode,
		Message:          rpcErr.ErrorMessage,
	}}}
}

type runtimeMutationCollector struct {
	mu        sync.Mutex
	mutations []runtimev2.SessionMutation
}

func (c *runtimeMutationCollector) append(mutation runtimev2.SessionMutation) {
	c.mu.Lock()
	c.mutations = append(c.mutations, mutation)
	c.mu.Unlock()
}

func (c *runtimeMutationCollector) snapshot() []runtimev2.SessionMutation {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]runtimev2.SessionMutation(nil), c.mutations...)
}

type contextKeyRuntimeMutations struct{}

func runtimeApplicationHandlerContext(ctx context.Context, request runtimev2.Request, collector *runtimeMutationCollector) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = context.WithValue(ctx, contextKeyRuntimeMutations{}, collector)
	ctx = withAuthKeyID(ctx, int64(request.Info.AuthKeyID))
	ctx = withUserID(ctx, request.Info.UserID)
	ctx = withLayer(ctx, request.Info.Layer)
	ctx = withBinding(ctx, Binding{
		ConnectionID: request.Info.ConnectionID,
		AuthKeyID:    int64(request.Info.AuthKeyID), SessionID: request.Info.SessionID,
		ServerSalt: request.Info.ServerSalt, UserID: request.Info.UserID, Layer: request.Info.Layer,
	})
	ctx = withClientMetadata(ctx, request.Info.Client)
	if request.Info.Sender != nil {
		ctx = withRuntimeSender(ctx, request.Info.Sender)
	}
	return ctx
}

var _ runtimev2.ApplicationDispatcher = (*runtimeApplicationDispatcher)(nil)
