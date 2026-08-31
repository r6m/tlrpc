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
	if !strings.Contains(register, "var Auth_ServiceDesc = tlrpc.ServiceDesc{") {
		t.Fatalf("expected static service descriptor")
	}
	if !strings.Contains(register, "ServiceName: \"auth\"") {
		t.Fatalf("expected TL service namespace in descriptor")
	}
	if !strings.Contains(register, "SchemaLayer: SchemaLayer") {
		t.Fatalf("expected descriptor layer ownership from generated schema metadata")
	}
	if !strings.Contains(register, "HandlerType: (*AuthServer)(nil)") {
		t.Fatalf("expected service handler type")
	}
	if !strings.Contains(register, "func _Auth_SendCode_Handler(srv interface{}, ctx context.Context, req *AuthSendCodeRequest) (*AuthSentCode, error)") {
		t.Fatalf("expected typed method handler")
	}
	if !strings.Contains(register, "return srv.(AuthServer).SendCode(ctx, req)") {
		t.Fatalf("expected method handler to invoke the generated interface")
	}
	if !strings.Contains(register, "ConstructorID: 0xa677244f") {
		t.Fatalf("expected method constructor ID")
	}
	if !strings.Contains(register, "NewRequest: func() tlrpc.TLObject { return &AuthSendCodeRequest{} }") {
		t.Fatalf("expected request constructor")
	}
	if !strings.Contains(register, "Handler: _Auth_SendCode_Handler") {
		t.Fatalf("expected static method handler")
	}
	if !strings.Contains(register, "s.RegisterService(Auth_ServiceDesc, srv)") {
		t.Fatalf("expected descriptor registration")
	}
	if strings.Contains(register, "s.RegisterConstructor(") || strings.Contains(register, "s.RegisterMethod(") {
		t.Fatalf("expected registration to use RegisterService exclusively")
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
	if !strings.Contains(content, "mtproto.EnterObject(rd)") {
		t.Fatalf("expected generated request decode budget entry")
	}
	if !strings.Contains(content, "mtproto.PrependReader(boxedCtor.Bytes(), rd)") {
		t.Fatalf("expected request constructor replay to preserve decode budget")
	}
	if strings.Contains(content, "io.MultiReader(&boxedCtor, rd)") {
		t.Fatalf("expected request constructor replay not to hide decode budget")
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

func TestServiceGenerator_RequestSupportsMultiFlagSets(t *testing.T) {
	const schemaText = `---types---
boolTrue#997275b5 = Bool;

---functions---
messages.testMultiFlags#01020304 flags:# flags2:# silent:flags.0?true close_friend:flags2.2?true about:flags2.3?string = Bool;
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
	if !strings.Contains(content, "flags2 := uint32(0)") {
		t.Fatalf("expected flags2 local variable in request serialize")
	}
	if !strings.Contains(content, "if r.CloseFriend {\n\t\tflags2 |= 1 << 2") {
		t.Fatalf("expected flags2 bool bit computation in request serialize")
	}
	if !strings.Contains(content, "if flags2&(1<<3) != 0") {
		t.Fatalf("expected flags2 optional gate in request serialize")
	}
	if !strings.Contains(content, "closeFriend = flags2&(1<<2) != 0") && !strings.Contains(content, "r.CloseFriend = flags2&(1<<2) != 0") {
		t.Fatalf("expected flags2 bool assignment in request deserialize")
	}
}
