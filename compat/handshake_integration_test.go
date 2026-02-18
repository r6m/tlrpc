package compat

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/examples/gen"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
	"github.com/r6m/tlrpc/transport"
)

const (
	reqPQMultiID      uint32 = 0xbe7e8ef1
	resPQID           uint32 = 0x05162463
	reqDHParamsID     uint32 = 0xd712e4be
	pQInnerDataID     uint32 = 0x83c95aec
	serverDHParamsOK  uint32 = 0xd0e8075c
	serverDHInnerData uint32 = 0xb5890dba
	setClientDHParams uint32 = 0xf5045f1f
	clientDHInnerData uint32 = 0x6643b654
	dhGenOK           uint32 = 0x3bcbf734
)

// For the server's hard-coded pq value 0x17ED48941A08F981.
var (
	fixedP = []byte{0x49, 0x4c, 0x55, 0x3b}
	fixedQ = []byte{0x53, 0x91, 0x10, 0x73}
)

type recordingAuthKeyManager struct {
	base crypto.AuthKeyManager
	mu   sync.RWMutex
	last struct {
		id  crypto.KeyID
		key crypto.AuthKey
		ok  bool
	}
}

func newRecordingAuthKeyManager(base crypto.AuthKeyManager) *recordingAuthKeyManager {
	return &recordingAuthKeyManager{base: base}
}

func (m *recordingAuthKeyManager) Get(keyID crypto.KeyID) (crypto.AuthKey, error) {
	return m.base.Get(keyID)
}

func (m *recordingAuthKeyManager) Put(keyID crypto.KeyID, key crypto.AuthKey) error {
	if err := m.base.Put(keyID, key); err != nil {
		return err
	}
	m.mu.Lock()
	m.last.id = keyID
	m.last.key = key
	m.last.ok = true
	m.mu.Unlock()
	return nil
}

func (m *recordingAuthKeyManager) Delete(keyID crypto.KeyID) error {
	return m.base.Delete(keyID)
}

func (m *recordingAuthKeyManager) Last() (crypto.KeyID, crypto.AuthKey, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.last.id, m.last.key, m.last.ok
}

type handshakeClient struct {
	conn        transport.Conn
	serverKey   *crypto.ServerKey
	authManager *recordingAuthKeyManager
	sessions    session.Manager
}

type handshakeResult struct {
	authKeyID  crypto.KeyID
	authKey    crypto.AuthKey
	serverSalt int64
	sessionID  int64
}

func newHandshakeHarness(t *testing.T) (*tlrpc.Server, *recordingAuthKeyManager, session.Manager, *crypto.ServerKey) {
	t.Helper()
	memAuth := crypto.NewMemoryAuthKeyManager()
	recordingAuth := newRecordingAuthKeyManager(memAuth)
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKey, err := crypto.GenerateServerKey()
	if err != nil {
		t.Fatalf("generate server key: %v", err)
	}
	serverKeys.AddKey(serverKey)
	sessions := session.NewMemoryManager()

	srv := tlrpc.NewServer(
		tlrpc.WithAuthKeyManager(recordingAuth),
		tlrpc.WithSessionManager(sessions),
		tlrpc.WithServerKeyManager(serverKeys),
		tlrpc.WithMaxLayer(170),
	)
	srv.RegisterConstructor((&gen.HelpGetConfigRequest{}).ConstructorID(), func() tlrpc.TLObject {
		return &gen.HelpGetConfigRequest{}
	})
	srv.RegisterMethod((&gen.HelpGetConfigRequest{}).ConstructorID(), func(ctx context.Context, obj tlrpc.TLObject) (interface{}, error) {
		_ = obj.(*gen.HelpGetConfigRequest)
		now := int32(time.Now().Unix())
		return &gen.Config{
			Date:                    now,
			Expires:                 now + 3600,
			ThisDc:                  1,
			DcTxtDomainName:         "localhost",
			ChatSizeMax:             200,
			MegagroupSizeMax:        10000,
			ForwardedCountMax:       100,
			OnlineUpdatePeriodMs:    30000,
			OfflineBlurTimeoutMs:    5000,
			OfflineIdleTimeoutMs:    30000,
			OnlineCloudTimeoutMs:    30000,
			NotifyCloudDelayMs:      3000,
			NotifyDefaultDelayMs:    1500,
			PushChatPeriodMs:        30000,
			PushChatLimit:           2,
			EditTimeLimit:           172800,
			RevokeTimeLimit:         172800,
			RevokePmTimeLimit:       172800,
			RatingEDecay:            2419200,
			StickersRecentLimit:     30,
			ChannelsReadMediaPeriod: 604800,
			CallReceiveTimeoutMs:    20000,
			CallRingTimeoutMs:       90000,
			CallConnectTimeoutMs:    30000,
			CallPacketTimeoutMs:     10000,
			MeURLPrefix:             "https://t.me/",
			CaptionLengthMax:        1024,
			MessageLengthMax:        4096,
			WebfileDcID:             1,
		}, nil
	})
	return srv, recordingAuth, sessions, serverKey
}

