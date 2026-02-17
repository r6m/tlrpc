package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// MsgsAck corresponds to msgs_ack#62d6b459 msg_ids:Vector<long> = MsgsAck;
type MsgsAck struct {
	MsgIDs []int64
}

func (*MsgsAck) ConstructorID() uint32 { return MsgsAckID }

func (m *MsgsAck) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteVectorHeader(w, len(m.MsgIDs)); err != nil {
		return err
	}
	for _, id := range m.MsgIDs {
		if err := mtproto.WriteInt64(w, id); err != nil {
			return err
		}
	}
	return nil
}

func (m *MsgsAck) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	ids := make([]int64, 0, 4)
	if err := mtproto.ReadVector(r, func() error {
		id, err := mtproto.ReadInt64(r)
		if err != nil {
			return err
		}
		ids = append(ids, id)
		return nil
	}); err != nil {
		return err
	}
	m.MsgIDs = ids
	return nil
}
