package client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/transport"
	"github.com/r6m/tlrpc/types"
)

// Codec selects the MTProto transport framing for TCP.
type Codec int

const (
	CodecAbridged Codec = iota
	CodecIntermediate
	CodecPadded
	CodecFull
)

// Client speaks MTProto v2 over a selected transport.
type Client struct {
	conn      transport.Conn
	authKeyID crypto.KeyID
	authKey   crypto.AuthKey
	serverKey *crypto.ServerKey

	serverSalt int64
	sessionID  int64
	seqNo      int32

	constructors map[uint32]func() tlrpc.TLObject
	trace        TraceFunc
	msgID        func() int64
	pendingMu    sync.Mutex
	pending      []tlrpc.TLObject
}

func (c *Client) queuePush(object tlrpc.TLObject) {
	if object == nil {
		return
	}
	c.pendingMu.Lock()
	c.pending = append(c.pending, object)
	c.pendingMu.Unlock()
}

func (c *Client) popPush() tlrpc.TLObject {
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if len(c.pending) == 0 {
		return nil
	}
	object := c.pending[0]
	copy(c.pending, c.pending[1:])
	c.pending = c.pending[:len(c.pending)-1]
	return object
}

type Option func(*Client)

// WithServerKey configures the server key used for the handshake.
func WithServerKey(key *crypto.ServerKey) Option {
	return func(c *Client) { c.serverKey = key }
}

// WithConstructors merges constructor registrations for response decoding.
func WithConstructors(constructors map[uint32]func() tlrpc.TLObject) Option {
	return func(c *Client) {
		for id, ctor := range constructors {
			c.constructors[id] = ctor
		}
	}
}

// WithTrace enables trace callbacks for inbound/outbound messages.
func WithTrace(fn TraceFunc) Option {
	return func(c *Client) { c.trace = fn }
}

// WithMsgIDGenerator overrides the default msg_id generator.
func WithMsgIDGenerator(fn func() int64) Option {
	return func(c *Client) { c.msgID = fn }
}

// New creates a client for an existing transport connection.
func New(conn transport.Conn, opts ...Option) *Client {
	c := &Client{
		conn:         conn,
		constructors: make(map[uint32]func() tlrpc.TLObject),
		msgID:        nextMsgID,
	}
	c.registerDefaultConstructors()
	for _, opt := range opts {
		opt(c)
	}
	return c
}

func (c *Client) registerDefaultConstructors() {
	for id, ctor := range types.GetBaseConstructors() {
		baseCtor := ctor
		c.constructors[id] = func() tlrpc.TLObject { return baseCtor() }
	}
	c.constructors[mtprototl.MsgContainerID] = func() tlrpc.TLObject { return &mtprototl.MsgContainer{} }
	c.constructors[mtprototl.MsgsAckID] = func() tlrpc.TLObject { return &mtprototl.MsgsAck{} }
	c.constructors[mtprototl.GzipPackedID] = func() tlrpc.TLObject { return &mtprototl.GzipPacked{} }
	c.constructors[mtprototl.RPCResultID] = func() tlrpc.TLObject { return &mtprototl.RPCResult{} }
	c.constructors[mtprototl.RPCErrorID] = func() tlrpc.TLObject { return &mtprototl.RPCError{} }
	c.constructors[mtprototl.NewSessionCreatedID] = func() tlrpc.TLObject { return &mtprototl.NewSessionCreated{} }
	c.constructors[mtprototl.GetFutureSaltsID] = func() tlrpc.TLObject { return &mtprototl.GetFutureSaltsRequest{} }
	c.constructors[mtprototl.FutureSaltsID] = func() tlrpc.TLObject { return &mtprototl.FutureSalts{} }
	c.constructors[mtprototl.BadMsgNotificationID] = func() tlrpc.TLObject { return &mtprototl.BadMsgNotification{} }
	c.constructors[mtprototl.BadServerSaltID] = func() tlrpc.TLObject { return &mtprototl.BadServerSalt{} }
	c.constructors[mtprototl.InvokeWithLayerID] = func() tlrpc.TLObject { return &mtprototl.InvokeWithLayer{} }
	c.constructors[mtprototl.InitConnectionID] = func() tlrpc.TLObject { return &mtprototl.InitConnection{} }
	c.constructors[mtprototl.InvokeWithoutUpdatesID] = func() tlrpc.TLObject { return &mtprototl.InvokeWithoutUpdates{} }
}

// DialTCP connects over TCP using the chosen MTProto codec.
func DialTCP(addr string, codec Codec, opts ...Option) (*Client, error) {
	proto, err := codecToProtocol(codec)
	if err != nil {
		return nil, err
	}
	conn, err := (&transport.TCPTransport{Protocol: proto}).Dial(addr)
	if err != nil {
		return nil, err
	}
	return New(conn, opts...), nil
}

