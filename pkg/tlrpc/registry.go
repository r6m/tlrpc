package tlrpc

import (
	"context"
	"fmt"
	"reflect"
	"sync"
)

// serviceRegistry manages registered services.
type serviceRegistry struct {
	mu       sync.RWMutex
	services map[string]*serviceInfo
}

type serviceInfo struct {
	desc  ServiceDesc
	impl  interface{}
	value reflect.Value
}

// newServiceRegistry creates a new service registry.
func newServiceRegistry() *serviceRegistry {
	return &serviceRegistry{
		services: make(map[string]*serviceInfo),
	}
}

// register registers a service.
func (r *serviceRegistry) register(desc ServiceDesc, impl interface{}) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.services[desc.ServiceName]; exists {
		panic(fmt.Sprintf("service %s already registered", desc.ServiceName))
	}

	r.services[desc.ServiceName] = &serviceInfo{
		desc:  desc,
		impl:  impl,
		value: reflect.ValueOf(impl),
	}
}

// lookup looks up a service method.
func (r *serviceRegistry) lookup(serviceName, methodName string) (Handler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	service, exists := r.services[serviceName]
	if !exists {
		return nil, false
	}

	for _, method := range service.desc.Methods {
		if method.MethodName == methodName {
			return r.wrapHandler(service, method.Handler), true
		}
	}

	return nil, false
}

// wrapHandler wraps a method handler with reflection.
func (r *serviceRegistry) wrapHandler(service *serviceInfo, handler Handler) Handler {
	return func(ctx context.Context, req interface{}) (interface{}, error) {
		return handler(ctx, req)
	}
}

// getServiceNames returns all registered service names.
func (r *serviceRegistry) getServiceNames() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	names := make([]string, 0, len(r.services))
	for name := range r.services {
		names = append(names, name)
	}
	return names
}