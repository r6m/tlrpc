package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// Ping corresponds to ping#7abe77ec ping_id:long = Pong.
type Ping struct {
	PingID int64
}

func (*Ping) ConstructorID() uint32 { return PingID }

func (m *Ping) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.PingID)
}

func (m *Ping) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	m.PingID, err = mtproto.ReadInt64(r)
	return err
}

// PingDelayDisconnect corresponds to ping_delay_disconnect#f3427b8c
// ping_id:long disconnect_delay:int = Pong.
type PingDelayDisconnect struct {
	PingID          int64
	DisconnectDelay int32
}

func (*PingDelayDisconnect) ConstructorID() uint32 { return PingDelayDisconnectID }

func (m *PingDelayDisconnect) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.PingID); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, m.DisconnectDelay)
}

func (m *PingDelayDisconnect) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.PingID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	m.DisconnectDelay, err = mtproto.ReadInt32(r)
	return err
}

// Pong corresponds to pong#347773c5 msg_id:long ping_id:long = Pong.
type Pong struct {
	MsgID  int64
	PingID int64
}

func (*Pong) ConstructorID() uint32 { return PongID }

func (m *Pong) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.MsgID); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.PingID)
}

func (m *Pong) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.MsgID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	m.PingID, err = mtproto.ReadInt64(r)
	return err
}
