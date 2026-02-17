package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

// RPCError corresponds to rpc_error#2144ca19 error_code:int error_message:string = RpcError;
type RPCError struct {
	ErrorCode    int32
	ErrorMessage string
}

func (*RPCError) ConstructorID() uint32 { return RPCErrorID }

func (e *RPCError) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, e.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, e.ErrorCode); err != nil {
		return err
	}
	return mtproto.WriteString(w, e.ErrorMessage)
}

func (e *RPCError) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != e.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, e.ConstructorID())
	}
	e.ErrorCode, err = mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	e.ErrorMessage, err = mtproto.ReadString(r)
	return err
}

// RPCResult corresponds to rpc_result#f35c6d01 req_msg_id:long result:Object = RpcResult;
type RPCResult struct {
	ReqMsgID  int64
	Result    Object
	ResultRaw []byte
}

func (*RPCResult) ConstructorID() uint32 { return RPCResultID }

func (r *RPCResult) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, r.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteInt64(w, r.ReqMsgID); err != nil {
		return err
	}
	if r.Result != nil {
		return r.Result.SerializeTL(w)
	}
	_, err := w.Write(r.ResultRaw)
	return err
}

func (r *RPCResult) DeserializeTL(rd io.Reader) error {
	ctor, err := mtproto.ReadUint32(rd)
	if err != nil {
		return err
	}
	if ctor != r.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, r.ConstructorID())
	}
	r.ReqMsgID, err = mtproto.ReadInt64(rd)
	if err != nil {
		return err
	}
	r.ResultRaw, err = io.ReadAll(rd)
	return err
}
