package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
)

const defaultLayer = 217

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}

	sub := os.Args[1]
	args := os.Args[2:]

	switch sub {
	case "ping-config":
		runPingConfig(args)
	case "invoke":
		runInvoke(args)
	case "auth-sendcode":
		runAuthSendCode(args)
	case "auth-signin":
		runAuthSignIn(args)
	case "-h", "--help", "help":
		usage()
	default:
		fmt.Fprintf(os.Stderr, "unknown subcommand: %s\n", sub)
		usage()
		os.Exit(2)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "tlrpc-client: MTProto dev harness")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Usage:")
	fmt.Fprintln(os.Stderr, "  tlrpc-client ping-config [flags]")
	fmt.Fprintln(os.Stderr, "  tlrpc-client invoke --method <name> [flags]")
	fmt.Fprintln(os.Stderr, "  tlrpc-client auth-sendcode --phone <phone> --api-id <id> --api-hash <hash> [flags]")
	fmt.Fprintln(os.Stderr, "  tlrpc-client auth-signin --phone <phone> --code <code> --code-hash <hash> [flags]")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Transport flags:")
	fmt.Fprintln(os.Stderr, "  --tcp :9000 --codec abridged|intermediate|padded|full")
	fmt.Fprintln(os.Stderr, "  --ws ws://localhost:9001/")
	fmt.Fprintln(os.Stderr, "  --layer N (default 217)")
	fmt.Fprintln(os.Stderr, "  --trace")
}

type commonFlags struct {
	tcpAddr string
	wsAddr  string
	codec   string
	layer   int
	trace   bool
	apiID   int
	device  string
	system  string
	app     string
	lang    string
	langPkg string
}

func (f *commonFlags) bind(fs *flag.FlagSet) {
	fs.StringVar(&f.tcpAddr, "tcp", "", "TCP address (e.g. :9000)")
	fs.StringVar(&f.wsAddr, "ws", "", "WebSocket address (e.g. ws://localhost:9001/)")
	fs.StringVar(&f.codec, "codec", "abridged", "TCP codec: abridged|intermediate|padded|full")
	fs.IntVar(&f.layer, "layer", defaultLayer, "API layer")
	fs.BoolVar(&f.trace, "trace", false, "enable client trace logs")
	fs.IntVar(&f.apiID, "api-id", 77777, "initConnection api_id")
	fs.StringVar(&f.device, "device-model", "tlrpc-client", "initConnection device_model")
	fs.StringVar(&f.system, "system-version", "dev", "initConnection system_version")
	fs.StringVar(&f.app, "app-version", "0.1", "initConnection app_version")
	fs.StringVar(&f.lang, "lang", "en", "initConnection lang code")
	fs.StringVar(&f.langPkg, "lang-pack", "", "initConnection lang pack")
}

func (f *commonFlags) initParams() client.InitParams {
	return client.InitParams{
		APIID:          int32(f.apiID),
		DeviceModel:    f.device,
		SystemVersion:  f.system,
		AppVersion:     f.app,
		SystemLangCode: f.lang,
		LangPack:       f.langPkg,
		LangCode:       f.lang,
	}
}

func dialClient(f commonFlags) (*client.Client, error) {
	if f.tcpAddr == "" && f.wsAddr == "" {
		return nil, errors.New("missing --tcp or --ws")
	}
	if f.tcpAddr != "" && f.wsAddr != "" {
		return nil, errors.New("choose one transport: --tcp or --ws")
	}

	serverKey, err := compatkeys.ServerKey()
	if err != nil {
		return nil, fmt.Errorf("load compat server key: %w", err)
	}

	constructors := gen.GetStaticConstructors()
	constructors[authSentCodeID] = func() tlrpc.TLObject { return &authSentCodeLite{} }
	constructors[authAuthorizationID] = func() tlrpc.TLObject { return &authAuthorizationLite{} }

	opts := []client.Option{
		client.WithServerKey(serverKey),
		client.WithConstructors(constructors),
	}
	if f.trace {
		opts = append(opts, client.WithTrace(traceToStderr))
	}

	if f.wsAddr != "" {
		return client.DialWS(f.wsAddr, opts...)
	}

	codec, err := parseCodec(f.codec)
	if err != nil {
		return nil, err
	}
	return client.DialTCP(f.tcpAddr, codec, opts...)
}

