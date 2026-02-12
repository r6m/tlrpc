package registry

import (
	"context"
	"testing"
)

func TestRegistryRegisterAndLookup(t *testing.T) {
	registry := New()
	desc := ServiceDesc{
		ServiceName: "auth",
		Methods: []MethodDesc{
			{
				MethodName: "auth.sendCode",
				Handler: func(ctx context.Context, req interface{}) (interface{}, error) {
					return nil, nil
				},
			},
		},
	}
	if err := registry.Register(desc, &struct{}{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if _, ok := registry.GetMethod("auth.sendCode"); !ok {
		t.Fatalf("expected method lookup")
	}
}

func TestRegistryDuplicate(t *testing.T) {
	registry := New()
	desc := ServiceDesc{ServiceName: "auth"}
	if err := registry.Register(desc, &struct{}{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	if err := registry.Register(desc, &struct{}{}); err == nil {
		t.Fatalf("expected duplicate registration error")
	}
}

func TestRegistryMethodCollision(t *testing.T) {
	registry := New()
	desc := ServiceDesc{
		ServiceName: "auth",
		Methods: []MethodDesc{
			{MethodName: "sendCode", Handler: func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }},
		},
	}
	if err := registry.Register(desc, &struct{}{}); err != nil {
		t.Fatalf("register: %v", err)
	}
	second := ServiceDesc{
		ServiceName: "auth2",
		Methods: []MethodDesc{
			{MethodName: "auth.sendCode", Handler: func(ctx context.Context, req interface{}) (interface{}, error) { return nil, nil }},
		},
	}
	if err := registry.Register(second, &struct{}{}); err == nil {
		t.Fatalf("expected method collision error")
	}
}
