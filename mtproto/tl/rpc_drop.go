package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// RPCDropAnswer corresponds to rpc_drop_answer#58e4a740 req_msg_id:long = RpcDropAnswer.
type RPCDropAnswer struct {
	ReqMsgID int64
}

func (*RPCDropAnswer) ConstructorID() uint32 { return RPCDropAnswerID }

func (m *RPCDropAnswer) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	return mtproto.WriteInt64(w, m.ReqMsgID)
}

func (m *RPCDropAnswer) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != m.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, m.ConstructorID())
	}
	m.ReqMsgID, err = mtproto.ReadInt64(r)
	return err
}

// RPCAnswerUnknown reports that the requested RPC answer is not retained.
type RPCAnswerUnknown struct{}

func (*RPCAnswerUnknown) ConstructorID() uint32 { return RPCAnswerUnknownID }
func (m *RPCAnswerUnknown) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, m.ConstructorID())
}
func (m *RPCAnswerUnknown) DeserializeTL(r io.Reader) error {
	return readEmptyConstructor(r, m.ConstructorID())
}

// RPCAnswerDroppedRunning reports an RPC whose handler is still running.
type RPCAnswerDroppedRunning struct{}

func (*RPCAnswerDroppedRunning) ConstructorID() uint32 { return RPCAnswerDroppedRunningID }
func (m *RPCAnswerDroppedRunning) SerializeTL(w io.Writer) error {
	return mtproto.WriteUint32(w, m.ConstructorID())
}
func (m *RPCAnswerDroppedRunning) DeserializeTL(r io.Reader) error {
	return readEmptyConstructor(r, m.ConstructorID())
}

// RPCAnswerDropped describes a retained answer that was dropped.
type RPCAnswerDropped struct {
	MsgID int64
	SeqNo int32
	Bytes int32
}

func (*RPCAnswerDropped) ConstructorID() uint32 { return RPCAnswerDroppedID }

func (m *RPCAnswerDropped) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, m.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, m.MsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.SeqNo); err != nil {
		return err
	}
	return mtproto.WriteInt32(w, m.Bytes)
}

func (m *RPCAnswerDropped) DeserializeTL(r io.Reader) error {
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
	if m.SeqNo, err = mtproto.ReadInt32(r); err != nil {
		return err
	}
	m.Bytes, err = mtproto.ReadInt32(r)
	return err
}

func readEmptyConstructor(r io.Reader, want uint32) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != want {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, want)
	}
	return nil
}
