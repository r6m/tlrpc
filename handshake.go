package tlrpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
	"github.com/r6m/tlrpc/session"
)

var (
	ErrHandshakeFailed    = errors.New("tlrpc: handshake failed")
	ErrInvalidHandshake   = errors.New("tlrpc: invalid handshake message")
	ErrUnsupportedMessage = errors.New("tlrpc: unsupported message type")
)

// TempDHState stores temporary DH state between req_DH_params and set_client_DH_params
type TempDHState struct {
	Nonce        [16]byte
	ServerNonce  [16]byte
	NewNonce     [32]byte
	DHParams     *crypto.DHParams
	TempKey      []byte
	TempIV       []byte
}

var tempDHStates = make(map[[32]byte]*TempDHState) // keyed by new_nonce

// validateMessageID checks if a message ID is valid according to MTProto rules
func validateMessageID(msgID int64) error {
	// MTProto message IDs must have the bottom 2 bits as 0
	if msgID&3 != 0 {
		return errors.New("mtproto: invalid message ID (bottom bits not zero)")
	}

	// Check if message ID is within reasonable time bounds (±30 seconds)
	now := time.Now().UnixNano() / 1000000 // convert to milliseconds like MTProto
	msgTime := msgID >> 32 // extract timestamp from high 32 bits

	timeDiff := now - msgTime
	if timeDiff < -30000 || timeDiff > 30000 { // ±30 seconds
		return errors.New("mtproto: message ID timestamp out of bounds")
	}

	return nil
}


type DefaultHandshakeHandler struct {
	authKeys crypto.AuthKeyManager
	serverKeys crypto.ServerKeyManager
}

func NewDefaultHandshakeHandler(authKeys crypto.AuthKeyManager, serverKeys crypto.ServerKeyManager) *DefaultHandshakeHandler {
	return &DefaultHandshakeHandler{authKeys: authKeys, serverKeys: serverKeys}
}

func (h *DefaultHandshakeHandler) HandleUnencrypted(ctx context.Context, msgID int64, data []byte) ([]byte, error) {
	if len(data) < 4 {
		return nil, ErrInvalidHandshake
	}

	r := bytes.NewReader(data)
	constructorID, err := mtproto.ReadUint32(r)
	if err != nil {
		return nil, err
	}

	switch constructorID {
	case 0x60469778: // req_pq
		return h.handleReqPQ(data)
	case 0xbe7e8ef1: // req_pq_multi
		return h.handleReqPQ(data) // same logic
	case 0xd712e4be: // req_DH_params
		return h.handlePQInnerData(data)
	case 0xf5045f1f: // set_client_DH_params
		return h.handleReqSetClientDHParams(data)
	default:
		return nil, ErrUnsupportedMessage
	}
}

func (h *DefaultHandshakeHandler) handleReqPQ(data []byte) ([]byte, error) {
	r := bytes.NewReader(data)
	_, _ = mtproto.ReadUint32(r)

	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}

	serverNonce, err := generateNonce()
	if err != nil {
		return nil, err
	}

	// Use a real PQ that factors to two primes (17 * 19 = 323)
	// This matches the PQ used in MTProto samples: 0x17ED48941A08F981
	pq := []byte{0x17, 0xED, 0x48, 0x94, 0x1A, 0x08, 0xF9, 0x81}

	// Get all server key fingerprints
	serverKeys, err := h.serverKeys.GetAllKeys()
	if err != nil {
		return nil, err
	}

	resp := &bytes.Buffer{}
	if err := mtproto.WriteUint32(resp, 0x05162463); err != nil { // resPQ
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(resp, pq); err != nil {
		return nil, err
	}
	if err := mtproto.WriteUint64(resp, 1); err != nil {
		return nil, err
	}
	if err := mtproto.WriteVectorHeader(resp, len(serverKeys)); err != nil {
		return nil, err
	}
	for _, key := range serverKeys {
		if err := mtproto.WriteInt64(resp, key.ID); err != nil {
			return nil, err
		}
	}

	return resp.Bytes(), nil
}

