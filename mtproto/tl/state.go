package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// MsgResendReq corresponds to msg_resend_req#7d861a08 msg_ids:Vector<long> = MsgResendReq;
type MsgResendReq struct {
	MsgIDs []int64
}

func (*MsgResendReq) ConstructorID() uint32 { return MsgResendReqID }

func (m *MsgResendReq) SerializeTL(w io.Writer) error {
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

func (m *MsgResendReq) DeserializeTL(r io.Reader) error {
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

// MsgsStateReq corresponds to msgs_state_req#da69fb52 msg_ids:Vector<long> = MsgsStateReq;
type MsgsStateReq struct {
	MsgIDs []int64
}

func (*MsgsStateReq) ConstructorID() uint32 { return MsgsStateReqID }

func (m *MsgsStateReq) SerializeTL(w io.Writer) error {
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

func (m *MsgsStateReq) DeserializeTL(r io.Reader) error {
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

// MsgsStateInfo corresponds to msgs_state_info#04deb57d req_msg_id:long info:bytes = MsgsStateInfo;
type MsgsStateInfo struct {
	ReqMsgID int64
	Info     []byte
}

func (*MsgsStateInfo) ConstructorID() uint32 { return MsgsStateInfoID }

func (m *MsgsStateInfo) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.ReqMsgID); err != nil {
		return err
	}
	return mtproto.WriteBytes(w, m.Info)
}

func (m *MsgsStateInfo) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	m.ReqMsgID, err = mtproto.ReadInt64(r)
	if err != nil {
		return err
	}
	m.Info, err = mtproto.ReadBytes(r)
	return err
}
