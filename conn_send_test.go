package tlrpc

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

type sendTestConn struct {
	ctx    context.Context
	writes [][]byte
}

func (c *sendTestConn) ReadMessage() ([]byte, error) { return nil, nil }
func (c *sendTestConn) WriteMessage(payload []byte) error {
	c.writes = append(c.writes, payload)
	return nil
}
func (c *sendTestConn) Close() error                { return nil }
func (c *sendTestConn) LocalAddr() net.Addr         { return nil }
func (c *sendTestConn) RemoteAddr() net.Addr        { return nil }
func (c *sendTestConn) SetDeadline(time.Time) error { return nil }
func (c *sendTestConn) Context() context.Context    { return c.ctx }

type sendTestObj struct{ id uint32 }

func (s *sendTestObj) ConstructorID() uint32 { return s.id }
func (s *sendTestObj) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, s.id)
}

func TestConnSendLocalPush(t *testing.T) {
	authKeys := crypto.NewMemoryAuthKeyManager()
	keyID := crypto.KeyID(0x1122334455667788)
	var key crypto.AuthKey
	for i := range key {
		key[i] = byte(0x20 + i)
	}
	if err := authKeys.Put(keyID, key); err != nil {
		t.Fatalf("put auth key: %v", err)
	}

	srv := NewServer(WithAuthKeyManager(authKeys))
	sess := &Session{SessionID: 99, ServerSalt: 1234}
	state := &connHandlerState{
		authKeyID: keyID,
		session:   sess,
	}

	conn := &sendTestConn{ctx: context.Background()}
	sc := newServerConn(srv, conn, state)

	obj := &sendTestObj{id: 0x7f000123}
	if err := sc.Send(obj); err != nil {
		t.Fatalf("send: %v", err)
	}
	if len(conn.writes) != 1 {
		t.Fatalf("expected 1 write, got %d", len(conn.writes))
	}

	packet := conn.writes[0]
	gotKeyID := crypto.KeyID(binary.LittleEndian.Uint64(packet[:8]))
	if gotKeyID != keyID {
		t.Fatalf("unexpected key id: got %x want %x", gotKeyID, keyID)
	}
	var msgKey [16]byte
	copy(msgKey[:], packet[8:24])
	inner, err := (&mtproto.EncryptedMessage{
		AuthKeyID:     gotKeyID,
		MsgKey:        msgKey,
		EncryptedData: packet[24:],
	}).Decrypt(key)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if inner.MsgID%4 != 3 {
		t.Fatalf("expected push msg_id mod 4 = 3, got %d", inner.MsgID%4)
	}
	if inner.SeqNo%2 == 0 {
		t.Fatalf("expected content-related seq_no to be odd, got %d", inner.SeqNo)
	}
}
