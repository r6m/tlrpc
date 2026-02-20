package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// NewSessionCreated corresponds to new_session_created#9ec20908.
type NewSessionCreated struct {
	FirstMsgID int64
	UniqueID   int64
	ServerSalt int64
}

func (*NewSessionCreated) ConstructorID() uint32 { return NewSessionCreatedID }

func (m *NewSessionCreated) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.FirstMsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.UniqueID); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.ServerSalt)
}

func (m *NewSessionCreated) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.FirstMsgID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	if m.UniqueID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	m.ServerSalt, err = mtproto.ReadInt64(r)
	return err
}