func parseCodec(value string) (client.Codec, error) {
	switch strings.ToLower(value) {
	case "abridged":
		return client.CodecAbridged, nil
	case "intermediate":
		return client.CodecIntermediate, nil
	case "padded", "padded-intermediate":
		return client.CodecPadded, nil
	case "full":
		return client.CodecFull, nil
	default:
		return client.CodecAbridged, fmt.Errorf("unknown codec: %s", value)
	}
}

func runPingConfig(args []string) {
	fs := flag.NewFlagSet("ping-config", flag.ExitOnError)
	var common commonFlags
	common.bind(fs)
	_ = fs.Parse(args)

	cli, err := dialClient(common)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Handshake(context.Background()); err != nil {
		exitErr(err)
	}

	req := &gen.HelpGetConfigRequest{}
	printReq(req)
	resp, err := cli.InvokeWrapped(context.Background(), int32(common.layer), common.initParams(), req, false)
	if err != nil {
		exitErr(err)
	}
	printResp(resp)
	printConfig(resp)
}

func runInvoke(args []string) {
	fs := flag.NewFlagSet("invoke", flag.ExitOnError)
	var common commonFlags
	var method string
	common.bind(fs)
	fs.StringVar(&method, "method", "", "RPC method name (e.g. help.getConfig)")
	_ = fs.Parse(args)

	if method == "" {
		exitErr(errors.New("--method is required"))
	}

	cli, err := dialClient(common)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Handshake(context.Background()); err != nil {
		exitErr(err)
	}

	req, wrapped, err := requestForMethod(method)
	if err != nil {
		exitErr(err)
	}
	printReq(req)

	var resp tlrpc.TLObject
	if wrapped {
		resp, err = cli.InvokeWrapped(context.Background(), int32(common.layer), common.initParams(), req, false)
	} else {
		resp, err = cli.Invoke(context.Background(), req)
	}
	if err != nil {
		exitErr(err)
	}
	printResp(resp)
}

func runAuthSendCode(args []string) {
	fs := flag.NewFlagSet("auth-sendcode", flag.ExitOnError)
	var common commonFlags
	var phone string
	var apiHash string
	common.bind(fs)
	fs.StringVar(&phone, "phone", "", "phone number")
	fs.StringVar(&apiHash, "api-hash", "", "api_hash")
	_ = fs.Parse(args)

	if phone == "" || apiHash == "" {
		exitErr(errors.New("--phone and --api-hash are required"))
	}

	cli, err := dialClient(common)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Handshake(context.Background()); err != nil {
		exitErr(err)
	}

	req := &gen.AuthSendCodeRequest{
		PhoneNumber: phone,
		APIID:       int32(common.apiID),
		APIHash:     apiHash,
		Settings:    gen.CodeSettings{},
	}
	printReq(req)
	resp, err := cli.InvokeWrapped(context.Background(), int32(common.layer), common.initParams(), req, false)
	if err != nil {
		exitErr(err)
	}
	printResp(resp)
	printAuthSentCode(resp)
}

func runAuthSignIn(args []string) {
	fs := flag.NewFlagSet("auth-signin", flag.ExitOnError)
	var common commonFlags
	var phone string
	var code string
	var codeHash string
	common.bind(fs)
	fs.StringVar(&phone, "phone", "", "phone number")
	fs.StringVar(&code, "code", "", "phone code")
	fs.StringVar(&codeHash, "code-hash", "", "phone_code_hash")
	_ = fs.Parse(args)

	if phone == "" || code == "" || codeHash == "" {
		exitErr(errors.New("--phone, --code, and --code-hash are required"))
	}

	cli, err := dialClient(common)
	if err != nil {
		exitErr(err)
	}
	defer func() { _ = cli.Close() }()

	if _, err := cli.Handshake(context.Background()); err != nil {
		exitErr(err)
	}

	req := &gen.AuthSignInRequest{
		PhoneNumber:   phone,
		PhoneCodeHash: codeHash,
		PhoneCode:     &code,
	}
	printReq(req)
	resp, err := cli.InvokeWrapped(context.Background(), int32(common.layer), common.initParams(), req, false)
	if err != nil {
		exitErr(err)
	}
	printResp(resp)
	printAuthUser(resp)
}

