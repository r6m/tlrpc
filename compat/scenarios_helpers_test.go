package compat

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/compat/client"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/internal/compatkeys"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

type scenarioServer struct {
	srv      *tlrpc.Server
	tcpLis   transport.Listener
	wsLis    transport.Listener
	tcpAddr  string
	wsURL    string
	authKeys crypto.AuthKeyManager
	store    session.Store
	updates  *updateStore
	unbound  chan tlrpc.Binding
}

type scenarioObserver struct {
	unbound chan<- tlrpc.Binding
}

func (o scenarioObserver) ObserveTLRPC(event tlrpc.Event) {
	sessionEvent, ok := event.(tlrpc.SessionEvent)
	if !ok || sessionEvent.Action != "released" {
		return
	}
	binding := tlrpc.Binding{
		ConnectionID: sessionEvent.ConnectionID,
		AuthKeyID:    sessionEvent.AuthKeyID,
		SessionID:    sessionEvent.SessionID,
	}
	select {
	case o.unbound <- binding:
	default:
	}
}

func startScenarioServer(t *testing.T) *scenarioServer {
	t.Helper()
	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatalf("load compat key: %v", err)
	}
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(compatKey)

	authKeys := crypto.NewMemoryAuthKeyManager()
	store := session.NewMemoryStore()
	updates := newUpdateStore()
	unbound := make(chan tlrpc.Binding, 16)

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(authKeys),
		tlrpc.WithSessionStore(store),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithObserver(scenarioObserver{unbound: unbound}),
	)

	gen.RegisterHelpServer(srv, &scenarioHelpService{})
	gen.RegisterAuthServer(srv, &scenarioAuthService{})
	gen.RegisterUsersServer(srv, &scenarioUsersService{})
	gen.RegisterUpdatesServer(srv, &scenarioUpdatesService{updates: updates})

	tcpLis, err := (&transport.TCPTransport{AllowObfuscation: true}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("tcp listen: %v", err)
	}
	wsLis, err := (&transport.WebSocketTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("ws listen: %v", err)
	}

	go func() {
		_ = srv.ServeTransport(tcpLis)
	}()
	go func() {
		_ = srv.ServeTransport(wsLis)
	}()

	addr := wsLis.Addr().(*net.TCPAddr)

	s := &scenarioServer{
		srv:      srv,
		tcpLis:   tcpLis,
		wsLis:    wsLis,
		tcpAddr:  tcpLis.Addr().String(),
		wsURL:    fmt.Sprintf("ws://%s:%d/", addr.IP.String(), addr.Port),
		authKeys: authKeys,
		store:    store,
		updates:  updates,
		unbound:  unbound,
	}

	t.Cleanup(func() {
		_ = srv.Stop()
		_ = tcpLis.Close()
		_ = wsLis.Close()
	})

	return s
}

func (s *scenarioServer) waitUnbound(t *testing.T, info client.SessionInfo) {
	t.Helper()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for {
		select {
		case binding := <-s.unbound:
			if binding.AuthKeyID == int64(info.AuthKeyID) && binding.SessionID == info.SessionID {
				return
			}
		case <-timer.C:
			t.Fatalf("session %d/%d was not unbound", info.AuthKeyID, info.SessionID)
		}
	}
}

func newScenarioClient(t *testing.T, tcpAddr, wsURL string, useWS bool) *client.Client {
	t.Helper()
	compatKey, err := compatkeys.ServerKey()
	if err != nil {
		t.Fatalf("load compat key: %v", err)
	}
	constructors := gen.GetStaticConstructors()
	constructors[userVectorID] = func() tlrpc.TLObject { return &userVector{} }
	constructors[authSentCodeID] = func() tlrpc.TLObject { return &authSentCodeLite{} }
	constructors[authAuthorizationID] = func() tlrpc.TLObject { return &authAuthorizationLite{} }
	constructors[updatesID] = func() tlrpc.TLObject { return &updatesLite{} }
	constructors[updatesDifferenceID] = func() tlrpc.TLObject { return &updatesDifferenceLite{} }

	opts := []client.Option{
		client.WithServerKey(compatKey),
		client.WithConstructors(constructors),
	}
	var cli *client.Client
	if useWS {
		cli, err = client.DialWS(wsURL, opts...)
	} else {
		cli, err = client.DialTCP(tcpAddr, client.CodecAbridged, opts...)
	}
	if err != nil {
		t.Fatalf("dial client: %v", err)
	}
	return cli
}

