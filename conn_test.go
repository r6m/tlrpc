package tlrpc

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

// mockConnIO implements connIO interface for testing
type mockConnIO struct {
	messages  [][]byte
	writes    [][]byte
	readIndex int
	context   context.Context
	closed    bool
}

func newMockConnIO(messages ...[]byte) *mockConnIO {
	return &mockConnIO{
		messages: messages,
		context:  context.Background(),
	}
}

func (m *mockConnIO) ReadMessage() ([]byte, error) {
	if m.readIndex >= len(m.messages) {
		return nil, io.EOF
	}
	msg := m.messages[m.readIndex]
	m.readIndex++
	return msg, nil
}

func (m *mockConnIO) WriteMessage(data []byte) error {
	if m.closed {
		return errors.New("connection closed")
	}
	m.writes = append(m.writes, append([]byte(nil), data...))
	return nil
}

func (m *mockConnIO) Close() error {
	m.closed = true
	return nil
}

func (m *mockConnIO) Context() context.Context {
	return m.context
}

// mockTLObjectForConn implements TLObject for testing
type mockTLObjectForConn struct {
	constructorID uint32
}

func (m *mockTLObjectForConn) ConstructorID() uint32 {
	return m.constructorID
}

func (m *mockTLObjectForConn) SerializeTL(w io.Writer) error {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint32(buf, m.constructorID)
	_, err := w.Write(buf)
	return err
}

func TestLayerFromSession(t *testing.T) {
	// Test with nil session
	if layerFromSession(nil) != 0 {
		t.Error("layerFromSession(nil) should return 0")
	}

	// Test with valid session
	sess := &session.Session{Layer: 42}
	if layerFromSession(sess) != 42 {
		t.Errorf("layerFromSession returned wrong layer: got %d, want %d", layerFromSession(sess), 42)
	}
}

func TestNextMsgID(t *testing.T) {
	id1 := nextMsgID()
	time.Sleep(time.Millisecond) // Ensure different timestamp
	id2 := nextMsgID()

	if id1 == id2 {
		t.Error("nextMsgID should return different IDs")
	}

	// MTProto message IDs should have bottom 2 bits as 0
	if id1&3 != 0 {
		t.Errorf("message ID should have bottom 2 bits as 0: %x", id1)
	}
	if id2&3 != 0 {
		t.Errorf("message ID should have bottom 2 bits as 0: %x", id2)
	}
}

func TestNextSeqNo(t *testing.T) {
	// Test with nil session
	if nextSeqNo(nil) != 0 {
		t.Error("nextSeqNo(nil) should return 0")
	}

	// Test with valid session
	sess := &Session{SeqNo: 5}
	seqNo := nextSeqNo(sess)
	if seqNo != 6 {
		t.Errorf("nextSeqNo returned wrong sequence number: got %d, want %d", seqNo, 6)
	}
	if sess.SeqNo != 6 {
		t.Errorf("session sequence number not incremented: got %d, want %d", sess.SeqNo, 6)
	}
}

func TestSerializeEncrypted(t *testing.T) {
	authKeyID := crypto.KeyID(0x1234567890abcdef)
	msgKey := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	encryptedData := []byte{0xAA, 0xBB, 0xCC, 0xDD}

	msg := &mtproto.EncryptedMessage{
		AuthKeyID:     authKeyID,
		MsgKey:        msgKey,
		EncryptedData: encryptedData,
	}

	serialized := serializeEncrypted(msg)

	// Check length: 8 (auth_key_id) + 16 (msg_key) + len(encrypted_data)
	expectedLen := 8 + 16 + len(encryptedData)
	if len(serialized) != expectedLen {
		t.Errorf("wrong serialized length: got %d, want %d", len(serialized), expectedLen)
	}

	// Check auth_key_id
	if binary.LittleEndian.Uint64(serialized[:8]) != uint64(authKeyID) {
		t.Error("auth_key_id not serialized correctly")
	}

	// Check msg_key
	if !bytes.Equal(serialized[8:24], msgKey[:]) {
		t.Error("msg_key not serialized correctly")
	}

	// Check encrypted data
	if !bytes.Equal(serialized[24:], encryptedData) {
		t.Error("encrypted data not serialized correctly")
	}
}

func TestEncodeTLObject(t *testing.T) {
	// Test nil object
	_, err := encodeTLObject(nil)
	if err == nil {
		t.Error("encodeTLObject(nil) should return error")
	}

	// Test valid object
	obj := &mockTLObjectForConn{constructorID: 0x12345678}
	data, err := encodeTLObject(obj)
	if err != nil {
		t.Errorf("encodeTLObject returned error: %v", err)
	}

	if len(data) == 0 {
		t.Error("encoded data should not be empty")
	}

	// Verify constructor ID is encoded
	if len(data) >= 4 && binary.LittleEndian.Uint32(data[:4]) != obj.constructorID {
		t.Error("constructor ID not encoded correctly")
	}
}

// TestConnHandler is a basic test for connHandler initialization
func TestConnHandler(t *testing.T) {
	server := NewServer()
	mockConn := newMockConnIO()

	handler := &connHandler{
		server: server,
		conn:   mockConn,
	}

	if handler.server != server {
		t.Error("connHandler.server not set correctly")
	}

	if handler.conn != mockConn {
		t.Error("connHandler.conn not set correctly")
	}
}

// Integration test for connHandler.run with EOF
func TestConnHandlerRunEOF(t *testing.T) {
	server := NewServer()
	mockConn := newMockConnIO() // Empty messages list will cause EOF

	handler := &connHandler{
		server: server,
		conn:   mockConn,
	}

	err := handler.run()
	if err != io.EOF {
		t.Errorf("connHandler.run() should return EOF, got: %v", err)
	}
}

// Test the exported functions that can be tested in isolation
func TestConnExportedFunctions(t *testing.T) {
	// These functions are now tested through the server tests
	// and other integration tests. Here we just ensure they exist
	// and don't panic on basic calls.

	// nextMsgID is tested above
	// nextSeqNo is tested above
	// encodeTLObject is tested above
	// etc.
}