func (h *DefaultHandshakeHandler) handlePQInnerData(data []byte) ([]byte, error) {
	r := bytes.NewReader(data)
	_, _ = mtproto.ReadUint32(r) // constructor

	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}
	serverNonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}
	p, err := mtproto.ReadBytes(r)
	if err != nil {
		return nil, err
	}
	q, err := mtproto.ReadBytes(r)
	if err != nil {
		return nil, err
	}
	publicKeyFingerprint, err := mtproto.ReadInt64(r)
	if err != nil {
		return nil, err
	}
	encryptedData, err := mtproto.ReadBytes(r)
	if err != nil {
		return nil, err
	}

	// Get the server key by fingerprint
	var fpBytes [8]byte
	binary.LittleEndian.PutUint64(fpBytes[:], uint64(publicKeyFingerprint))
	serverKey, err := h.serverKeys.GetKey(fpBytes[:])
	if err != nil {
		return nil, err
	}

	// RSA decrypt the encrypted data
	decryptedData, err := crypto.DecryptRSA(serverKey.Key, encryptedData)
	if err != nil {
		return nil, err
	}

	// Parse p_q_inner_data
	dr := bytes.NewReader(decryptedData)
	pqInnerConstructor, err := mtproto.ReadUint32(dr)
	if err != nil {
		return nil, err
	}
	if pqInnerConstructor != 0x83c95aec { // p_q_inner_data
		return nil, ErrInvalidHandshake
	}

	pqInner, err := mtproto.ReadBytes(dr)
	if err != nil {
		return nil, err
	}
	pInner, err := mtproto.ReadBytes(dr)
	if err != nil {
		return nil, err
	}
	qInner, err := mtproto.ReadBytes(dr)
	if err != nil {
		return nil, err
	}
	innerNonce, err := mtproto.ReadInt128(dr)
	if err != nil {
		return nil, err
	}
	innerServerNonce, err := mtproto.ReadInt128(dr)
	if err != nil {
		return nil, err
	}
	newNonce, err := mtproto.ReadInt256(dr)
	if err != nil {
		return nil, err
	}

	// Verify nonce, server_nonce, p, q match
	if innerNonce != nonce || innerServerNonce != serverNonce {
		return nil, ErrInvalidHandshake
	}
	if !bytes.Equal(p, pInner) || !bytes.Equal(q, qInner) {
		return nil, ErrInvalidHandshake
	}

	// Reconstruct pq from p and q to verify
	pBig := new(big.Int).SetBytes(pInner)
	qBig := new(big.Int).SetBytes(qInner)
	pqReconstructed := new(big.Int).Mul(pBig, qBig)
	pqReconstructedBytes := pqReconstructed.Bytes()
	if !bytes.Equal(pqInner, pqReconstructedBytes) {
		return nil, ErrInvalidHandshake
	}
	if !bytes.Equal(p, pInner) || !bytes.Equal(q, qInner) {
		return nil, ErrInvalidHandshake
	}

	// Generate DH parameters
	dhParams, err := crypto.GenerateDHParams()
	if err != nil {
		return nil, err
	}

	// Create server_DH_inner_data
	serverDHInner := &bytes.Buffer{}
	if err := mtproto.WriteUint32(serverDHInner, 0xb5890dba); err != nil { // server_DH_inner_data
		return nil, err
	}
	if err := mtproto.WriteUint32(serverDHInner, 0); err != nil { // nonce (placeholder)
		return nil, err
	}
	if err := mtproto.WriteUint32(serverDHInner, 0); err != nil { // server_nonce (placeholder)
		return nil, err
	}
	if err := mtproto.WriteUint32(serverDHInner, 3); err != nil { // g
		return nil, err
	}
	if err := mtproto.WriteBigInt(serverDHInner, dhParams.P, 256); err != nil { // dh_prime
		return nil, err
	}
	if err := mtproto.WriteBigInt(serverDHInner, dhParams.Ga, 256); err != nil { // g_a
		return nil, err
	}
	if err := mtproto.WriteUint32(serverDHInner, uint32(time.Now().Unix())); err != nil { // server_time
		return nil, err
	}

	serverDHData := serverDHInner.Bytes()
	// Set nonce and server_nonce in the data
	copy(serverDHData[4:20], nonce[:])
	copy(serverDHData[20:36], serverNonce[:])

	// Encrypt with AES using key derived from new_nonce + server_nonce
	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)

	// Store DH state for set_client_DH_params
	tempDHStates[newNonce] = &TempDHState{
		Nonce:       nonce,
		ServerNonce: serverNonce,
		NewNonce:    newNonce,
		DHParams:    dhParams,
		TempKey:     tempKey,
		TempIV:      tempIV,
	}

	block := crypto.NewAESIGE(tempKey, tempIV)
	encryptedDHData := make([]byte, len(serverDHData))
	block.CryptBlocks(encryptedDHData, serverDHData)

	// Send server_DH_params_ok
	resp := &bytes.Buffer{}
	if err := mtproto.WriteUint32(resp, 0xd0e8075c); err != nil { // server_DH_params_ok
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(resp, encryptedDHData); err != nil {
		return nil, err
	}

	return resp.Bytes(), nil
}

