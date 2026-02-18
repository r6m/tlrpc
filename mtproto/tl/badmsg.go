package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// BadMsgNotification corresponds to bad_msg_notification#a7eff811.
type BadMsgNotification struct {
	BadMsgID  int64
	BadMsgSeq int32
	ErrorCode int32
}

func (*BadMsgNotification) ConstructorID() uint32 { return BadMsgNotificationID }

func (m *BadMsgNotification) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.BadMsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.BadMsgSeq); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, m.ErrorCode)
}

func (m *BadMsgNotification) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.BadMsgID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	if m.BadMsgSeq, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	m.ErrorCode, err = mtproto.ReadInt32(r)
	return err
}

// BadServerSalt corresponds to bad_server_salt#edab447b.
type BadServerSalt struct {
	BadMsgID  int64
	BadMsgSeq int32
	ErrorCode int32
	NewSalt   int64
}

func (*BadServerSalt) ConstructorID() uint32 { return BadServerSaltID }

func (m *BadServerSalt) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.BadMsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.BadMsgSeq); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.ErrorCode); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.NewSalt)
}

func (m *BadServerSalt) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.BadMsgID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	if m.BadMsgSeq, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	if m.ErrorCode, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	m.NewSalt, err = mtproto.ReadInt64(r)
	return err
}
