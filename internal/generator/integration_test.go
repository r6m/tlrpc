package generator

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r6m/tlrpc/internal/naming"
	"github.com/r6m/tlrpc/internal/parser"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration_SimpleSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "simple.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.NoError(t, err)

	// Check that we parsed the expected elements
	assert.Len(t, schema.Types, 4) // Bool, User, auth.SentCode, auth.Authorization
	assert.Len(t, schema.Functions, 2)
	assert.True(t, len(schema.Constructors) > 0)
}

func TestIntegration_SchemaWithErrors(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "with_errors.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.Error(t, err)

	errors := v.Errors()
	assert.True(t, len(errors) > 0, "Should have validation errors")
}

func TestIntegration_FlagsSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "flags.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.NoError(t, err)
}

func TestIntegration_CircularSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schemas", "circular.tl"))
	require.NoError(t, err)

	p := parser.NewParser(string(data))
	schema, err := p.Parse()
	require.NoError(t, err)

	v := parser.NewValidator(schema)
	err = v.Validate()
	assert.NoError(t, err)
}

func TestIntegration_RealSchema(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "testdata", "schema-217.tl"))
	require.NoError(t, err)

	parser := parser.NewParser(string(data))
	schema, err := parser.Parse()
	require.NoError(t, err)

	assert.True(t, len(schema.Types) > 0)
	assert.True(t, len(schema.Constructors) > 0)
}

func TestIntegration_FrameworkFirstCustomSchema(t *testing.T) {
	data := readTestSchema(t, "framework_acceptance.tl")
	p := parser.NewParser(string(data))
	schema, err := p.ParseWithLayer(7)
	require.NoError(t, err)
	require.NoError(t, parser.NewValidator(schema).Validate())
	require.Equal(t, 7, schema.Layer)
	require.Len(t, schema.Functions, 4)

	moduleDir := t.TempDir()
	generatedDir := filepath.Join(moduleDir, "customapi")
	require.NoError(t, generateFrameworkAcceptancePackage(generatedDir, schema))
	servicesSource, err := os.ReadFile(filepath.Join(generatedDir, "services.go"))
	require.NoError(t, err)
	require.Contains(t, string(servicesSource), "// Layer: 7")
	require.NotContains(t, strings.ToLower(string(servicesSource)), "tgserver")
	require.NotContains(t, string(servicesSource), "// Layer: 228")

	repoRoot, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	goMod := fmt.Sprintf(`module example.com/tlrpc-framework-acceptance

go 1.25

require github.com/r6m/tlrpc v0.0.0

replace github.com/r6m/tlrpc => %s
`, filepath.ToSlash(repoRoot))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "go.mod"), []byte(goMod), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(moduleDir, "framework_test.go"), []byte(frameworkAcceptanceTest), 0o600))

	cmd := exec.Command("go", "test", "-count=1", "./...")
	cmd.Dir = moduleDir
	cmd.Env = append(os.Environ(), "GOWORK=off", "GOFLAGS=-mod=mod")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "custom-schema consumer failed:\n%s", output)
}

func generateFrameworkAcceptancePackage(outDir string, schema *parser.Schema) error {
	writer := NewFileWriter(outDir, "customapi", "framework_acceptance.tl", schema.Layer)
	namer := naming.NewNamer()
	typesOut := writer.NewFile("types.go")
	interfacesOut := writer.NewFile("interfaces.go")

	for i := range schema.Types {
		if err := NewTypeGenerator(namer, typesOut, schema).GenerateType(&schema.Types[i]); err != nil {
			return err
		}
		if err := NewTypeGenerator(namer, interfacesOut, schema).GenerateInterface(&schema.Types[i]); err != nil {
			return err
		}
	}

	services := NewServiceGenerator(namer, schema, writer.NewFile("services.go"))
	if err := services.GenerateService(schema.Functions); err != nil {
		return err
	}
	registration := NewServiceGenerator(namer, schema, writer.NewFile("register.go"))
	if err := registration.GenerateRegistration(schema.Functions); err != nil {
		return err
	}
	requests := NewServiceGenerator(namer, schema, writer.NewFile("requests.go"))
	if err := requests.GenerateRequests(schema.Functions); err != nil {
		return err
	}
	if err := NewCodecGenerator(namer, writer.NewFile("codec.go")).Generate(schema); err != nil {
		return err
	}
	constants := writer.NewFile("constants.go")
	if err := GenerateSchemaMetadata(constants, schema.Layer); err != nil {
		return err
	}
	if err := GenerateConstructorConstants(namer, constants, schema.Constructors); err != nil {
		return err
	}
	if err := GenerateBaseAliases(writer.NewFile("base_aliases.go")); err != nil {
		return err
	}
	return writer.WriteAll()
}