func handshakeAndLogin(t *testing.T, cli *client.Client, layer int32) int64 {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if _, err := cli.Handshake(ctx); err != nil {
		t.Fatalf("handshake: %v", err)
	}

	send := &gen.AuthSendCodeRequest{
		PhoneNumber: "+15551234567",
		APIID:       77777,
		APIHash:     "devhash",
		Settings:    gen.CodeSettings{},
	}
	if _, err := cli.InvokeWrapped(ctx, layer, defaultInitParams(), send, false); err != nil {
		t.Fatalf("auth.sendCode: %v", err)
	}

	code := "12345"
	signIn := &gen.AuthSignInRequest{
		PhoneNumber:   "+15551234567",
		PhoneCodeHash: "compat-phone-code-hash",
		PhoneCode:     &code,
	}
	resp, err := cli.InvokeWrapped(ctx, layer, defaultInitParams(), signIn, false)
	if err != nil {
		t.Fatalf("auth.signIn: %v", err)
	}
	return extractAuthUserID(t, resp)
}

func defaultInitParams() client.InitParams {
	return client.InitParams{
		APIID:          77777,
		DeviceModel:    "scenario-test",
		SystemVersion:  "test",
		AppVersion:     "1.0",
		SystemLangCode: "en",
		LangPack:       "",
		LangCode:       "en",
	}
}

// userVector is a small helper to decode vector<user> responses.
const userVectorID uint32 = 0x1cb5c415
const authSentCodeID uint32 = 0x5e002502
const authAuthorizationID uint32 = 0x2ea2c0d4
const updatesID uint32 = 0x74ae4240
const updatesDifferenceID uint32 = 0x00f49ca0

type userVector struct {
	Items []gen.UserType
}

func (v *userVector) ConstructorID() uint32 { return userVectorID }
func (v *userVector) Method() string        { return "" }
func (v *userVector) TLName() string        { return "vector" }

func (v *userVector) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, v.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteVectorHeader(w, len(v.Items)); err != nil {
		return err
	}
	for _, item := range v.Items {
		if err := item.SerializeTL(w); err != nil {
			return err
		}
	}
	return nil
}

func (v *userVector) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x", ctor)
	}
	var items []gen.UserType
	if err := mtproto.ReadVector(r, func() error {
		item, err := decodeUserType(r)
		if err != nil {
			return err
		}
		items = append(items, item)
		return nil
	}); err != nil {
		return err
	}
	v.Items = items
	return nil
}

func decodeUserType(r io.Reader) (gen.UserType, error) {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return nil, err
	}
	var obj gen.UserType
	switch ctor {
	case (&gen.UserEmpty{}).ConstructorID():
		obj = &gen.UserEmpty{}
	case (&gen.User{}).ConstructorID():
		obj = &gen.User{}
	default:
		return nil, fmt.Errorf("unknown user constructor: 0x%08x", ctor)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], ctor)
	if err := obj.DeserializeTL(io.MultiReader(bytes.NewReader(buf[:]), r)); err != nil {
		return nil, err
	}
	return obj, nil
}

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

type updatesLite struct {
	Updates []gen.UpdateType
	Users   []gen.UserType
	Chats   []gen.ChatType
	Date    int32
	Seq     int32
}

func (v *updatesLite) ConstructorID() uint32 { return updatesID }
func (v *updatesLite) Method() string        { return "" }
func (v *updatesLite) TLName() string        { return "updates" }

func (v *updatesLite) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, v.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteVectorHeader(w, len(v.Updates)); err != nil {
		return err
	}
	for _, item := range v.Updates {
		if err := item.SerializeTL(w); err != nil {
			return err
		}
	}
	if err := mtproto.WriteVectorHeader(w, len(v.Users)); err != nil {
		return err
	}
	for _, item := range v.Users {
		if err := item.SerializeTL(w); err != nil {
			return err
		}
	}
	if err := mtproto.WriteVectorHeader(w, len(v.Chats)); err != nil {
		return err
	}
	for _, item := range v.Chats {
		if err := item.SerializeTL(w); err != nil {
			return err
		}
	}
	if err := mtproto.WriteInt32(w, v.Date); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, v.Seq); err != nil {
		return err
	}
	return nil
}

