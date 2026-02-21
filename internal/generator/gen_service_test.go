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
	gen := NewServiceGenerator(naming.NewNamer(), schema, &servicesBuf)
	reg := NewServiceGenerator(naming.NewNamer(), schema, &registerBuf)
	req := NewServiceGenerator(naming.NewNamer(), schema, &requestsBuf)

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
	if !strings.Contains(requests, "func (r *AuthSendCodeRequest) TLName() string") {
		t.Fatalf("expected TLName method on request")
	}
}

func TestServiceGenerator_RequestVectorUnionDeserializeUsesConstructorDispatch(t *testing.T) {
	const schemaText = `---types---
inputUserSelf#f7c1b13f = InputUser;
inputUser#f21158c6 user_id:long access_hash:long = InputUser;
userEmpty#d3bc4b7c id:long = User;

---functions---
users.getUsers#0d91a548 id:Vector<InputUser> = Vector<User>;
`
	p := parser.NewParser(schemaText)
	schema, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var requestsBuf bytes.Buffer
	req := NewServiceGenerator(naming.NewNamer(), schema, &requestsBuf)
	if err := req.GenerateRequests(schema.Functions); err != nil {
		t.Fatalf("generate requests: %v", err)
	}
	content := requestsBuf.String()
	if !strings.Contains(content, "GetStaticConstructors()[ctorID]") {
		t.Fatalf("expected constructor dispatch in request vector decode")
	}
	if strings.Contains(content, "var item InputUserType\n\t\tif err := item.DeserializeTL(rd); err != nil {") {
		t.Fatalf("expected to avoid direct nil-interface DeserializeTL call in vectors")
	}
}

func TestServiceGenerator_RequestTrueFlagsUsePresenceBitsOnly(t *testing.T) {
	const schemaText = `---types---
boolTrue#997275b5 = Bool;

---functions---
messages.testTrueFlags#01020304 flags:# silent:flags.0?true id:int = Bool;
`
	p := parser.NewParser(schemaText)
	schema, err := p.Parse()
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	var requestsBuf bytes.Buffer
	req := NewServiceGenerator(naming.NewNamer(), schema, &requestsBuf)
	if err := req.GenerateRequests(schema.Functions); err != nil {
		t.Fatalf("generate requests: %v", err)
	}
	content := requestsBuf.String()
	if strings.Contains(content, "WriteBool(w, r.Silent)") {
		t.Fatalf("expected flags.0?true to avoid bool payload in serialize")
	}
	if strings.Contains(content, "ReadBool(rd)") {
		t.Fatalf("expected flags.0?true to avoid bool payload in deserialize")
	}
	if !strings.Contains(content, "r.Silent = flags&(1<<0) != 0") {
		t.Fatalf("expected true flag field assignment from flags bit")
	}
}