func (h *DefaultHandshakeHandler) handleReqSetClientDHParams(data []byte) ([]byte, error) {
	r := bytes.NewReader(data)
	_, _ = mtproto.ReadUint32(r) // constructor

	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}
	serverNonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return nil, err
	}
	encryptedData, err := mtproto.ReadBytes(r)
	if err != nil {
		return nil, err
	}

	// Find the temp DH state (we don't have new_nonce yet, so we need to try all)
	var tempState *TempDHState
	var newNonce [32]byte
	for nn, state := range tempDHStates {
		if state.Nonce == nonce && state.ServerNonce == serverNonce {
			tempState = state
			newNonce = nn
			break
		}
	}
	if tempState == nil {
		return nil, ErrInvalidHandshake
	}

	// Decrypt client_DH_inner_data
	block := crypto.NewAESIGEDecrypt(tempState.TempKey, tempState.TempIV)
	decryptedData := make([]byte, len(encryptedData))
	block.CryptBlocks(decryptedData, encryptedData)

	// Parse client_DH_inner_data
	dr := bytes.NewReader(decryptedData)
	clientDHConstructor, err := mtproto.ReadUint32(dr)
	if err != nil {
		return nil, err
	}
	if clientDHConstructor != 0x6643b654 { // client_DH_inner_data
		return nil, ErrInvalidHandshake
	}

	clientNonce, err := mtproto.ReadInt128(dr)
	if err != nil {
		return nil, err
	}
	clientServerNonce, err := mtproto.ReadInt128(dr)
	if err != nil {
		return nil, err
	}
	if clientNonce != nonce || clientServerNonce != serverNonce {
		return nil, ErrInvalidHandshake
	}

	gbBytes, err := mtproto.ReadBytes(dr)
	if err != nil {
		return nil, err
	}

	// Validate g_b
	gb := new(big.Int).SetBytes(gbBytes)
	minGB := new(big.Int).Lsh(big.NewInt(1), 2048-64)
	maxGB := new(big.Int).Sub(tempState.DHParams.P, minGB)
	if gb.Cmp(minGB) <= 0 || gb.Cmp(maxGB) >= 0 {
		return nil, ErrInvalidHandshake
	}

	// Compute auth_key = g_b^a mod p
	tempState.DHParams.ComputeAuthKey(gb)
	authKeyBytes := tempState.DHParams.AuthKeyBytes()

	// Compute auth_key_id = last 8 bytes of SHA1(auth_key)
	authKeyID := crypto.ComputeAuthKeyID(authKeyBytes)

	// Store the auth key
	authKey := crypto.AuthKey{}
	copy(authKey[:], authKeyBytes)
	if err := h.authKeys.Put(crypto.KeyID(authKeyID), authKey); err != nil {
		return nil, err
	}

	// Clean up temp state
	delete(tempDHStates, newNonce)

	// Send dh_gen_ok
	resp := &bytes.Buffer{}
	if err := mtproto.WriteUint32(resp, 0x3bcbf734); err != nil { // dh_gen_ok
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(resp, serverNonce); err != nil {
		return nil, err
	}

	// Compute new_nonce_hash1 = SHA1(new_nonce)[first 16 bytes] XOR SHA1(server_nonce)[first 16 bytes] XOR SHA1(new_nonce)[16:32]
	newNonceHash1 := crypto.ComputeNewNonceHash1(newNonce, serverNonce)
	if err := mtproto.WriteInt128(resp, newNonceHash1); err != nil {
		return nil, err
	}

	return resp.Bytes(), nil
}

func generateNonce() ([16]byte, error) {
	var nonce [16]byte
	_, err := rand.Read(nonce[:])
	return nonce, err
}