func (v *updatesLite) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x", ctor)
	}
	var updates []gen.UpdateType
	if err := mtproto.ReadVector(r, func() error {
		item, err := decodeUpdateType(r)
		if err != nil {
			return err
		}
		updates = append(updates, item)
		return nil
	}); err != nil {
		return err
	}
	var users []gen.UserType
	if err := mtproto.ReadVector(r, func() error {
		item, err := decodeUserType(r)
		if err != nil {
			return err
		}
		users = append(users, item)
		return nil
	}); err != nil {
		return err
	}
	chatCount, err := readVectorCount(r)
	if err != nil {
		return err
	}
	if chatCount != 0 {
		return fmt.Errorf("unexpected chats vector length %d", chatCount)
	}
	date, err := mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	seq, err := mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	v.Updates = updates
	v.Users = users
	v.Date = date
	v.Seq = seq
	return nil
}

type updatesDifferenceLite struct {
	OtherUpdates []gen.UpdateType
	State        gen.UpdatesState
}

func (v *updatesDifferenceLite) ConstructorID() uint32 { return updatesDifferenceID }
func (v *updatesDifferenceLite) Method() string        { return "" }
func (v *updatesDifferenceLite) TLName() string        { return "updates.difference" }

func (v *updatesDifferenceLite) SerializeTL(w io.Writer) error {
	return fmt.Errorf("updatesDifferenceLite serialize not implemented")
}

func (v *updatesDifferenceLite) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != v.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %x", ctor)
	}
	msgCount, err := readVectorCount(r)
	if err != nil {
		return err
	}
	if msgCount != 0 {
		return fmt.Errorf("unexpected message vector length %d", msgCount)
	}
	encCount, err := readVectorCount(r)
	if err != nil {
		return err
	}
	if encCount != 0 {
		return fmt.Errorf("unexpected encrypted message vector length %d", encCount)
	}
	var updates []gen.UpdateType
	if err := mtproto.ReadVector(r, func() error {
		item, err := decodeUpdateType(r)
		if err != nil {
			return err
		}
		updates = append(updates, item)
		return nil
	}); err != nil {
		return err
	}
	chatCount, err := readVectorCount(r)
	if err != nil {
		return err
	}
	if chatCount != 0 {
		return fmt.Errorf("unexpected chats vector length %d", chatCount)
	}
	userCount, err := readVectorCount(r)
	if err != nil {
		return err
	}
	for i := int32(0); i < userCount; i++ {
		if _, err := decodeUserType(r); err != nil {
			return err
		}
	}
	var state gen.UpdatesState
	if err := state.DeserializeTL(r); err != nil {
		return err
	}
	v.OtherUpdates = updates
	v.State = state
	return nil
}

func decodeUpdateType(r io.Reader) (gen.UpdateType, error) {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return nil, err
	}
	var obj gen.UpdateType
	switch ctor {
	case (&gen.UpdateUserStatus{}).ConstructorID():
		obj = &gen.UpdateUserStatus{Status: &gen.UserStatusOnline{}}
	default:
		return nil, fmt.Errorf("unknown update constructor: 0x%08x", ctor)
	}
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], ctor)
	if err := obj.DeserializeTL(io.MultiReader(bytes.NewReader(buf[:]), r)); err != nil {
		return nil, err
	}
	return obj, nil
}

func readVectorCount(r io.Reader) (int32, error) {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return 0, err
	}
	if ctor != mtproto.VectorConstructorID {
		return 0, fmt.Errorf("unexpected vector constructor: 0x%08x", ctor)
	}
	count, err := mtproto.ReadInt32(r)
	if err != nil {
		return 0, err
	}
	return count, nil
}

type updateEntry struct {
	pts    int32
	update gen.UpdateType
}

type userUpdateState struct {
	pts     int32
	seq     int32
	date    int32
	updates []updateEntry
}

type updateStore struct {
	mu     sync.Mutex
	byUser map[int64]*userUpdateState
}

func newUpdateStore() *updateStore {
	return &updateStore{byUser: make(map[int64]*userUpdateState)}
}

func (s *updateStore) state(userID int64) *userUpdateState {
	state, ok := s.byUser[userID]
	if !ok {
		now := int32(time.Now().Unix())
		state = &userUpdateState{pts: 1, seq: 1, date: now}
		s.byUser[userID] = state
	}
	return state
}

func (s *updateStore) snapshot(userID int64) *gen.UpdatesState {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state(userID)
	return &gen.UpdatesState{
		Pts:         state.pts,
		Qts:         0,
		Date:        state.date,
		Seq:         state.seq,
		UnreadCount: 0,
	}
}

