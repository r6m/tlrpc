package codegen

import (
	"bytes"
	"testing"
)

func TestServiceGenerator_SimpleSchema(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var servicesBuf bytes.Buffer
	var registerBuf bytes.Buffer
	var requestsBuf bytes.Buffer
	gen := NewServiceGenerator(NewNamer(), &servicesBuf)
	reg := NewServiceGenerator(NewNamer(), &registerBuf)
	req := NewServiceGenerator(NewNamer(), &requestsBuf)

	if err := gen.GenerateService(schema.Functions); err != nil {
		t.Fatalf("generate service: %v", err)
	}
	if err := reg.GenerateRegistration(schema.Functions); err != nil {
		t.Fatalf("generate registration: %v", err)
	}
	if err := req.GenerateRequests(schema.Functions); err != nil {
		t.Fatalf("generate requests: %v", err)
	}

	services := servicesBuf.String()
	if !contains(services, "type AuthServer interface") {
		t.Fatalf("expected AuthServer interface")
	}
	if !contains(services, "SendCode(ctx context.Context, req *AuthSendCodeRequest) (*SentCode, error)") {
		t.Fatalf("expected SendCode signature")
	}

	register := registerBuf.String()
	if !contains(register, "func RegisterAuthServer") {
		t.Fatalf("expected RegisterAuthServer")
	}
	if !contains(register, "MethodName: \"auth.sendCode\"") {
		t.Fatalf("expected method name mapping")
	}

	requests := requestsBuf.String()
	if !contains(requests, "type AuthSendCodeRequest struct") {
		t.Fatalf("expected AuthSendCodeRequest")
	}
	if !contains(requests, "PhoneNumber string") {
		t.Fatalf("expected PhoneNumber field")
	}
	if !contains(requests, "APIID int32") {
		t.Fatalf("expected APIID field")
	}
}