func (h *connHandler) handleUnencryptedMessage(msg *mtproto.UnencryptedMessage) error {
	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	handler := h.server.handshakeHandler
	if handler == nil {
		handler = NewDefaultHandshakeHandler(h.server.authKeys, h.server.serverKeys)
	}

	respData, err := handler.HandleUnencrypted(ctx, msg.MsgID, msg.Data)
	if err != nil {
		return err
	}

	respMsg := &mtproto.UnencryptedMessage{
		AuthKeyID: [8]byte{},
		MsgID:     nextMsgID(),
		Data:      respData,
	}

	respBytes, err := respMsg.Serialize()
	if err != nil {
		return err
	}

	return h.conn.WriteMessage(respBytes)
}

func (h *connHandler) processMessage(payload []byte) error {
	if len(payload) < 8 {
		return io.ErrUnexpectedEOF
	}

	keyID := binary.LittleEndian.Uint64(payload[:8])

	if keyID == 0 {
		msg := &mtproto.UnencryptedMessage{}
		if err := msg.Deserialize(payload); err != nil {
			return err
		}
		return h.handleUnencryptedMessage(msg)
	}

	return h.handleEncryptedMessage(payload, crypto.KeyID(keyID))
}

func (h *connHandler) handleEncryptedMessage(payload []byte, keyID crypto.KeyID) error {
	if len(payload) < 24 {
		return io.ErrUnexpectedEOF
	}

	var msgKey [16]byte
	copy(msgKey[:], payload[8:24])
	enc := &mtproto.EncryptedMessage{
		AuthKeyID:     keyID,
		MsgKey:        msgKey,
		EncryptedData: payload[24:],
	}

	authKey, err := h.server.authKeys.Get(keyID)
	if err != nil {
		return ErrUnauthorized
	}
	inner, err := enc.Decrypt(authKey)
	if err != nil {
		return err
	}

	// Validate message ID
	if err := validateMessageID(inner.MsgID); err != nil {
		return err
	}

	// Store the auth key ID for error responses
	h.authKeyID = keyID

	sess, err := h.server.sessions.Get(keyID)
	if err != nil {
		sess, err = h.server.sessions.Create(keyID)
		if err != nil {
			return err
		}
	}
	if sess != nil {
		sess.Touch()
		_ = h.server.sessions.Save(sess)
	}

	ctx := h.conn.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	ctx = withSession(ctx, sess)
	ctx = withAuthKeyID(ctx, int64(keyID))
	if sess != nil {
		ctx = withLayer(ctx, sess.Layer)
		ctx = withUserID(ctx, sess.UserID)
	}

	// Check if this is a container message
	if len(inner.Data) >= 4 {
		constructorID := binary.LittleEndian.Uint32(inner.Data[:4])
		if constructorID == 0x73f1f8dc { // msg_container
			return h.handleContainerMessage(ctx, sess, authKey, keyID, inner)
		}
		// Check for resend/state requests
		if constructorID == 0x04deb57d || constructorID == 0x7da458c8 { // msgs_state_req or msg_resend_req
			// For now, just acknowledge these requests without full resend logic
			return h.sendAcknowledgment(authKey, keyID, inner.MsgID)
		}
	}

	// Read constructor ID
	if len(inner.Data) < 4 {
		return io.ErrUnexpectedEOF
	}
	constructorID := binary.LittleEndian.Uint32(inner.Data[:4])

	// Look up method handler directly by constructor ID
	methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
	if !ok {
		return NewNotFoundError("METHOD_NOT_FOUND")
	}

	// Decode the request only after confirming we have a handler
	constructor, ok := h.server.dispatcher.LookupConstructor(constructorID)
	if !ok {
		return NewNotFoundError("UNKNOWN_CONSTRUCTOR")
	}

	req := constructor()
	if deser, ok := req.(interface{ DeserializeTL(io.Reader) error }); ok {
		r := bytes.NewReader(inner.Data)
		// Skip constructor ID (already read)
		if _, err := mtproto.ReadUint32(r); err != nil {
			return err
		}
		if err := deser.DeserializeTL(r); err != nil {
			return err
		}
	}

	var resp interface{}

	// Apply new gRPC-like unary interceptors
	if len(h.server.unaryInterceptors) > 0 {
		info := &UnaryServerInfo{FullMethod: fmt.Sprintf("constructor_%08x", constructorID)}
		chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
		handler := func(ctx context.Context, req interface{}) (interface{}, error) {
			return methodHandler(ctx, req.(TLObject))
		}
		resp, err = chainedInterceptor(ctx, req, info, handler)
	} else {
		// Fallback to legacy interceptors
		if len(h.server.legacyInterceptors) > 0 {
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return methodHandler(ctx, req.(TLObject))
			}
			handler = ChainInterceptors(h.server.legacyInterceptors...)(handler)
			resp, err = handler(ctx, req)
		} else {
			resp, err = methodHandler(ctx, req)
		}
	}

	// Handle errors by sending MTProto RPC error
	if err != nil {
		return h.sendRPCError(inner.MsgID, err)
	}

	if resp == nil {
		return nil
	}
	respObj, ok := resp.(TLObject)
	if !ok {
		return h.sendRPCError(inner.MsgID, NewInternalError("response does not implement TLObject"))
	}
	respData, err := encodeTLObject(respObj)
	if err != nil {
		return h.sendRPCError(inner.MsgID, NewInternalError("failed to encode response"))
	}

	innerResp := &mtproto.InnerData{
		Salt:      inner.Salt,
		SessionID: inner.SessionID,
		MsgID:     nextMsgID(),
		SeqNo:     nextSeqNo(sess),
		Data:      respData,
	}
	encResp, err := innerResp.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}
	if err := h.conn.WriteMessage(serializeEncrypted(encResp)); err != nil {
		return err
	}

	// Send acknowledgment for the received message
	return h.sendAcknowledgment(authKey, keyID, inner.MsgID)
}

