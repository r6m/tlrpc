package handshake

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"sync"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

const (
	reqPQID                  = uint32(0x60469778)
	reqPQMultiID             = uint32(0xbe7e8ef1)
	resPQID                  = uint32(0x05162463)
	reqDHParamsID            = uint32(0xd712e4be)
	serverDHParamsOKID       = uint32(0xd0e8075c)
	setClientDHParamsID      = uint32(0xf5045f1f)
	dhGenOKID                = uint32(0x3bcbf734)
	dhGenRetryID             = uint32(0x46dc1fb9)
	pqInnerDataID            = uint32(0x83c95aec)
	pqInnerDataDCID          = uint32(0xa9f55f95)
	pqInnerDataTempDCID      = uint32(0x56fddf88)
	serverDHInnerDataID      = uint32(0xb5890dba)
	clientDHInnerDataID      = uint32(0x6643b654)
	handshakeGenerator       = uint32(3)
	maxHandshakePayloadBytes = 4096
)

const advertisedPQ = "\x17\xED\x48\x94\x1A\x08\xF9\x81"

type stage uint8

const (
	stageFresh stage = iota
	stagePQIssued
	stageDHIssued
	stageClosed
)

// Result is emitted exactly once when authorization succeeds.
type Result struct {
	AuthKeyID         crypto.KeyID
	InitialServerSalt int64
}

// Output contains the unencrypted MTProto response and, only for dh_gen_ok,
// the completed authorization result consumed by the connection runtime.
type Output struct {
	Response []byte
	Result   *Result
}

type dhState struct {
	newNonce [32]byte
	privateA *big.Int
	tempKey  []byte
	tempIV   []byte
	retryID  int64
}

// Session contains all mutable handshake state for exactly one accepted
// connection. A Session cannot be transferred between connections or reused
// after authorization, Close, expiry, or a terminal protocol failure.
type Session struct {
	engine *Engine

	mu          sync.Mutex
	stage       stage
	nonce       [16]byte
	serverNonce [16]byte
	dh          *dhState
}

// Close releases the engine capacity held by the session. It is idempotent.
func (s *Session) Close() {
	s.mu.Lock()
	if s.stage != stageClosed {
		s.stage = stageClosed
		s.dh = nil
	}
	s.mu.Unlock()
	s.engine.remove(s)
}

