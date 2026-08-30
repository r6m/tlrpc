package tl

import (
	"fmt"
	"io"

	"github.com/r6m/tlrpc/mtproto"
)

const MaxContainerMessages = 8192

// Message corresponds to message msg_id:long seqno:int bytes:int body:Object = Message;
type Message struct {
	MsgID   int64
	SeqNo   int32
	Bytes   int32
	Body    Object
	BodyRaw []byte
}

func (m *Message) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteInt64(w, m.MsgID); err != nil {
		return err
	}
	if err := mtproto.WriteInt32(w, m.SeqNo); err != nil {
		return err
	}
	var body []byte
	var err error
	if m.Body != nil {
		body, err = serializeObject(m.Body)
		if err != nil {
			return err
		}
	} else {
		body = m.BodyRaw
	}
	if err := mtproto.WriteInt32(w, int32(len(body))); err != nil {
		return err
	}
	_, err = w.Write(body)
	return err
}

func (m *Message) DeserializeTL(r io.Reader) error {
	var err error
	m.MsgID, err = mtproto.ReadInt64(r)
	if err != nil {
		return err
	}
	m.SeqNo, err = mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	m.Bytes, err = mtproto.ReadInt32(r)
	if err != nil {
		return err
	}
	if m.Bytes < 0 {
		return fmt.Errorf("negative message bytes: %d", m.Bytes)
	}
	m.BodyRaw, err = mtproto.ReadSizedBytes(r, int(m.Bytes))
	return err
}

// MsgContainer corresponds to msg_container#73f1f8dc messages:vector<%Message> = MessageContainer;
type MsgContainer struct {
	Messages []Message
}

func (*MsgContainer) ConstructorID() uint32 { return MsgContainerID }

func (c *MsgContainer) SerializeTL(w io.Writer) error {
	if err := mtproto.WriteUint32(w, c.ConstructorID()); err != nil {
		return err
	}
	if err := mtproto.WriteVectorHeader(w, len(c.Messages)); err != nil {
		return err
	}
	for i := range c.Messages {
		if err := c.Messages[i].SerializeTL(w); err != nil {
			return err
		}
	}
	return nil
}

func (c *MsgContainer) DeserializeTL(r io.Reader) error {
	ctor, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}
	if ctor != c.ConstructorID() {
		return fmt.Errorf("wrong constructor: got %08x, want %08x", ctor, c.ConstructorID())
	}
	messages := make([]Message, 0, 2)
	if err := mtproto.ReadVectorBounded(r, MaxContainerMessages, func() error {
		var m Message
		if err := m.DeserializeTL(r); err != nil {
			return err
		}
		messages = append(messages, m)
		return nil
	}); err != nil {
		return err
	}
	c.Messages = messages
	return nil
}