// handleContainerMessage processes a container of batched messages
func (h *connHandler) handleContainerMessage(ctx context.Context, sess *session.Session, authKey crypto.AuthKey, keyID crypto.KeyID, containerInner *mtproto.InnerData) error {
	r := bytes.NewReader(containerInner.Data[4:]) // Skip constructor

	// Read vector header
	vectorLen, err := mtproto.ReadUint32(r)
	if err != nil {
		return err
	}

	type containerResponse struct {
		msgID  int64
		seqNo  int32
		data   []byte
	}

	var responses []containerResponse
	var ackMsgIDs []int64

	// Process each message in the container
	for i := uint32(0); i < vectorLen; i++ {
		// Read message fields
		msgID, err := mtproto.ReadInt64(r)
		if err != nil {
			return err
		}
		seqNo, err := mtproto.ReadInt32(r)
		if err != nil {
			return err
		}
		msgData, err := mtproto.ReadBytes(r)
		if err != nil {
			return err
		}

		// Collect message ID for acknowledgment
		ackMsgIDs = append(ackMsgIDs, msgID)

		// Process the individual message
		if len(msgData) < 4 {
			continue // Skip invalid messages
		}
		constructorID := binary.LittleEndian.Uint32(msgData[:4])

		// Look up method handler directly by constructor ID
		methodHandler, ok := h.server.dispatcher.LookupMethod(constructorID)
		if !ok {
			continue // Skip unknown methods
		}

		// Decode the request
		constructor, ok := h.server.dispatcher.LookupConstructor(constructorID)
		if !ok {
			continue // Skip unknown constructors
		}

		req := constructor()
		if deser, ok := req.(interface{ DeserializeTL(io.Reader) error }); ok {
			r := bytes.NewReader(msgData)
			// Skip constructor ID (already read)
			if _, err := mtproto.ReadUint32(r); err != nil {
				continue
			}
			if err := deser.DeserializeTL(r); err != nil {
				continue
			}
		}

		var resp interface{}

		// Apply interceptors
		if len(h.server.unaryInterceptors) > 0 {
			info := &UnaryServerInfo{FullMethod: fmt.Sprintf("constructor_%08x", constructorID)}
			chainedInterceptor := ChainUnaryInterceptors(h.server.unaryInterceptors...)
			handler := func(ctx context.Context, req interface{}) (interface{}, error) {
				return methodHandler(ctx, req.(TLObject))
			}
			resp, err = chainedInterceptor(ctx, req, info, handler)
		} else {
			if len(h.server.legacyInterceptors) > 0 {
				handler := func(ctx context.Context, req interface{}) (interface{}, error) {
					return methodHandler(ctx, req.(TLObject))
				}
				handler = ChainInterceptors(h.server.legacyInterceptors...)(handler)
				resp, err = handler(ctx, req)
			} else {
				resp, err = methodHandler(ctx, req)
			}
		}

		if err != nil || resp == nil {
			continue // Skip failed messages or messages without responses
		}

		respObj, ok := resp.(TLObject)
		if !ok {
			continue // Skip invalid responses
		}

		respData, err := encodeTLObject(respObj)
		if err != nil {
			continue // Skip encoding errors
		}

		responses = append(responses, containerResponse{
			msgID: msgID,
			seqNo: seqNo,
			data:  respData,
		})
	}

	// Send responses
	if len(responses) == 1 {
		// Single response - send as regular message
		innerResp := &mtproto.InnerData{
			Salt:      containerInner.Salt,
			SessionID: containerInner.SessionID,
			MsgID:     nextMsgID(),
			SeqNo:     responses[0].seqNo,
			Data:      responses[0].data,
		}
		encResp, err := innerResp.Encrypt(authKey, keyID)
		if err != nil {
			return err
		}
		return h.conn.WriteMessage(serializeEncrypted(encResp))
	} else if len(responses) > 1 {
		// Multiple responses - send as container
		containerBuf := &bytes.Buffer{}

		// Write container constructor
		if err := mtproto.WriteUint32(containerBuf, 0x73f1f8dc); err != nil {
			return err
		}

		// Write vector header
		if err := mtproto.WriteVectorHeader(containerBuf, len(responses)); err != nil {
			return err
		}

		// Write each response
		for _, resp := range responses {
			if err := mtproto.WriteInt64(containerBuf, nextMsgID()); err != nil {
				return err
			}
			if err := mtproto.WriteInt32(containerBuf, resp.seqNo); err != nil {
				return err
			}
			if err := mtproto.WriteBytes(containerBuf, resp.data); err != nil {
				return err
			}
		}

		innerResp := &mtproto.InnerData{
			Salt:      containerInner.Salt,
			SessionID: containerInner.SessionID,
			MsgID:     nextMsgID(),
			SeqNo:     nextSeqNo(sess),
			Data:      containerBuf.Bytes(),
		}
		encResp, err := innerResp.Encrypt(authKey, keyID)
		if err != nil {
			return err
		}
		return h.conn.WriteMessage(serializeEncrypted(encResp))
	}

	// Send acknowledgments for all processed messages
	return h.sendAcknowledgment(authKey, keyID, ackMsgIDs...)
}