func writeUnencrypted(conn transport.Conn, msgID int64, body []byte) error {
	msg := &mtproto.UnencryptedMessage{
		AuthKeyID: [8]byte{},
		MsgID:     msgID,
		Data:      body,
	}
	raw, err := msg.Serialize()
	if err != nil {
		return err
	}
	return conn.WriteMessage(raw)
}

func readUnencrypted(conn transport.Conn) (*mtproto.UnencryptedMessage, error) {
	raw, err := conn.ReadMessage()
	if err != nil {
		return nil, err
	}
	msg := &mtproto.UnencryptedMessage{}
	if err := msg.Deserialize(raw); err != nil {
		return nil, err
	}
	return msg, nil
}

func decryptServerPacket(t *testing.T, packet []byte, key crypto.AuthKey) *mtproto.InnerData {
	t.Helper()
	keyID := crypto.KeyID(binary.LittleEndian.Uint64(packet[:8]))
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	dec, err := (&mtproto.EncryptedMessage{
		AuthKeyID:     keyID,
		MsgKey:        msgKey,
		EncryptedData: packet[24:],
	}).Decrypt(key)
	if err != nil {
		t.Fatalf("decrypt encrypted packet: %v", err)
	}
	return dec
}

func performFullHandshake(t *testing.T, c *handshakeClient) handshakeResult {
	t.Helper()
	nonce := [16]byte{0x01, 0x02, 0x03, 0x04}
	var newNonce [32]byte
	for i := range newNonce {
		newNonce[i] = byte(0x10 + i)
	}

	reqPQ := serializeTL(t, func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, reqPQMultiID); err != nil {
			return err
		}
		return mtproto.WriteInt128(w, nonce)
	})
	if err := writeUnencrypted(c.conn, nextClientMsgID(), reqPQ); err != nil {
		t.Fatalf("write req_pq_multi: %v", err)
	}
	resPQMsg, err := readUnencrypted(c.conn)
	if err != nil {
		t.Fatalf("read resPQ: %v", err)
	}

	var serverNonce [16]byte
	var keyFingerprint int64
	{
		r := bytes.NewReader(resPQMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			t.Fatalf("read resPQ ctor: %v", err)
		}
		if ctor != resPQID {
			t.Fatalf("unexpected resPQ ctor: 0x%08x", ctor)
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read nonce: %v", err)
		}
		if gotNonce != nonce {
			t.Fatalf("nonce mismatch")
		}
		serverNonce, err = mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read server nonce: %v", err)
		}
		if _, err := mtproto.ReadBytes(r); err != nil {
			t.Fatalf("read pq bytes: %v", err)
		}
		// Current handshake encoder writes a legacy marker before vector.
		if _, err := mtproto.ReadUint64(r); err != nil {
			t.Fatalf("read fingerprint marker: %v", err)
		}
		var fingerprints []int64
		if err := mtproto.ReadVector(r, func() error {
			fp, err := mtproto.ReadInt64(r)
			if err != nil {
				return err
			}
			fingerprints = append(fingerprints, fp)
			return nil
		}); err != nil {
			t.Fatalf("read fingerprint vector: %v", err)
		}
		if len(fingerprints) == 0 {
			t.Fatalf("resPQ returned no fingerprints")
		}
		keyFingerprint = fingerprints[0]
	}

	pqInner := serializeTL(t, func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, pQInnerDataID); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, []byte{0x17, 0xED, 0x48, 0x94, 0x1A, 0x08, 0xF9, 0x81}); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedP); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedQ); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		return mtproto.WriteInt256(w, newNonce)
	})
	encryptedPQInner, err := rsa.EncryptPKCS1v15(rand.Reader, &c.serverKey.Key.PublicKey, pqInner)
	if err != nil {
		t.Fatalf("encrypt p_q_inner_data: %v", err)
	}

	reqDH := serializeTL(t, func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, reqDHParamsID); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedP); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedQ); err != nil {
			return err
		}
		if err := mtproto.WriteInt64(w, keyFingerprint); err != nil {
			return err
		}
		return mtproto.WriteBytes(w, encryptedPQInner)
	})
	if err := writeUnencrypted(c.conn, nextClientMsgID(), reqDH); err != nil {
		t.Fatalf("write req_DH_params: %v", err)
	}
	serverDHParamsMsg, err := readUnencrypted(c.conn)
	if err != nil {
		t.Fatalf("read server_DH_params_ok: %v", err)
	}

	var encryptedAnswer []byte
	{
		r := bytes.NewReader(serverDHParamsMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			t.Fatalf("read server_DH_params ctor: %v", err)
		}
		if ctor != serverDHParamsOK {
			t.Fatalf("unexpected server_DH_params ctor: 0x%08x", ctor)
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read nonce: %v", err)
		}
		if gotNonce != nonce {
			t.Fatalf("nonce mismatch on server_DH_params")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read server_nonce: %v", err)
		}
		if gotServerNonce != serverNonce {
			t.Fatalf("server nonce mismatch")
		}
		encryptedAnswer, err = mtproto.ReadBytes(r)
		if err != nil {
			t.Fatalf("read encrypted_answer: %v", err)
		}
	}

	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)
	serverDHPlain := make([]byte, len(encryptedAnswer))
	crypto.NewAESIGEDecrypt(tempKey, tempIV).CryptBlocks(serverDHPlain, encryptedAnswer)
	{
		r := bytes.NewReader(serverDHPlain)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			t.Fatalf("read server_DH_inner_data ctor: %v", err)
		}
		if ctor != serverDHInnerData {
			t.Fatalf("unexpected server_DH_inner_data ctor: 0x%08x", ctor)
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read server_DH_inner nonce: %v", err)
		}
		if gotNonce != nonce {
			t.Fatalf("server_DH_inner nonce mismatch")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read server_DH_inner server_nonce: %v", err)
		}
		if gotServerNonce != serverNonce {
			t.Fatalf("server_DH_inner server nonce mismatch")
		}
	}

	gb := new(big.Int).Lsh(big.NewInt(1), 2000).Bytes()
	clientDHPlain := serializeTL(t, func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, clientDHInnerData); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		// The current server parser expects g_b bytes directly (no retry_id field).
		return mtproto.WriteBytes(w, gb)
	})
	if rem := len(clientDHPlain) % 16; rem != 0 {
		clientDHPlain = append(clientDHPlain, make([]byte, 16-rem)...)
	}
	encryptedClientDH := make([]byte, len(clientDHPlain))
	crypto.NewAESIGE(tempKey, tempIV).CryptBlocks(encryptedClientDH, clientDHPlain)

	setClientDH := serializeTL(t, func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, setClientDHParams); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		return mtproto.WriteBytes(w, encryptedClientDH)
	})
	if err := writeUnencrypted(c.conn, nextClientMsgID(), setClientDH); err != nil {
		t.Fatalf("write set_client_DH_params: %v", err)
	}
	dhGenMsg, err := readUnencrypted(c.conn)
	if err != nil {
		t.Fatalf("read dh_gen_ok: %v", err)
	}
	{
		r := bytes.NewReader(dhGenMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			t.Fatalf("read dh_gen ctor: %v", err)
		}
		if ctor != dhGenOK {
			t.Fatalf("unexpected dh_gen ctor: 0x%08x", ctor)
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read dh_gen nonce: %v", err)
		}
		if gotNonce != nonce {
			t.Fatalf("dh_gen nonce mismatch")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read dh_gen server_nonce: %v", err)
		}
		if gotServerNonce != serverNonce {
			t.Fatalf("dh_gen server nonce mismatch")
		}
		gotHash, err := mtproto.ReadInt128(r)
		if err != nil {
			t.Fatalf("read new_nonce_hash1: %v", err)
		}
		wantHash := crypto.ComputeNewNonceHash1(newNonce, serverNonce)
		if gotHash != wantHash {
			t.Fatalf("new_nonce_hash1 mismatch")
		}
	}

	authKeyID, authKey, ok := c.authManager.Last()
	if !ok {
		t.Fatalf("auth key not stored by server")
	}
	if _, err := c.authManager.Get(authKeyID); err != nil {
		t.Fatalf("stored auth key lookup failed: %v", err)
	}

	// Pre-create known session state before first encrypted call.
	sess, err := c.sessions.Create(authKeyID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ServerSalt = 0x1111222233334444
	sess.SessionID = 0x0102030405060708
	if err := c.sessions.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	return handshakeResult{
		authKeyID:  authKeyID,
		authKey:    authKey,
		serverSalt: sess.ServerSalt,
		sessionID:  sess.SessionID,
	}
}

