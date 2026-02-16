package tlrpc

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"math/big"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
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
	now := time.Now().Unix() // Unix timestamp in seconds
	msgTime := msgID >> 32 // extract timestamp from high 32 bits

	timeDiff := now - msgTime
	if timeDiff < -30 || timeDiff > 30 { // ±30 seconds
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
