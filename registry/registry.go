package registry

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"sync"
)

// Handler represents a request handler function.
type Handler func(ctx context.Context, req interface{}) (interface{}, error)

// Interceptor represents middleware for request/response processing.
type Interceptor func(next Handler) Handler

// ServiceDesc describes a service for registration.
type ServiceDesc struct {
	ServiceName string
	HandlerType interface{}
	Methods     []MethodDesc
}

// MethodDesc describes a method within a service.
type MethodDesc struct {
	MethodName string
	Handler    func(ctx context.Context, req interface{}) (interface{}, error)
}

// Registry stores registered services.
type Registry struct {
	mu       sync.RWMutex
	services map[string]*serviceInfo
	methods  map[string]MethodDesc
}

type serviceInfo struct {
	desc ServiceDesc
	impl interface{}
}

// New creates a new registry.
func New() *Registry {
	return &Registry{
		services: make(map[string]*serviceInfo),
		methods:  make(map[string]MethodDesc),
	}
}

// Register registers a service implementation.
func (r *Registry) Register(desc ServiceDesc, impl interface{}) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if desc.ServiceName == "" {
		return fmt.Errorf("service name is required")
	}
	if impl == nil {
		return fmt.Errorf("implementation cannot be nil")
	}
	if _, exists := r.services[desc.ServiceName]; exists {
		return fmt.Errorf("service %s already registered", desc.ServiceName)
	}
	if err := r.validateImplementation(desc, impl); err != nil {
		return err
	}

	r.services[desc.ServiceName] = &serviceInfo{desc: desc, impl: impl}
	for _, m := range desc.Methods {
		fullName := m.MethodName
		if !strings.Contains(fullName, ".") {
			fullName = desc.ServiceName + "." + m.MethodName
		}
		if _, exists := r.methods[fullName]; exists {
			return fmt.Errorf("method %s already registered", fullName)
		}
		r.methods[fullName] = m
	}

	return nil
}

func (r *Registry) validateImplementation(desc ServiceDesc, impl interface{}) error {
	implType := reflect.TypeOf(impl)
	if implType.Kind() != reflect.Ptr && implType.Kind() != reflect.Interface {
		return fmt.Errorf("implementation must be pointer or interface")
	}
	if desc.HandlerType == nil {
		return nil
	}
	handlerType := reflect.TypeOf(desc.HandlerType)
	if handlerType.Kind() == reflect.Ptr {
		handlerType = handlerType.Elem()
	}
	if !implType.AssignableTo(handlerType) && !implType.Implements(handlerType) {
		return fmt.Errorf("implementation does not match handler type")
	}
	return nil
}

// GetMethod returns a method by full name.
func (r *Registry) GetMethod(name string) (MethodDesc, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	m, ok := r.methods[name]
	return m, ok
}

// ListServices returns registered service names.
func (r *Registry) ListServices() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	services := make([]string, 0, len(r.services))
	for name := range r.services {
		services = append(services, name)
	}
	return services
}

// ListMethods returns registered method names.
func (r *Registry) ListMethods() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	methods := make([]string, 0, len(r.methods))
	for name := range r.methods {
		methods = append(methods, name)
	}
	return methods
}