// Handle processes one unencrypted MTProto handshake body. messageID is kept
// in the contract for the connection runtime; message-ID validation belongs to
// the encrypted protocol validator rather than this handshake state machine.
func (s *Session) Handle(ctx context.Context, messageID int64, data []byte) (Output, error) {
	_ = messageID
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if len(data) < 4 || len(data) > maxHandshakePayloadBytes {
		return Output{}, ErrInvalidHandshake
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stage == stageClosed {
		return Output{}, ErrClosed
	}
	if err := s.engine.check(s); err != nil {
		s.stage = stageClosed
		s.dh = nil
		return Output{}, err
	}

	constructorID := binary.LittleEndian.Uint32(data[:4])
	var output Output
	var err error
	switch constructorID {
	case reqPQID, reqPQMultiID:
		if s.stage != stageFresh {
			err = ErrInvalidHandshake
			break
		}
		output.Response, err = s.handleReqPQ(data)
	case reqDHParamsID:
		if s.stage != stagePQIssued {
			err = ErrInvalidHandshake
			break
		}
		output.Response, err = s.handleReqDHParams(data)
	case setClientDHParamsID:
		if s.stage != stageDHIssued {
			err = ErrInvalidHandshake
			break
		}
		// A malformed final attempt is terminal. A valid dh_gen_retry response
		// explicitly restores the issued state from handleSetClientDHParams.
		dh := s.dh
		s.dh = nil
		s.stage = stageClosed
		output, err = s.handleSetClientDHParams(data, dh)
	default:
		err = ErrUnsupportedMessage
	}

	if err != nil {
		// Once a client has advanced beyond req_pq, a malformed transition is
		// terminal. This prevents cryptographic state from becoming retryable.
		if s.stage != stageFresh {
			s.stage = stageClosed
			s.dh = nil
			s.engine.remove(s)
		}
		return Output{}, normalizeError(err)
	}
	if output.Result != nil || s.stage == stageClosed {
		s.engine.remove(s)
	} else {
		s.engine.refresh(s)
	}
	return output, nil
}

func (s *Session) handleReqPQ(data []byte) ([]byte, error) {
	r := bytes.NewReader(data[4:])
	nonce, err := mtproto.ReadInt128(r)
	if err != nil || r.Len() != 0 {
		return nil, ErrInvalidHandshake
	}
	var serverNonce [16]byte
	if err := s.engine.readRandom(serverNonce[:]); err != nil {
		return nil, fmt.Errorf("generate server nonce: %w", err)
	}
	keys, err := s.engine.serverKeys.GetAllKeys()
	if err != nil {
		return nil, err
	}
	if len(keys) == 0 {
		return nil, ErrInvalidConfig
	}

	response := &bytes.Buffer{}
	if err := mtproto.WriteUint32(response, resPQID); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(response, []byte(advertisedPQ)); err != nil {
		return nil, err
	}
	if err := mtproto.WriteVectorHeader(response, len(keys)); err != nil {
		return nil, err
	}
	for _, key := range keys {
		if key == nil || key.Key == nil {
			return nil, ErrInvalidConfig
		}
		if err := mtproto.WriteInt64(response, key.ID); err != nil {
			return nil, err
		}
	}

	s.nonce = nonce
	s.serverNonce = serverNonce
	s.stage = stagePQIssued
	return response.Bytes(), nil
}

func (s *Session) handleReqDHParams(data []byte) ([]byte, error) {
	r := bytes.NewReader(data[4:])
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
	fingerprint, err := mtproto.ReadInt64(r)
	if err != nil {
		return nil, err
	}
	encrypted, err := mtproto.ReadBytes(r)
	if err != nil || r.Len() != 0 {
		return nil, ErrInvalidHandshake
	}
	if nonce != s.nonce || serverNonce != s.serverNonce {
		return nil, ErrInvalidHandshake
	}
	if err := validatePQFactors(p, q, []byte(advertisedPQ)); err != nil {
		return nil, err
	}

	var fingerprintBytes [8]byte
	binary.LittleEndian.PutUint64(fingerprintBytes[:], uint64(fingerprint))
	serverKey, err := s.engine.serverKeys.GetKey(fingerprintBytes[:])
	if err != nil {
		return nil, err
	}
	if serverKey == nil || serverKey.Key == nil {
		return nil, ErrInvalidConfig
	}
	decrypted, err := crypto.DecryptRSA(serverKey.Key, encrypted)
	if err != nil {
		return nil, err
	}
	inner, err := parsePQInnerData(decrypted)
	if err != nil {
		return nil, err
	}
	if inner.nonce != nonce || inner.serverNonce != serverNonce ||
		!equalBigEndian(inner.p, p) || !equalBigEndian(inner.q, q) ||
		!equalBigEndian(inner.pq, []byte(advertisedPQ)) {
		return nil, ErrInvalidHandshake
	}
	if err := validatePQFactors(inner.p, inner.q, inner.pq); err != nil {
		return nil, err
	}

	privateA, err := s.generatePrivateExponent()
	if err != nil {
		return nil, err
	}
	ga := new(big.Int).Exp(crypto.DHGenerator, privateA, crypto.DHPrime)
	if err := validateDHPublicValue(ga, crypto.DHPrime); err != nil {
		return nil, err
	}

	plain := &bytes.Buffer{}
	if err := mtproto.WriteUint32(plain, serverDHInnerDataID); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(plain, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(plain, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteUint32(plain, handshakeGenerator); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(plain, crypto.DHPrime.Bytes()); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(plain, ga.Bytes()); err != nil {
		return nil, err
	}
	if err := mtproto.WriteUint32(plain, uint32(s.engine.now().Unix())); err != nil {
		return nil, err
	}

	encryptedDH, tempKey, tempIV, err := s.encryptServerDH(inner.newNonce, serverNonce, plain.Bytes())
	if err != nil {
		return nil, err
	}
	response := &bytes.Buffer{}
	if err := mtproto.WriteUint32(response, serverDHParamsOKID); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteBytes(response, encryptedDH); err != nil {
		return nil, err
	}

	s.dh = &dhState{newNonce: inner.newNonce, privateA: privateA, tempKey: tempKey, tempIV: tempIV}
	s.stage = stageDHIssued
	return response.Bytes(), nil
}

func (s *Session) handleSetClientDHParams(data []byte, dh *dhState) (Output, error) {
	if dh == nil {
		return Output{}, ErrInvalidHandshake
	}
	r := bytes.NewReader(data[4:])
	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return Output{}, err
	}
	serverNonce, err := mtproto.ReadInt128(r)
	if err != nil {
		return Output{}, err
	}
	encrypted, err := mtproto.ReadBytes(r)
	if err != nil || r.Len() != 0 || len(encrypted) == 0 || len(encrypted)%16 != 0 {
		return Output{}, ErrInvalidHandshake
	}
	if nonce != s.nonce || serverNonce != s.serverNonce {
		return Output{}, ErrInvalidHandshake
	}

	decrypted := make([]byte, len(encrypted))
	crypto.NewAESIGEDecrypt(dh.tempKey, dh.tempIV).CryptBlocks(decrypted, encrypted)
	inner, err := parseClientDHInnerData(decrypted)
	if err != nil {
		return Output{}, err
	}
	if inner.nonce != nonce || inner.serverNonce != serverNonce {
		return Output{}, ErrInvalidHandshake
	}
	if inner.retryID != dh.retryID {
		return Output{}, ErrInvalidHandshake
	}
	gb := new(big.Int).SetBytes(inner.gb)
	if err := validateDHPublicValue(gb, crypto.DHPrime); err != nil {
		return Output{}, err
	}

	authKeyBig := new(big.Int).Exp(gb, dh.privateA, crypto.DHPrime)
	authKeyBytes := authKeyBig.Bytes()
	if len(authKeyBytes) != len(crypto.AuthKey{}) {
		authHash := sha1.Sum(authKeyBytes)
		dh.retryID = int64(binary.LittleEndian.Uint64(authHash[:8]))
		response, err := encodeDHGenResponse(
			dhGenRetryID,
			nonce,
			serverNonce,
			crypto.ComputeNewNonceHash2Auth(dh.newNonce, authKeyBytes),
		)
		if err != nil {
			return Output{}, err
		}
		// MTProto auth keys are exactly 2048 bits. Clients retry with a new
		// exponent when the minimal big-endian shared secret is shorter.
		s.dh = dh
		s.stage = stageDHIssued
		return Output{Response: response}, nil
	}
	var authKey crypto.AuthKey
	copy(authKey[:], authKeyBytes)
	authKeyID := authKey.ID()
	if err := s.engine.authKeys.Put(authKeyID, authKey); err != nil {
		return Output{}, err
	}
	serverSalt := computeServerSalt(dh.newNonce, serverNonce)

	nonceHash := crypto.ComputeNewNonceHash1Auth(dh.newNonce, authKeyBytes)
	response, err := encodeDHGenResponse(dhGenOKID, nonce, serverNonce, nonceHash)
	if err != nil {
		return Output{}, err
	}
	return Output{
		Response: response,
		Result: &Result{
			AuthKeyID:         authKeyID,
			InitialServerSalt: serverSalt,
		},
	}, nil
}

func encodeDHGenResponse(constructor uint32, nonce, serverNonce, nonceHash [16]byte) ([]byte, error) {
	response := &bytes.Buffer{}
	if err := mtproto.WriteUint32(response, constructor); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, nonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, serverNonce); err != nil {
		return nil, err
	}
	if err := mtproto.WriteInt128(response, nonceHash); err != nil {
		return nil, err
	}
	return response.Bytes(), nil
}

func (s *Session) generatePrivateExponent() (*big.Int, error) {
	s.engine.randomMu.Lock()
	defer s.engine.randomMu.Unlock()
	for range 32 {
		a, err := crand.Int(s.engine.random, crypto.DHPrime)
		if err != nil {
			return nil, err
		}
		if a.Cmp(big.NewInt(2)) >= 0 {
			return a, nil
		}
	}
	return nil, errors.New("handshake: randomness produced invalid DH exponent")
}

func (s *Session) encryptServerDH(newNonce [32]byte, serverNonce [16]byte, plain []byte) ([]byte, []byte, []byte, error) {
	hash := sha1.Sum(plain)
	padded := make([]byte, 20+len(plain))
	copy(padded, hash[:])
	copy(padded[20:], plain)
	if remainder := len(padded) % 16; remainder != 0 {
		padding := make([]byte, 16-remainder)
		if err := s.engine.readRandom(padding); err != nil {
			return nil, nil, nil, err
		}
		padded = append(padded, padding...)
	}
	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)
	encrypted := make([]byte, len(padded))
	crypto.NewAESIGE(tempKey, tempIV).CryptBlocks(encrypted, padded)
	return encrypted, tempKey, tempIV, nil
}

