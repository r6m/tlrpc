package generator

import (
	"bytes"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
)

func TestServiceGenerator_SimpleSchema(t *testing.T) {
	data := readTestSchema(t, "simple.tl")
	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var servicesBuf bytes.Buffer
	var registerBuf bytes.Buffer
	var requestsBuf bytes.Buffer
	gen := NewServiceGenerator(naming.NewNamer(), &servicesBuf)
	reg := NewServiceGenerator(naming.NewNamer(), &registerBuf)
	req := NewServiceGenerator(naming.NewNamer(), &requestsBuf)

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
	if !strings.Contains(services, "type AuthServer interface") {
		t.Fatalf("expected AuthServer interface")
	}
	if !strings.Contains(services, "SendCode(ctx context.Context, req *AuthSendCodeRequest) (*AuthSentCode, error)") {
		t.Fatalf("expected SendCode signature")
	}

	register := registerBuf.String()
	if !strings.Contains(register, "func RegisterAuthServer") {
		t.Fatalf("expected RegisterAuthServer")
	}
	if !strings.Contains(register, "s.RegisterConstructor(0xa677244f") {
		t.Fatalf("expected constructor registration")
	}
	if !strings.Contains(register, "s.RegisterMethod(0xa677244f") {
		t.Fatalf("expected method registration")
	}

	requests := requestsBuf.String()
	if !strings.Contains(requests, "type AuthSendCodeRequest struct") {
		t.Fatalf("expected AuthSendCodeRequest")
	}
	if !strings.Contains(requests, "PhoneNumber string") {
		t.Fatalf("expected PhoneNumber field")
	}
	if !strings.Contains(requests, "APIID int32") {
		t.Fatalf("expected APIID field")
	}
}