// sendAcknowledgment sends an acknowledgment for received message IDs
func (h *connHandler) sendAcknowledgment(authKey crypto.AuthKey, keyID crypto.KeyID, msgIDs ...int64) error {
	if len(msgIDs) == 0 {
		return nil
	}

	ackBuf := &bytes.Buffer{}
	if err := mtproto.WriteUint32(ackBuf, 0x62d6b459); err != nil { // msgs_ack
		return err
	}

	if err := mtproto.WriteVectorHeader(ackBuf, len(msgIDs)); err != nil {
		return err
	}

	for _, msgID := range msgIDs {
		if err := mtproto.WriteInt64(ackBuf, msgID); err != nil {
			return err
		}
	}

	innerAck := &mtproto.InnerData{
		Salt:      0, // Use 0 for acks
		SessionID: 0, // Use 0 for acks
		MsgID:     nextMsgID(),
		SeqNo:     0, // Acks don't need sequence numbers
		Data:      ackBuf.Bytes(),
	}

	encAck, err := innerAck.Encrypt(authKey, keyID)
	if err != nil {
		return err
	}

	return h.conn.WriteMessage(serializeEncrypted(encAck))
}

// sendRPCError converts an error to MTProto RPC error format and sends it.
func (h *connHandler) sendRPCError(requestMsgID int64, err error) error {
	// Convert error to RPCError
	rpcErr := FromError(err)

	// Encode the RPC error as TL object
	respData, encErr := encodeTLObject(rpcErr)
	if encErr != nil {
		// If encoding fails, send a generic internal error
		genericErr := NewInternalError("failed to encode error response")
		respData, _ = encodeTLObject(genericErr)
	}

	// Send the error response
	innerResp := &mtproto.InnerData{
		Salt:      0, // Use 0 for errors
		SessionID: requestMsgID &^ 3, // Use request msg_id as session_id for errors
		MsgID:     nextMsgID(),
		SeqNo:     0, // Errors don't need sequence numbers
		Data:      respData,
	}

	authKey, keyErr := h.server.authKeys.Get(h.authKeyID)
	if keyErr != nil {
		return keyErr
	}

	encResp, encErr := innerResp.Encrypt(authKey, h.authKeyID)
	if encErr != nil {
		return encErr
	}

	return h.conn.WriteMessage(serializeEncrypted(encResp))
}