func sendWrappedHelpGetConfig(t *testing.T, conn transport.Conn, hs handshakeResult) *mtprototl.RPCResult {
	t.Helper()
	helpReq := serializeTL(t, func(w io.Writer) error {
		return (&gen.HelpGetConfigRequest{}).SerializeTL(w)
	})
	initReq := serializeTL(t, func(w io.Writer) error {
		return (&mtprototl.InitConnection{
			Flags:          0,
			APIID:          77777,
			DeviceModel:    "compat-test",
			SystemVersion:  "test",
			AppVersion:     "1.0",
			SystemLangCode: "en",
			LangPack:       "",
			LangCode:       "en",
			QueryRaw:       helpReq,
		}).SerializeTL(w)
	})
	wrapped := serializeTL(t, func(w io.Writer) error {
		return (&mtprototl.InvokeWithLayer{
			Layer:    170,
			QueryRaw: initReq,
		}).SerializeTL(w)
	})

	inner := &mtproto.InnerData{
		Salt:      hs.serverSalt,
		SessionID: hs.sessionID,
		MsgID:     nextClientMsgID(),
		SeqNo:     1,
		Data:      wrapped,
	}
	enc, err := inner.Encrypt(hs.authKey, hs.authKeyID)
	if err != nil {
		t.Fatalf("encrypt wrapped request: %v", err)
	}
	if err := conn.WriteMessage(serializeEncrypted(enc)); err != nil {
		t.Fatalf("write encrypted wrapped request: %v", err)
	}

	for i := 0; i < 4; i++ {
		packet, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read encrypted response: %v", err)
		}
		dec := decryptServerPacket(t, packet, hs.authKey)
		if binary.LittleEndian.Uint32(dec.Data[:4]) == mtprototl.MsgsAckID {
			continue
		}
		result := &mtprototl.RPCResult{}
		if err := result.DeserializeTL(bytes.NewReader(dec.Data)); err != nil {
			t.Fatalf("decode rpc_result: %v", err)
		}
		return result
	}
	t.Fatalf("rpc_result not received")
	return nil
}

