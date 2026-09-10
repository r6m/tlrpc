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
	if !strings.Contains(register, "func _Auth_SendCode_Handler(srv any, ctx context.Context, req tlrpc.TLObject) (any, error)") {
		t.Fatalf("expected typed method handler")
	}
	if !strings.Contains(register, "typedRequest, ok := req.(*AuthSendCodeRequest)") || !strings.Contains(register, "return srv.(AuthServer).SendCode(ctx, typedRequest)") {
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
	if !strings.Contains(content, "decodeInputUserType(d)") {
		t.Fatalf("expected constructor dispatch in request vector decode")
	}
	if !strings.Contains(content, "d.EnterObject()") {
		t.Fatalf("expected generated request decode budget entry")
	}
	if strings.Contains(content, "PrependReader") || strings.Contains(content, "bytes.Buffer") {
		t.Fatalf("cursor family decode must not replay constructor bytes")
	}
	if strings.Contains(content, "var item InputUserType\n\t\tif err := item.DeserializeTL(rd); err != nil {") {
		t.Fatalf("expected to avoid direct nil-interface DeserializeTL call in vectors")
	}
}

func TestServiceGenerator_LayeredResultChangeReusesRequestAndTypesResponseEncoder(t *testing.T) {
	base, err := parser.NewParser(`---types---
resultA#1 = ResultA;
resultB#2 = ResultB;
---functions---
test.call#10 value:int = ResultA;`).ParseWithLayer(228)
	if err != nil {
		t.Fatal(err)
	}
	difference, err := parser.ParseLayerDifference(`---functions---
test.call#10 value:int = ResultB;`, 229, "229.tl")
	if err != nil {
		t.Fatal(err)
	}
	layered, err := parser.ResolveLayers(base, 228, []parser.LayerDifference{difference})
	if err != nil {
		t.Fatal(err)
	}
	var services, registration, requests bytes.Buffer
	g := NewServiceGenerator(naming.NewNamer(), layered.Schema, &services)
	if err := g.GenerateService(layered.Schema.Functions); err != nil {
		t.Fatal(err)
	}
	if err := NewServiceGenerator(naming.NewNamer(), layered.Schema, &registration).GenerateRegistration(layered.Schema.Functions); err != nil {
		t.Fatal(err)
	}
	if err := NewServiceGenerator(naming.NewNamer(), layered.Schema, &requests).GenerateRequests(layered.Schema.Functions); err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(requests.String(), "type TestCallRequest struct"); got != 1 {
		t.Fatalf("request declarations = %d, want 1\n%s", got, requests.String())
	}
	if !strings.Contains(services.String(), "CallLayer229(ctx context.Context, req *TestCallRequest) (ResultBType, error)") {
		t.Fatalf("missing result-versioned handler:\n%s", services.String())
	}
	if !strings.Contains(registration.String(), "tlrpc.EncodeTypedResponse[ResultAType]") || !strings.Contains(registration.String(), "tlrpc.EncodeTypedResponse[ResultBType]") {
		t.Fatalf("missing exact response encoders:\n%s", registration.String())
	}
	if !strings.Contains(requests.String(), "tlLayerSupports(layer, 228, 0)") {
		t.Fatalf("shared request did not union handler intervals:\n%s", requests.String())
	}
}

func TestServiceGenerator_RejectsBareMethodResults(t *testing.T) {
	schema, err := parser.NewParser(`---types---
item#1 value:int = Item;
---functions---
test.call#10 = !Item;`).Parse()
	if err != nil {
		t.Fatal(err)
	}
	err = NewServiceGenerator(naming.NewNamer(), schema, &bytes.Buffer{}).GenerateService(schema.Functions)
	if err == nil || !strings.Contains(err.Error(), "bare result layouts are unsupported") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestZeroValueSupportsDoubleAlias(t *testing.T) {
	if got := zeroValue("Double"); got != "0" {
		t.Fatalf("zeroValue(Double) = %q, want 0", got)
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
	if !strings.Contains(content, "if r.CloseFriend { flags2 |= 1 << 2 }") {
		t.Fatalf("expected flags2 bool bit computation in request serialize")
	}
	if !strings.Contains(content, "if flags2&(1<<3) != 0") {
		t.Fatalf("expected flags2 optional gate in request serialize")
	}
	if !strings.Contains(content, "closeFriend = flags2&(1<<2) != 0") && !strings.Contains(content, "r.CloseFriend = flags2&(1<<2) != 0") {
		t.Fatalf("expected flags2 bool assignment in request deserialize")
	}
}
