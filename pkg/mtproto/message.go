// Package mtproto provides MTProto protocol message structures.
package mtproto

import (
	"time"
)

// Message represents an MTProto message.
type Message struct {
	Salt      int64
	SessionID int64
	MessageID int64
	SeqNo     int32
	Length    int32
	Body      []byte
}

// NewMessage creates a new message.
func NewMessage(sessionID int64, body []byte) *Message {
	return &Message{
		SessionID: sessionID,
		MessageID: generateMessageID(),
		SeqNo:     0, // TODO: Implement sequence numbers
		Length:    int32(len(body)),
		Body:      body,
	}
}

// generateMessageID generates a unique message ID.
func generateMessageID() int64 {
	// MTProto message IDs are based on time with specific requirements
	now := time.Now().UnixNano() / int64(time.Millisecond)
	return now << 32
}

// UnencryptedMessage represents an unencrypted message (handshake only).
type UnencryptedMessage struct {
	MessageID int64
	Body      []byte
}

// EncryptedMessage represents an encrypted message.
type EncryptedMessage struct {
	AuthKeyID int64
	MessageID int64
	Data      []byte
}

// Container represents a message container.
type Container struct {
	Messages []*Message
}

// NewContainer creates a new message container.
func NewContainer() *Container {
	return &Container{
		Messages: make([]*Message, 0),
	}
}

// AddMessage adds a message to the container.
func (c *Container) AddMessage(msg *Message) {
	c.Messages = append(c.Messages, msg)
}

// Size returns the number of messages in the container.
func (c *Container) Size() int {
	return len(c.Messages)
}