const frameworkAcceptanceTest = `package acceptance_test

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"example.com/tlrpc-framework-acceptance/customapi"
	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

type memoryAddr string

func (a memoryAddr) Network() string { return "memory" }
func (a memoryAddr) String() string  { return string(a) }

type memoryConn struct {
	in     <-chan []byte
	out    chan<- []byte
	ctx    context.Context
	cancel context.CancelFunc
}

func newMemoryPair() (*memoryConn, *memoryConn) {
	ctx, cancel := context.WithCancel(context.Background())
	clientToServer := make(chan []byte, 16)
	serverToClient := make(chan []byte, 16)
	return &memoryConn{in: serverToClient, out: clientToServer, ctx: ctx, cancel: cancel},
		&memoryConn{in: clientToServer, out: serverToClient, ctx: ctx, cancel: cancel}
}

func (c *memoryConn) ReadMessage(_ int) ([]byte, error) {
	select {
	case payload := <-c.in:
		return append([]byte(nil), payload...), nil
	case <-c.ctx.Done():
		return nil, io.EOF
	}
}

func (c *memoryConn) WriteMessage(payload []byte) error {
	select {
	case c.out <- append([]byte(nil), payload...):
		return nil
	case <-c.ctx.Done():
		return io.ErrClosedPipe
	}
}

func (c *memoryConn) Close() error                       { c.cancel(); return nil }
func (c *memoryConn) LocalAddr() net.Addr                { return memoryAddr("local") }
func (c *memoryConn) RemoteAddr() net.Addr               { return memoryAddr("remote") }
func (c *memoryConn) SetDeadline(time.Time) error        { return nil }
func (c *memoryConn) Context() context.Context           { return c.ctx }

type singleListener struct {
	conn transport.Conn
	done chan struct{}
	once sync.Once
}

func newSingleListener(conn transport.Conn) *singleListener {
	return &singleListener{conn: conn, done: make(chan struct{})}
}

func (l *singleListener) Accept() (transport.Conn, error) {
	if l.conn != nil {
		conn := l.conn
		l.conn = nil
		return conn, nil
	}
	<-l.done
	return nil, io.EOF
}

func (l *singleListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *singleListener) Addr() net.Addr { return memoryAddr("listener") }

type catalogService struct {
	customapi.UnimplementedCatalogServer
}

func (catalogService) Resolve(_ context.Context, req *customapi.CatalogResolveRequest) (customapi.AssetType, error) {
	if req.ID != 42 {
		return nil, tlrpc.NewBadRequestError("ASSET_ID_INVALID")
	}
	title := "Framework handbook"
	return &customapi.AssetLink{ID: req.ID, URL: "https://example.test/framework", Title: &title, Tags: []string{"go", "rpc"}}, nil
}

func (catalogService) Search(_ context.Context, req *customapi.CatalogSearchRequest) (*customapi.CatalogPage, error) {
	if req.Query != "frameworks" || req.Cursor == nil || *req.Cursor != "page-1" || req.Limit != 2 {
		return nil, tlrpc.NewBadRequestError("SEARCH_INPUT_INVALID")
	}
	summary := "Generated from a custom TL schema"
	title := "Public API"
	next := "page-2"
	return &customapi.CatalogPage{
		Items: []customapi.AssetType{
			&customapi.AssetDocument{ID: 1, Name: "guide", Summary: &summary, Tags: []string{"tl", "generated"}},
			&customapi.AssetLink{ID: 2, URL: "https://example.test/api", Title: &title, Tags: []string{"public"}},
		},
		NextCursor: &next,
	}, nil
}

type workflowService struct {
	customapi.UnimplementedWorkflowServer
}

func (workflowService) Submit(_ context.Context, req *customapi.WorkflowSubmitRequest) (customapi.JobStatusType, error) {
	if _, ok := req.Asset.(*customapi.AssetDocument); !ok || len(req.Labels) != 2 || req.Note == nil || *req.Note != "ship it" {
		return nil, tlrpc.NewBadRequestError("JOB_INPUT_INVALID")
	}
	return &customapi.JobQueued{JobID: 9001, Asset: req.Asset, Labels: req.Labels}, nil
}

func (workflowService) Reject(context.Context, *customapi.WorkflowRejectRequest) (customapi.JobStatusType, error) {
	return nil, tlrpc.NewBadRequestError("REJECTED_BY_POLICY")
}

func TestCustomSchemaServicesDispatchOverPublicAPIs(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryStore()
	var authKey crypto.AuthKey
	for i := range authKey {
		authKey[i] = byte(i)
	}
	keyID := authKey.ID()
	if err := authKeys.Put(keyID, authKey); err != nil {
		t.Fatal(err)
	}
	const sessionID = int64(0x0101010102020202)
	const serverSalt = int64(0x1122334455667788)
	key := session.SessionKey{AuthKeyID: keyID, SessionID: sessionID}
	if _, _, err := sessions.LoadOrCreate(context.Background(), key, session.Snapshot{
		AuthKeyID: keyID,
		SessionID: sessionID,
		ServerSalt: serverSalt,
		Layer:      customapi.SchemaLayer,
	}); err != nil {
		t.Fatal(err)
	}

	srv := tlrpc.NewServer(tlrpc.WithAuthKeyManager(authKeys), tlrpc.WithSessionStore(sessions))
	if customapi.Catalog_ServiceDesc.SchemaLayer != customapi.SchemaLayer || len(customapi.Catalog_ServiceDesc.Methods) != 2 {
		t.Fatalf("catalog generated service descriptor: %#v", customapi.Catalog_ServiceDesc)
	}
	if customapi.Workflow_ServiceDesc.SchemaLayer != customapi.SchemaLayer || len(customapi.Workflow_ServiceDesc.Methods) != 2 {
		t.Fatalf("workflow generated service descriptor: %#v", customapi.Workflow_ServiceDesc)
	}
	customapi.RegisterCatalogServer(srv, catalogService{})
	customapi.RegisterWorkflowServer(srv, workflowService{})
	clientConn, serverConn := newMemoryPair()
	lis := newSingleListener(serverConn)
	go func() { _ = srv.ServeTransport(lis) }()
	t.Cleanup(func() {
		_ = srv.Stop()
		_ = lis.Close()
	})

	cli := client.New(clientConn, client.WithConstructors(customapi.GetStaticConstructors()))
	cli.SetSession(keyID, authKey, serverSalt, sessionID)
	t.Cleanup(func() { _ = cli.Close() })
	ctx := context.Background()

	cursor := "page-1"
	searchObj, err := cli.Invoke(ctx, &customapi.CatalogSearchRequest{Query: "frameworks", Cursor: &cursor, Limit: 2})
	if err != nil {
		t.Fatalf("catalog.search: %v", err)
	}
	page, ok := searchObj.(*customapi.CatalogPage)
	if !ok || len(page.Items) != 2 || page.NextCursor == nil || *page.NextCursor != "page-2" {
		t.Fatalf("catalog.search response: %#v", searchObj)
	}
	if _, ok := page.Items[0].(*customapi.AssetDocument); !ok {
		t.Fatalf("first union member: %T", page.Items[0])
	}
	if _, ok := page.Items[1].(*customapi.AssetLink); !ok {
		t.Fatalf("second union member: %T", page.Items[1])
	}

	resolvedObj, err := cli.Invoke(ctx, &customapi.CatalogResolveRequest{ID: 42})
	if err != nil {
		t.Fatalf("catalog.resolve: %v", err)
	}
	resolved, ok := resolvedObj.(*customapi.AssetLink)
	if !ok || resolved.Title == nil || *resolved.Title != "Framework handbook" || len(resolved.Tags) != 2 {
		t.Fatalf("catalog.resolve response: %#v", resolvedObj)
	}

	note := "ship it"
	queuedObj, err := cli.Invoke(ctx, &customapi.WorkflowSubmitRequest{
		Asset: &customapi.AssetDocument{ID: 7, Name: "proposal", Tags: []string{"design"}},
		Labels: []string{"accepted", "framework"},
		Note: &note,
	})
	if err != nil {
		t.Fatalf("workflow.submit: %v", err)
	}
	queued, ok := queuedObj.(*customapi.JobQueued)
	if !ok || queued.JobID != 9001 || len(queued.Labels) != 2 {
		t.Fatalf("workflow.submit response: %#v", queuedObj)
	}

	_, err = cli.Invoke(ctx, &customapi.WorkflowRejectRequest{Reason: "policy"})
	var rpcErr *tlrpc.RPCError
	if !errors.As(err, &rpcErr) || rpcErr.ErrorCode != 400 || rpcErr.ErrorMessage != "REJECTED_BY_POLICY" {
		t.Fatalf("workflow.reject error: %v", err)
	}
}
`