func dialTCP(t *testing.T, proto transport.Protocol, addr string) transport.Conn {
	t.Helper()
	conn, err := (&transport.TCPTransport{Protocol: proto}).Dial(addr)
	if err != nil {
		t.Fatalf("tcp dial: %v", err)
	}
	return conn
}

func TestMTProtoHandshakeAcrossTCPTransports(t *testing.T) {
	for _, proto := range []transport.Protocol{
		transport.ProtocolAbridged,
		transport.ProtocolIntermediate,
		transport.ProtocolPaddedIntermediate,
		transport.ProtocolFull,
	} {
		t.Run(fmt.Sprintf("proto_%d", proto), func(t *testing.T) {
			srv, authMgr, sessions, serverKey := newHandshakeHarness(t)
			lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
			if err != nil {
				t.Fatalf("listen: %v", err)
			}
			runServer(t, srv, lis)

			conn := dialTCP(t, proto, lis.Addr().String())
			t.Cleanup(func() { _ = conn.Close() })

			client := &handshakeClient{
				conn:        conn,
				serverKey:   serverKey,
				authManager: authMgr,
				sessions:    sessions,
			}
			hs := performFullHandshake(t, client)
			result := sendWrappedHelpGetConfig(t, conn, hs)

			cfg := &gen.Config{}
			if err := cfg.DeserializeTL(bytes.NewReader(result.ResultRaw)); err != nil {
				t.Fatalf("decode help.getConfig result: %v", err)
			}
		})
	}
}

func TestMTProtoHandshakeWebSocketObfuscated2Padded(t *testing.T) {
	srv, authMgr, sessions, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.WebSocketTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)
	addr := lis.Addr().(*net.TCPAddr)

	conn, err := (&transport.WebSocketTransport{Protocol: transport.ProtocolPaddedIntermediate}).Dial(
		fmt.Sprintf("ws://%s:%d", addr.IP.String(), addr.Port),
	)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	client := &handshakeClient{
		conn:        conn,
		serverKey:   serverKey,
		authManager: authMgr,
		sessions:    sessions,
	}
	hs := performFullHandshake(t, client)
	result := sendWrappedHelpGetConfig(t, conn, hs)
	cfg := &gen.Config{}
	if err := cfg.DeserializeTL(bytes.NewReader(result.ResultRaw)); err != nil {
		t.Fatalf("decode help.getConfig result: %v", err)
	}
}