func requestForMethod(method string) (tlrpc.TLObject, bool, error) {
	switch method {
	case "help.getConfig":
		return &gen.HelpGetConfigRequest{}, true, nil
	default:
		return nil, false, fmt.Errorf("unsupported method: %s", method)
	}
}

func printReq(req tlrpc.TLObject) {
	fmt.Printf("request: %s (0x%08x)\n", tlName(req), req.ConstructorID())
}

func printResp(resp tlrpc.TLObject) {
	fmt.Printf("response: %s (0x%08x)\n", tlName(resp), resp.ConstructorID())
}

func printConfig(resp tlrpc.TLObject) {
	cfg, ok := resp.(*gen.Config)
	if !ok {
		return
	}
	fmt.Printf("config: dc=%d chat_max=%d msg_max=%d dc_txt_domain=%s\n", cfg.ThisDc, cfg.ChatSizeMax, cfg.MessageLengthMax, cfg.DcTxtDomainName)
}

func printAuthSentCode(resp tlrpc.TLObject) {
	sent, ok := resp.(*gen.AuthSentCode)
	if !ok {
		return
	}
	fmt.Printf("auth.sendCode: phone_code_hash=%s\n", sent.PhoneCodeHash)
}

func printAuthUser(resp tlrpc.TLObject) {
	auth, ok := resp.(*gen.AuthAuthorization)
	if !ok {
		return
	}
	switch u := auth.User.(type) {
	case *gen.User:
		fmt.Printf("auth.signIn: user_id=%d\n", u.ID)
	case *gen.UserEmpty:
		fmt.Printf("auth.signIn: user_id=%d (empty)\n", u.ID)
	}
}

func tlName(obj tlrpc.TLObject) string {
	if obj == nil {
		return "<nil>"
	}
	if named, ok := obj.(interface{ TLName() string }); ok {
		return named.TLName()
	}
	if named, ok := obj.(interface{ Method() string }); ok {
		if name := named.Method(); name != "" {
			return name
		}
	}
	return fmt.Sprintf("constructor_0x%08x", obj.ConstructorID())
}

func traceToStderr(ev client.TraceEvent) {
	line := fmt.Sprintf("trace %s method=%s tl=%s ctor=0x%08x", ev.Direction, ev.Method, ev.TLName, ev.Constructor)
	if len(ev.WrapperStack) > 0 {
		line += " wrappers=" + strings.Join(ev.WrapperStack, ",")
	}
	fmt.Fprintln(os.Stderr, line)
}

func exitErr(err error) {
	if err == nil {
		return
	}
	if rpcErr, ok := tlrpc.IsRPCError(err); ok {
		fmt.Fprintf(os.Stderr, "rpc_error: code=%d message=%s\n", rpcErr.ErrorCode, rpcErr.ErrorMessage)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "error: %v\n", err)
	os.Exit(1)
}

const authSentCodeID uint32 = 0x5e002502
const authAuthorizationID uint32 = 0x2ea2c0d4

type authSentCodeLite struct {
	gen.AuthSentCode
}

func (a *authSentCodeLite) DeserializeTL(r io.Reader) error {
	a.Type_ = &gen.AuthSentCodeTypeApp{}
	return a.AuthSentCode.DeserializeTL(r)
}

type authAuthorizationLite struct {
	gen.AuthAuthorization
}

func (a *authAuthorizationLite) DeserializeTL(r io.Reader) error {
	a.User = &gen.UserEmpty{}
	return a.AuthAuthorization.DeserializeTL(r)
}