type pqInnerData struct {
	pq          []byte
	p           []byte
	q           []byte
	nonce       [16]byte
	serverNonce [16]byte
	newNonce    [32]byte
}

func parsePQInnerData(data []byte) (pqInnerData, error) {
	for _, offset := range []int{0, sha1.Size} {
		if len(data) < offset+4 {
			continue
		}
		r := bytes.NewReader(data[offset:])
		constructor, err := mtproto.ReadUint32(r)
		if err != nil || (constructor != pqInnerDataID && constructor != pqInnerDataDCID && constructor != pqInnerDataTempDCID) {
			continue
		}
		var inner pqInnerData
		if inner.pq, err = mtproto.ReadBytes(r); err != nil {
			continue
		}
		if inner.p, err = mtproto.ReadBytes(r); err != nil {
			continue
		}
		if inner.q, err = mtproto.ReadBytes(r); err != nil {
			continue
		}
		if inner.nonce, err = mtproto.ReadInt128(r); err != nil {
			continue
		}
		if inner.serverNonce, err = mtproto.ReadInt128(r); err != nil {
			continue
		}
		if inner.newNonce, err = mtproto.ReadInt256(r); err != nil {
			continue
		}
		if constructor == pqInnerDataDCID {
			if _, err = mtproto.ReadInt32(r); err != nil {
				continue
			}
		}
		if constructor == pqInnerDataTempDCID {
			if _, err = mtproto.ReadInt32(r); err != nil {
				continue
			}
			if _, err = mtproto.ReadInt32(r); err != nil {
				continue
			}
		}
		consumed := len(data[offset:]) - r.Len()
		if offset == sha1.Size {
			hash := sha1.Sum(data[offset : offset+consumed])
			if !bytes.Equal(data[:sha1.Size], hash[:]) {
				continue
			}
		}
		return inner, nil
	}
	return pqInnerData{}, ErrInvalidHandshake
}