func TestHandshakeNegativeWrongMsgIDGetsBadMsgNotification(t *testing.T) {
	srv, authMgr, sessions, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	conn := dialTCP(t, transport.ProtocolIntermediate, lis.Addr().String())
	t.Cleanup(func() { _ = conn.Close() })

	client := &handshakeClient{
		conn:        conn,
		serverKey:   serverKey,
		authManager: authMgr,
		sessions:    sessions,
	}
	hs := performFullHandshake(t, client)

	reqBody := serializeTL(t, func(w io.Writer) error {
		return (&gen.HelpGetConfigRequest{}).SerializeTL(w)
	})
	msgID1 := nextClientMsgID()
	sendEncrypted := func(msgID int64) {
		inner := &mtproto.InnerData{
			Salt:      hs.serverSalt,
			SessionID: hs.sessionID,
			MsgID:     msgID,
			SeqNo:     1,
			Data:      reqBody,
		}
		enc, err := inner.Encrypt(hs.authKey, hs.authKeyID)
		if err != nil {
			t.Fatalf("encrypt request: %v", err)
		}
		if err := conn.WriteMessage(serializeEncrypted(enc)); err != nil {
			t.Fatalf("write request: %v", err)
		}
	}

	sendEncrypted(msgID1)
	_, _ = conn.ReadMessage()
	sendEncrypted(msgID1 - 4)

	for i := 0; i < 4; i++ {
		packet, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		dec := decryptServerPacket(t, packet, hs.authKey)
		ctor := binary.LittleEndian.Uint32(dec.Data[:4])
		if ctor == mtprototl.BadMsgNotificationID {
			return
		}
	}
	t.Fatalf("bad_msg_notification not received")
}

func TestHandshakeNegativeWrongSaltGetsBadServerSalt(t *testing.T) {
	srv, authMgr, sessions, serverKey := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	conn := dialTCP(t, transport.ProtocolIntermediate, lis.Addr().String())
	t.Cleanup(func() { _ = conn.Close() })

	client := &handshakeClient{
		conn:        conn,
		serverKey:   serverKey,
		authManager: authMgr,
		sessions:    sessions,
	}
	hs := performFullHandshake(t, client)

	reqBody := serializeTL(t, func(w io.Writer) error {
		return (&gen.HelpGetConfigRequest{}).SerializeTL(w)
	})
	inner := &mtproto.InnerData{
		Salt:      hs.serverSalt + 1,
		SessionID: hs.sessionID,
		MsgID:     nextClientMsgID(),
		SeqNo:     1,
		Data:      reqBody,
	}
	enc, err := inner.Encrypt(hs.authKey, hs.authKeyID)
	if err != nil {
		t.Fatalf("encrypt request: %v", err)
	}
	if err := conn.WriteMessage(serializeEncrypted(enc)); err != nil {
		t.Fatalf("write request: %v", err)
	}

	for i := 0; i < 4; i++ {
		packet, err := conn.ReadMessage()
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		dec := decryptServerPacket(t, packet, hs.authKey)
		ctor := binary.LittleEndian.Uint32(dec.Data[:4])
		if ctor == mtprototl.BadServerSaltID {
			return
		}
	}
	t.Fatalf("bad_server_salt not received")
}

func TestHandshakeNegativeInvalidEncryptedDataFailsCleanly(t *testing.T) {
	srv, _, _, _ := newHandshakeHarness(t)
	lis, err := (&transport.TCPTransport{}).Listen("127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	runServer(t, srv, lis)

	conn := dialTCP(t, transport.ProtocolIntermediate, lis.Addr().String())
	t.Cleanup(func() { _ = conn.Close() })

	// auth_key_id + msg_key + non-block-size encrypted payload -> decrypt must fail cleanly.
	raw := make([]byte, 8+16+7)
	binary.LittleEndian.PutUint64(raw[:8], uint64(1))
	copy(raw[8:24], bytes.Repeat([]byte{0x42}, 16))
	copy(raw[24:], []byte{1, 2, 3, 4, 5, 6, 7})
	if err := conn.WriteMessage(raw); err != nil {
		t.Fatalf("write malformed encrypted packet: %v", err)
	}
	_ = conn.Close()

	// Server should continue serving new connections after malformed input.
	conn2 := dialTCP(t, transport.ProtocolIntermediate, lis.Addr().String())
	defer conn2.Close()
}
