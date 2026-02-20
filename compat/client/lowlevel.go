package client

import (
	"errors"

	"github.com/r6m/tlrpc"
	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

// NextMsgID returns a client-side MTProto message ID.
func NextMsgID() int64 { return nextMsgID() }

// SerializeTL serializes a TL object to bytes.
func SerializeTL(obj tlrpc.TLObject) ([]byte, error) { return encodeTL(obj) }

// EncryptMessage encrypts a payload using the client's auth key.
func (c *Client) EncryptMessage(msgID int64, seqNo int32, payload []byte) ([]byte, error) {
	return c.EncryptMessageWithSalt(c.serverSalt, msgID, seqNo, payload)
}

// EncryptMessageWithSalt encrypts a payload using an explicit server salt.
func (c *Client) EncryptMessageWithSalt(salt int64, msgID int64, seqNo int32, payload []byte) ([]byte, error) {
	if c.authKeyID == 0 {
		return nil, errors.New("compat client: missing auth key")
	}
	inner := &mtproto.InnerData{
		Salt:      salt,
		SessionID: c.sessionID,
		MsgID:     msgID,
		SeqNo:     seqNo,
		Data:      payload,
	}
	enc, err := inner.EncryptFromClient(c.authKey, c.authKeyID)
	if err != nil {
		return nil, err
	}
	return serializeEncrypted(enc), nil
}

// DecryptMessage decrypts a server packet using the client's auth key.
func (c *Client) DecryptMessage(packet []byte) (*mtproto.InnerData, error) {
	return decryptPacket(packet, c.authKey)
}

// SetSession seeds the client with a known auth key + session parameters.
func (c *Client) SetSession(authKeyID crypto.KeyID, authKey crypto.AuthKey, serverSalt, sessionID int64) {
	c.authKeyID = authKeyID
	c.authKey = authKey
	c.serverSalt = serverSalt
	c.sessionID = sessionID
}

// Session returns a snapshot of the current session state.
func (c *Client) Session() SessionInfo {
	return SessionInfo{
		AuthKeyID:  c.authKeyID,
		AuthKey:    c.authKey,
		ServerSalt: c.serverSalt,
		SessionID:  c.sessionID,
	}
}
