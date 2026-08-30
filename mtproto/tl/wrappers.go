package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// InvokeWithLayer corresponds to invokeWithLayer#da9b0d0d {X:Type} layer:int query:!X = X.
type InvokeWithLayer struct {
	Layer    int32
	QueryRaw []byte
}

func (*InvokeWithLayer) ConstructorID() uint32 { return InvokeWithLayerID }

func (m *InvokeWithLayer) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.Layer); err != nil {
		return err
	}
	_, err := w.Write(m.QueryRaw)
	return err
}

func (m *InvokeWithLayer) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.Layer, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	m.QueryRaw, err = io.ReadAll(r)
	return err
}

// InitConnection corresponds to initConnection#c1cd5ea9 {X:Type} ... query:!X = X.
type InitConnection struct {
	Flags          uint32
	APIID          int32
	DeviceModel    string
	SystemVersion  string
	AppVersion     string
	SystemLangCode string
	LangPack       string
	LangCode       string
	QueryRaw       []byte
}

func (*InitConnection) ConstructorID() uint32 { return InitConnectionID }

func (m *InitConnection) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteUint32(w, m.Flags); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.APIID); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.DeviceModel); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.SystemVersion); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.AppVersion); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.SystemLangCode); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.LangPack); err != nil {
		return err
	}
	if err := mtproto.WriteString(w, m.LangCode); err != nil {
		return err
	}
	// Optional proxy/params are intentionally unsupported in this minimal wrapper implementation.
	if m.Flags&(1<<0) != 0 || m.Flags&(1<<1) != 0 {
		return fmt.Errorf("initConnection optional proxy/params are not supported")
	}
	_, err := w.Write(m.QueryRaw)
	return err
}

func (m *InitConnection) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	if m.Flags, err = mtproto.ReadUint32(r); err != nil {
		return err
	}
	if m.APIID, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	if m.DeviceModel, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.SystemVersion, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.AppVersion, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.SystemLangCode, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.LangPack, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.LangCode, err = mtproto.ReadString(r); err != nil {
		return err
	}
	if m.Flags&(1<<0) != 0 || m.Flags&(1<<1) != 0 {
		return fmt.Errorf("initConnection optional proxy/params are not supported")
	}
	m.QueryRaw, err = io.ReadAll(r)
	return err
}

// InvokeAfterMsg corresponds to invokeAfterMsg#cb9f372d {X:Type} msg_id:long query:!X = X.
type InvokeAfterMsg struct {
	MsgID    int64
	QueryRaw []byte
}

func (*InvokeAfterMsg) ConstructorID() uint32 { return InvokeAfterMsgID }

func (m *InvokeAfterMsg) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.MsgID); err != nil {
		return err
	}
	_, err := w.Write(m.QueryRaw)
	return err
}

func (m *InvokeAfterMsg) DeserializeTL(r io.Reader) error {
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
	m.QueryRaw, err = io.ReadAll(r)
	return err
}

// InvokeAfterMsgs corresponds to invokeAfterMsgs#3dc4b4f0 {X:Type} msg_ids:Vector<long> query:!X = X.
type InvokeAfterMsgs struct {
	MsgIDs   []int64
	QueryRaw []byte
}

func (*InvokeAfterMsgs) ConstructorID() uint32 { return InvokeAfterMsgsID }

func (m *InvokeAfterMsgs) SerializeTL(w io.Writer) error {
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
	_, err := w.Write(m.QueryRaw)
	return err
}

func (m *InvokeAfterMsgs) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	ids := make([]int64, 0, 4)
	if err := mtproto.ReadVectorBounded(r, MaxMessageStateIDs, func() error {
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
	m.QueryRaw, err = io.ReadAll(r)
	return err
}

// InvokeWithoutUpdates corresponds to invokeWithoutUpdates#bf9459b7 {X:Type} query:!X = X.
type InvokeWithoutUpdates struct {
	QueryRaw []byte
}

func (*InvokeWithoutUpdates) ConstructorID() uint32 { return InvokeWithoutUpdatesID }

func (m *InvokeWithoutUpdates) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	_, err := w.Write(m.QueryRaw)
	return err
}

func (m *InvokeWithoutUpdates) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	m.QueryRaw, err = io.ReadAll(r)
	return err
}