func (s *updateStore) publish(srv *tlrpc.Server, userID int64, update gen.UpdateType) (*gen.UpdatesState, error) {
	s.mu.Lock()
	state := s.state(userID)
	state.pts++
	state.seq++
	state.date = int32(time.Now().Unix())
	state.updates = append(state.updates, updateEntry{pts: state.pts, update: update})
	current := &gen.UpdatesState{
		Pts:         state.pts,
		Qts:         0,
		Date:        state.date,
		Seq:         state.seq,
		UnreadCount: 0,
	}
	s.mu.Unlock()

	push := &gen.Updates{
		Updates: []gen.UpdateType{update},
		Users:   []gen.UserType{&gen.UserEmpty{ID: userID}},
		Chats:   nil,
		Date:    current.Date,
		Seq:     current.Seq,
	}
	return current, srv.Publish(userID, push)
}

func (s *updateStore) difference(userID int64, req *gen.UpdatesGetDifferenceRequest) gen.UpdatesDifferenceType {
	s.mu.Lock()
	defer s.mu.Unlock()
	state := s.state(userID)
	var updates []gen.UpdateType
	for _, entry := range state.updates {
		if entry.pts > req.Pts {
			updates = append(updates, entry.update)
		}
	}
	if len(updates) == 0 {
		return &gen.UpdatesDifferenceEmpty{Date: state.date, Seq: state.seq}
	}
	return &gen.UpdatesDifference{
		OtherUpdates: updates,
		Users:        []gen.UserType{&gen.UserEmpty{ID: userID}},
		Chats:        nil,
		State: gen.UpdatesState{
			Pts:         state.pts,
			Qts:         0,
			Date:        state.date,
			Seq:         state.seq,
			UnreadCount: 0,
		},
	}
}

type scenarioHelpService struct{ gen.UnimplementedHelpServer }

func (s *scenarioHelpService) GetConfig(context.Context, *gen.HelpGetConfigRequest) (*gen.Config, error) {
	now := int32(time.Now().Unix())
	return &gen.Config{
		Date:             now,
		Expires:          now + 3600,
		ThisDc:           1,
		DcTxtDomainName:  "localhost",
		ChatSizeMax:      200,
		MessageLengthMax: 4096,
	}, nil
}

type scenarioAuthService struct{ gen.UnimplementedAuthServer }

func (*scenarioAuthService) SendCode(context.Context, *gen.AuthSendCodeRequest) (gen.AuthSentCodeType, error) {
	timeout := int32(60)
	return &gen.AuthSentCode{
		Type_:         &gen.AuthSentCodeTypeApp{Length: 5},
		PhoneCodeHash: "compat-phone-code-hash",
		Timeout:       &timeout,
	}, nil
}

func (*scenarioAuthService) SignIn(ctx context.Context, _ *gen.AuthSignInRequest) (gen.AuthAuthorizationType, error) {
	const userID = int64(1)
	if err := tlrpc.BindSessionUser(ctx, userID); err != nil {
		return nil, err
	}
	return &gen.AuthAuthorization{User: &gen.UserEmpty{ID: userID}}, nil
}

type scenarioUsersService struct{ gen.UnimplementedUsersServer }

func (*scenarioUsersService) GetUsers(ctx context.Context, _ *gen.UsersGetUsersRequest) ([]gen.UserType, error) {
	userID := tlrpc.UserIDFromContext(ctx)
	if userID == 0 {
		userID = 1
	}
	return []gen.UserType{&gen.UserEmpty{ID: userID}}, nil
}

type scenarioUpdatesService struct {
	gen.UnimplementedUpdatesServer
	updates *updateStore
}

func (s *scenarioUpdatesService) GetState(ctx context.Context, _ *gen.UpdatesGetStateRequest) (*gen.UpdatesState, error) {
	userID := tlrpc.UserIDFromContext(ctx)
	if userID == 0 {
		userID = 1
	}
	return s.updates.snapshot(userID), nil
}

func (s *scenarioUpdatesService) GetDifference(ctx context.Context, req *gen.UpdatesGetDifferenceRequest) (gen.UpdatesDifferenceType, error) {
	userID := tlrpc.UserIDFromContext(ctx)
	if userID == 0 {
		userID = 1
	}
	return s.updates.difference(userID, req), nil
}

func extractAuthUserID(t *testing.T, resp tlrpc.TLObject) int64 {
	t.Helper()
	auth, ok := resp.(*gen.AuthAuthorization)
	if !ok {
		if lite, ok := resp.(*authAuthorizationLite); ok {
			auth = &lite.AuthAuthorization
		} else {
			t.Fatalf("expected AuthAuthorization, got %T", resp)
		}
	}
	switch u := auth.User.(type) {
	case *gen.User:
		return u.ID
	case *gen.UserEmpty:
		return u.ID
	default:
		t.Fatalf("unexpected user type %T", auth.User)
	}
	return 0
}
