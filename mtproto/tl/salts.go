package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

const MaxFutureSalts = 64

// GetFutureSaltsRequest corresponds to get_future_salts#b921bd04 num:int = FutureSalts.
type GetFutureSaltsRequest struct {
	Num int32
}

func (*GetFutureSaltsRequest) ConstructorID() uint32 { return GetFutureSaltsID }

func (m *GetFutureSaltsRequest) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, m.Num)
}

func (m *GetFutureSaltsRequest) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	m.Num, err = mtproto.ReadInt32(r)
	return err
}

// FutureSalt corresponds to future_salt#0949d9dc valid_since:int valid_until:int salt:long = FutureSalt.
type FutureSalt struct {
	ValidSince int32
	ValidUntil int32
	Salt       int64
}

func (*FutureSalt) ConstructorID() uint32 { return FutureSaltID }

func (m *FutureSalt) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	return m.serializeBare(w)
}

func (m *FutureSalt) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	return m.deserializeBare(r)
}

func (m *FutureSalt) serializeBare(w io.Writer) error {
	if err := mtproto.WriteInt32(w, m.ValidSince); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.ValidUntil); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.Salt)
}

func (m *FutureSalt) deserializeBare(r io.Reader) error {
	var err error
	if m.ValidSince, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	if m.ValidUntil, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	m.Salt, err = mtproto.ReadInt64(r)
	return err
}

// FutureSalts corresponds to future_salts#ae500895 req_msg_id:long now:int salts:vector<future_salt> = FutureSalts.
//
// Note: salts is encoded as bare vector here (count + bare future_salt entries),
// matching MTProto schema "vector<future_salt>".
type FutureSalts struct {
	ReqMsgID int64
	Now      int32
	Salts    []FutureSalt
}

func (*FutureSalts) ConstructorID() uint32 { return FutureSaltsID }

func (m *FutureSalts) SerializeTL(w io.Writer) error {
	if len(m.Salts) > MaxFutureSalts {
		return fmt.Errorf("future_salts count exceeds limit: %d", len(m.Salts))
	}
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.ReqMsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.Now); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, int32(len(m.Salts))); err != nil {
		return err
	}
	for i := range m.Salts {
		if err := m.Salts[i].serializeBare(w); err != nil {
			return err
		}
	}
	return nil
}

func (m *FutureSalts) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.ReqMsgID, err = mtproto.ReadInt64(r); err != nil {
		return err
	}
	if m.Now, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	count, err := mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	if count < 0 {
		return fmt.Errorf("invalid future_salts count: %d", count)
	}
	if count > MaxFutureSalts {
		return fmt.Errorf("future_salts count exceeds limit: %d", count)
	}
	m.Salts = make([]FutureSalt, 0, count)
	for i := int32(0); i < count; i++ {
		var item FutureSalt
		if err := item.deserializeBare(r); err != nil {
			return err
		}
		m.Salts = append(m.Salts, item)
	}
	return nil
}
