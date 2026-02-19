package tlrpc

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	mtprototl "github.com/r6m/tlrpc/mtproto/tl"
	"github.com/r6m/tlrpc/session"
)

func TestServerPublishSendsEncryptedUpdate(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryManager()
	srv := NewServer(WithAuthKeyManager(authKeys), WithSessionManager(sessions))

	keyID := crypto.KeyID(0x4455667788990011)
	var key crypto.AuthKey
	for i := range key {
		key[i] = byte(0x10 + i)
	}
	if err := authKeys.Put(keyID, key); err != nil {
		t.Fatalf("put auth key: %v", err)
	}

	conn := newMockConnIO()
	srv.updateHub.bind(42, updateBinding{
		conn:      conn,
		keyID:     keyID,
		sessionID: 100,
		salt:      200,
		msgIDs:    mtproto.NewMsgIDGenerator(),
		seqNos:    mtproto.NewSeqNoGenerator(0),
	})

	update := &mockTLObjectForConn{constructorID: 0x7f000001}
	if err := srv.Publish(42, update); err != nil {
		t.Fatalf("publish: %v", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("expected 1 published update packet, got %d", len(conn.writes))
	}

	packet := conn.writes[0]
	gotKeyID := crypto.KeyID(binary.LittleEndian.Uint64(packet[:8]))
	if gotKeyID != keyID {
		t.Fatalf("unexpected key id: got %x, want %x", gotKeyID, keyID)
	}
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	inner, err := (&mtproto.EncryptedMessage{
		AuthKeyID:     gotKeyID,
		MsgKey:        msgKey,
		EncryptedData: packet[24:],
	}).Decrypt(key)
	if err != nil {
		t.Fatalf("decrypt published packet: %v", err)
	}
	gotCtor := binary.LittleEndian.Uint32(inner.Data[:4])
	if gotCtor != update.constructorID {
		t.Fatalf("unexpected update ctor: got %08x, want %08x", gotCtor, update.constructorID)
	}
}

func TestHandleEncryptedAckAfterPublishedUpdate(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	sessions := session.NewMemoryManager()
	srv := NewServer(WithAuthKeyManager(authKeys), WithSessionManager(sessions))

	keyID := crypto.KeyID(0x1111222233334444)
	var key crypto.AuthKey
	for i := range key {
		key[i] = byte(i)
	}
	if err := authKeys.Put(keyID, key); err != nil {
		t.Fatalf("put auth key: %v", err)
	}
	sess, err := sessions.Create(keyID)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	sess.ServerSalt = 123
	sess.SessionID = 456
	if err := sessions.Save(sess); err != nil {
		t.Fatalf("save session: %v", err)
	}

	ackBody, err := encodeTLObject(&mtprototl.MsgsAck{MsgIDs: []int64{mtproto.NewMsgIDGenerator().Next()}})
	if err != nil {
		t.Fatalf("encode ack body: %v", err)
	}
	inner := &mtproto.InnerData{
		Salt:      sess.ServerSalt,
		SessionID: sess.SessionID,
		MsgID:     int64(time.Now().Unix()<<32) &^ 3,
		SeqNo:     1,
		Data:      ackBody,
	}
	enc, err := inner.Encrypt(key, keyID)
	if err != nil {
		t.Fatalf("encrypt ack packet: %v", err)
	}
	packet := serializeEncrypted(enc)

	conn := newMockConnIO(packet)
	h := &connHandler{server: srv, conn: conn}
	if err := h.run(); err == nil {
		t.Fatalf("expected EOF after processing single packet")
	}
	if len(conn.writes) == 0 {
		t.Fatalf("expected server acknowledgment write after client ack")
	}
}