type clientDHInnerData struct {
	nonce       [16]byte
	serverNonce [16]byte
	retryID     int64
	gb          []byte
}

func parseClientDHInnerData(data []byte) (clientDHInnerData, error) {
	for _, offset := range []int{sha1.Size, 0} {
		if len(data) < offset+4 {
			continue
		}
		r := bytes.NewReader(data[offset:])
		constructor, err := mtproto.ReadUint32(r)
		if err != nil || constructor != clientDHInnerDataID {
			continue
		}
		var inner clientDHInnerData
		if inner.nonce, err = mtproto.ReadInt128(r); err != nil {
			continue
		}
		if inner.serverNonce, err = mtproto.ReadInt128(r); err != nil {
			continue
		}
		if inner.retryID, err = mtproto.ReadInt64(r); err != nil {
			continue
		}
		if inner.gb, err = mtproto.ReadBytes(r); err != nil {
			continue
		}
		consumed := len(data[offset:]) - r.Len()
		if offset == sha1.Size {
			hash := sha1.Sum(data[offset : offset+consumed])
			if !bytes.Equal(data[:sha1.Size], hash[:]) {
				continue
			}
		}
		return inner, nil
	}
	return clientDHInnerData{}, ErrInvalidHandshake
}

func validatePQFactors(pBytes, qBytes, pqBytes []byte) error {
	p := new(big.Int).SetBytes(pBytes)
	q := new(big.Int).SetBytes(qBytes)
	pq := new(big.Int).SetBytes(pqBytes)
	if p.Cmp(big.NewInt(2)) < 0 || q.Cmp(big.NewInt(2)) < 0 || !p.ProbablyPrime(32) || !q.ProbablyPrime(32) {
		return ErrInvalidHandshake
	}
	if new(big.Int).Mul(new(big.Int).Set(p), q).Cmp(pq) != 0 {
		return ErrInvalidHandshake
	}
	return nil
}

func validateDHPublicValue(value, prime *big.Int) error {
	if value == nil || prime == nil {
		return ErrInvalidHandshake
	}
	margin := new(big.Int).Lsh(big.NewInt(1), 2048-64)
	upper := new(big.Int).Sub(prime, margin)
	if value.Cmp(margin) <= 0 || value.Cmp(upper) >= 0 {
		return ErrInvalidHandshake
	}
	return nil
}

func equalBigEndian(a, b []byte) bool {
	return new(big.Int).SetBytes(a).Cmp(new(big.Int).SetBytes(b)) == 0
}

func computeServerSalt(newNonce [32]byte, serverNonce [16]byte) int64 {
	return int64(binary.LittleEndian.Uint64(newNonce[:8]) ^ binary.LittleEndian.Uint64(serverNonce[:8]))
}

func normalizeError(err error) error {
	if err == nil || errors.Is(err, ErrInvalidHandshake) || errors.Is(err, ErrUnsupportedMessage) ||
		errors.Is(err, ErrExpired) || errors.Is(err, ErrClosed) || errors.Is(err, ErrInvalidConfig) {
		return err
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, mtproto.ErrInvalidMessageLength) {
		return ErrInvalidHandshake
	}
	return err
}