// DialWS connects over WebSocket with obfuscated2 + padded intermediate.
func DialWS(addr string, opts ...Option) (*Client, error) {
	conn, err := (&transport.WebSocketTransport{Protocol: transport.ProtocolPaddedIntermediate}).Dial(addr)
	if err != nil {
		return nil, err
	}
	return New(conn, opts...), nil
}

// Close closes the underlying connection.
func (c *Client) Close() error {
	if c.conn == nil {
		return nil
	}
	return c.conn.Close()
}

// Conn exposes the underlying transport connection (for low-level tests).
func (c *Client) Conn() transport.Conn { return c.conn }

// Handshake performs the MTProto v2 handshake and initializes auth/session state.
func (c *Client) Handshake(ctx context.Context) (*SessionInfo, error) {
	if c.serverKey == nil {
		return nil, errors.New("compat client: server key is required")
	}
	return c.performHandshake(ctx)
}

// Invoke sends a single TL request and returns the decoded response object.
func (c *Client) Invoke(ctx context.Context, req tlrpc.TLObject) (tlrpc.TLObject, error) {
	payload, err := encodeTL(req)
	if err != nil {
		return nil, err
	}
	return c.invokeRaw(ctx, payload)
}

// InvokeWrapped wraps a request with initConnection and invokeWithLayer.
// If withoutUpdates is true, invokeWithoutUpdates is applied inside initConnection.
func (c *Client) InvokeWrapped(ctx context.Context, layer int32, init InitParams, inner tlrpc.TLObject, withoutUpdates bool) (tlrpc.TLObject, error) {
	innerRaw, err := encodeTL(inner)
	if err != nil {
		return nil, err
	}
	query := innerRaw
	if withoutUpdates {
		without := &mtprototl.InvokeWithoutUpdates{QueryRaw: query}
		query, err = encodeTL(without)
		if err != nil {
			return nil, err
		}
	}
	initReq := &mtprototl.InitConnection{
		Flags:          init.Flags,
		APIID:          init.APIID,
		DeviceModel:    init.DeviceModel,
		SystemVersion:  init.SystemVersion,
		AppVersion:     init.AppVersion,
		SystemLangCode: init.SystemLangCode,
		LangPack:       init.LangPack,
		LangCode:       init.LangCode,
		QueryRaw:       query,
	}
	initRaw, err := encodeTL(initReq)
	if err != nil {
		return nil, err
	}
	wrapped := &mtprototl.InvokeWithLayer{Layer: layer, QueryRaw: initRaw}
	wrappedRaw, err := encodeTL(wrapped)
	if err != nil {
		return nil, err
	}
	return c.invokeRaw(ctx, wrappedRaw)
}

func (c *Client) invokeRaw(ctx context.Context, payload []byte) (tlrpc.TLObject, error) {
	if c.authKeyID == 0 {
		return nil, errors.New("compat client: handshake required before invoke")
	}
	seq := c.nextSeqNo()
	for attempt := 0; attempt < 2; attempt++ {
		msgID := c.msgID()
		inner := &mtproto.InnerData{
			Salt:      c.serverSalt,
			SessionID: c.sessionID,
			MsgID:     msgID,
			SeqNo:     seq,
			Data:      payload,
		}
		enc, err := inner.EncryptFromClient(c.authKey, c.authKeyID)
		if err != nil {
			return nil, err
		}
		if err := c.conn.WriteMessage(serializeEncrypted(enc)); err != nil {
			return nil, err
		}
		c.traceOutbound(payload, msgID, seq)

		resp, err := c.readResponse(ctx, msgID)
		if err == nil {
			return resp, nil
		}
		var badSalt *BadServerSaltError
		if errors.As(err, &badSalt) {
			c.serverSalt = badSalt.NewSalt
			continue
		}
		return nil, err
	}
	return nil, errors.New("compat client: failed after bad_server_salt")
}

func (c *Client) nextSeqNo() int32 {
	seq := c.seqNo*2 + 1
	c.seqNo++
	return seq
}

func codecToProtocol(codec Codec) (transport.Protocol, error) {
	switch codec {
	case CodecAbridged:
		return transport.ProtocolAbridged, nil
	case CodecIntermediate:
		return transport.ProtocolIntermediate, nil
	case CodecPadded:
		return transport.ProtocolPaddedIntermediate, nil
	case CodecFull:
		return transport.ProtocolFull, nil
	default:
		return transport.ProtocolUnknown, fmt.Errorf("unknown codec: %d", codec)
	}
}

// SessionInfo exposes the handshake-derived session state.
type SessionInfo struct {
	AuthKeyID  crypto.KeyID
	AuthKey    crypto.AuthKey
	ServerSalt int64
	SessionID  int64
}

// InitParams describes initConnection fields.
type InitParams struct {
	Flags          uint32
	APIID          int32
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	SystemLangCode string
	LangPack       string
	LangCode       string
}

func nextMsgID() int64 {
	now := time.Now().UnixNano()
	sec := now / int64(time.Second)
	nsec := now % int64(time.Second)
	msgID := (sec << 32) | (nsec << 2)
	return msgID &^ 3
}
