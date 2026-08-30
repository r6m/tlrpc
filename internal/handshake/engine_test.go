package handshake

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"sync"
	"testing"
	"time"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

var (
	sharedServerKeyOnce sync.Once
	sharedServerKey     *crypto.ServerKey
	sharedServerKeyErr  error
)

func TestIndependentClients(t *testing.T) {
	engine, authKeys := newTestEngine(t, 4, time.Minute, time.Now)
	first, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new first session: %v", err)
	}
	second, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new second session: %v", err)
	}

	firstResult := completeHandshake(t, first, 1)
	secondResult := completeHandshake(t, second, 2)
	if firstResult.AuthKeyID == secondResult.AuthKeyID {
		t.Fatal("independent clients produced the same auth key ID")
	}
	if _, err := authKeys.Get(firstResult.AuthKeyID); err != nil {
		t.Fatalf("first auth key was not stored: %v", err)
	}
	if _, err := authKeys.Get(secondResult.AuthKeyID); err != nil {
		t.Fatalf("second auth key was not stored: %v", err)
	}
}

func TestSessionExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	engine, _ := newTestEngine(t, 1, time.Minute, func() time.Time { return now })
	session, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	if _, err := session.Handle(context.Background(), 0, encodeReqPQ([16]byte{1})); err != nil {
		t.Fatalf("req_pq: %v", err)
	}

	now = now.Add(time.Minute)
	if _, err := session.Handle(context.Background(), 0, encodeReqPQ([16]byte{1})); !errors.Is(err, ErrExpired) {
		t.Fatalf("expired session error = %v, want %v", err, ErrExpired)
	}
	if _, err := engine.NewSession(); err != nil {
		t.Fatalf("expired session did not release capacity: %v", err)
	}
}

func TestEngineCapacity(t *testing.T) {
	engine, _ := newTestEngine(t, 1, time.Minute, time.Now)
	first, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new first session: %v", err)
	}
	if _, err := engine.NewSession(); !errors.Is(err, ErrCapacity) {
		t.Fatalf("second session error = %v, want %v", err, ErrCapacity)
	}
	first.Close()
	if _, err := engine.NewSession(); err != nil {
		t.Fatalf("closed session did not release capacity: %v", err)
	}
}

func TestFinalDHStateIsOneShot(t *testing.T) {
	engine, _ := newTestEngine(t, 1, time.Minute, time.Now)
	session, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	client := advanceToClientDH(t, session, 3)

	malformed := encodeSetClientDH(client.nonce, client.serverNonce, bytes.Repeat([]byte{0x42}, 15))
	assertNoPanic(t, func() {
		if _, err := session.Handle(context.Background(), 0, malformed); !errors.Is(err, ErrInvalidHandshake) {
			t.Fatalf("misaligned ciphertext error = %v, want %v", err, ErrInvalidHandshake)
		}
	})
	if _, err := session.Handle(context.Background(), 0, client.finalRequest); !errors.Is(err, ErrClosed) {
		t.Fatalf("reused final DH state error = %v, want %v", err, ErrClosed)
	}
}

func TestMalformedInputNeverPanics(t *testing.T) {
	engine, _ := newTestEngine(t, 16, time.Minute, time.Now)
	inputs := [][]byte{
		nil,
		{1, 2, 3},
		{0xff, 0xff, 0xff, 0xff},
		append([]byte{0x78, 0x97, 0x46, 0x60}, bytes.Repeat([]byte{1}, 15)...),
		bytes.Repeat([]byte{0}, maxHandshakePayloadBytes+1),
	}
	for i, input := range inputs {
		session, err := engine.NewSession()
		if err != nil {
			t.Fatalf("case %d new session: %v", i, err)
		}
		assertNoPanic(t, func() {
			if _, err := session.Handle(context.Background(), 0, input); err == nil {
				t.Fatalf("case %d accepted malformed input", i)
			}
		})
		session.Close()
	}

	session, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new staged session: %v", err)
	}
	pqOutput, err := session.Handle(context.Background(), 0, encodeReqPQ([16]byte{9}))
	if err != nil {
		t.Fatalf("stage req_pq: %v", err)
	}
	_, serverNonce, _, _ := decodeResPQ(t, pqOutput.Response)
	badReqDH := &bytes.Buffer{}
	_ = mtproto.WriteUint32(badReqDH, reqDHParamsID)
	_ = mtproto.WriteInt128(badReqDH, [16]byte{9})
	_ = mtproto.WriteInt128(badReqDH, serverNonce)
	_ = mtproto.WriteBytes(badReqDH, []byte{3})
	assertNoPanic(t, func() {
		if _, err := session.Handle(context.Background(), 0, badReqDH.Bytes()); err == nil {
			t.Fatal("accepted truncated req_DH_params")
		}
	})
}

func TestAuthorizationResultCorrectness(t *testing.T) {
	engine, authKeys := newTestEngine(t, 1, time.Minute, time.Now)
	session, err := engine.NewSession()
	if err != nil {
		t.Fatalf("new session: %v", err)
	}
	client := advanceToClientDH(t, session, 7)
	output, err := session.Handle(context.Background(), 0, client.finalRequest)
	if err != nil {
		t.Fatalf("set_client_DH_params: %v", err)
	}
	if output.Result == nil {
		t.Fatal("authorization completed without Result")
	}
	if output.Result.AuthKeyID != client.expectedKey.ID() {
		t.Fatalf("auth key ID = %d, want %d", output.Result.AuthKeyID, client.expectedKey.ID())
	}
	wantSalt := computeServerSalt(client.newNonce, client.serverNonce)
	if output.Result.InitialServerSalt != wantSalt {
		t.Fatalf("server salt = %d, want %d", output.Result.InitialServerSalt, wantSalt)
	}
	stored, err := authKeys.Get(output.Result.AuthKeyID)
	if err != nil {
		t.Fatalf("get stored auth key: %v", err)
	}
	if !stored.Equal(client.expectedKey) {
		t.Fatal("stored auth key does not match the client-derived key")
	}
	verifyDHGenOK(t, output.Response, client, output.Result.AuthKeyID)
	if _, err := session.Handle(context.Background(), 0, client.finalRequest); !errors.Is(err, ErrClosed) {
		t.Fatalf("completed session reuse error = %v, want %v", err, ErrClosed)
	}
}

type deterministicReader struct {
	seed    byte
	counter uint64
	buffer  []byte
}

func (r *deterministicReader) Read(dst []byte) (int, error) {
	written := 0
	for written < len(dst) {
		if len(r.buffer) == 0 {
			var input [9]byte
			input[0] = r.seed
			binary.LittleEndian.PutUint64(input[1:], r.counter)
			r.counter++
			hash := sha256.Sum256(input[:])
			r.buffer = append(r.buffer[:0], hash[:]...)
		}
		n := copy(dst[written:], r.buffer)
		written += n
		r.buffer = r.buffer[n:]
	}
	return written, nil
}

func newTestEngine(t *testing.T, capacity int, ttl time.Duration, clock func() time.Time) (*Engine, *crypto.MemoryAuthKeyManager) {
	t.Helper()
	authKeys := crypto.NewMemoryAuthKeyManager()
	serverKeys := crypto.NewMemoryServerKeyManager()
	serverKeys.AddKey(testServerKey(t))
	engine, err := New(Config{
		AuthKeys:   authKeys,
		ServerKeys: serverKeys,
		Capacity:   capacity,
		TTL:        ttl,
		Clock:      clock,
		Random:     &deterministicReader{seed: byte(capacity)},
	})
	if err != nil {
		t.Fatalf("new engine: %v", err)
	}
	return engine, authKeys
}

func testServerKey(t *testing.T) *crypto.ServerKey {
	t.Helper()
	sharedServerKeyOnce.Do(func() {
		sharedServerKey, sharedServerKeyErr = crypto.GenerateServerKey()
	})
	if sharedServerKeyErr != nil {
		t.Fatalf("generate server key: %v", sharedServerKeyErr)
	}
	return sharedServerKey
}

type clientDHState struct {
	nonce        [16]byte
	serverNonce  [16]byte
	newNonce     [32]byte
	expectedKey  crypto.AuthKey
	finalRequest []byte
}

func completeHandshake(t *testing.T, session *Session, seed byte) *Result {
	t.Helper()
	client := advanceToClientDH(t, session, seed)
	output, err := session.Handle(context.Background(), 0, client.finalRequest)
	if err != nil {
		t.Fatalf("complete handshake: %v", err)
	}
	if output.Result == nil {
		t.Fatal("complete handshake returned nil result")
	}
	return output.Result
}

func advanceToClientDH(t *testing.T, session *Session, seed byte) clientDHState {
	t.Helper()
	var nonce [16]byte
	var newNonce [32]byte
	for i := range nonce {
		nonce[i] = seed + byte(i)
	}
	for i := range newNonce {
		newNonce[i] = seed + byte(i*3)
	}

	pqOutput, err := session.Handle(context.Background(), 0, encodeReqPQ(nonce))
	if err != nil {
		t.Fatalf("req_pq: %v", err)
	}
	gotNonce, serverNonce, pq, fingerprint := decodeResPQ(t, pqOutput.Response)
	if gotNonce != nonce {
		t.Fatal("resPQ nonce mismatch")
	}
	factors, err := crypto.FactorizePQ(new(big.Int).SetBytes(pq))
	if err != nil {
		t.Fatalf("factor pq: %v", err)
	}
	p := factors.P.Bytes()
	q := factors.Q.Bytes()

	inner := &bytes.Buffer{}
	_ = mtproto.WriteUint32(inner, pqInnerDataID)
	_ = mtproto.WriteBytes(inner, pq)
	_ = mtproto.WriteBytes(inner, p)
	_ = mtproto.WriteBytes(inner, q)
	_ = mtproto.WriteInt128(inner, nonce)
	_ = mtproto.WriteInt128(inner, serverNonce)
	_ = mtproto.WriteInt256(inner, newNonce)
	encryptedPQ, err := rsa.EncryptPKCS1v15(crand.Reader, &testServerKey(t).Key.PublicKey, inner.Bytes())
	if err != nil {
		t.Fatalf("encrypt p_q_inner_data: %v", err)
	}
	reqDH := &bytes.Buffer{}
	_ = mtproto.WriteUint32(reqDH, reqDHParamsID)
	_ = mtproto.WriteInt128(reqDH, nonce)
	_ = mtproto.WriteInt128(reqDH, serverNonce)
	_ = mtproto.WriteBytes(reqDH, p)
	_ = mtproto.WriteBytes(reqDH, q)
	_ = mtproto.WriteInt64(reqDH, fingerprint)
	_ = mtproto.WriteBytes(reqDH, encryptedPQ)

	dhOutput, err := session.Handle(context.Background(), 0, reqDH.Bytes())
	if err != nil {
		t.Fatalf("req_DH_params: %v", err)
	}
	ga := decodeServerDH(t, dhOutput.Response, nonce, serverNonce, newNonce)
	b := new(big.Int).SetBytes(bytes.Repeat([]byte{seed + 0x40}, 256))
	gb := new(big.Int).Exp(crypto.DHGenerator, b, crypto.DHPrime)
	authKeyBig := new(big.Int).Exp(ga, b, crypto.DHPrime)
	var expectedKey crypto.AuthKey
	copy(expectedKey[:], leftPad256(authKeyBig))

	clientInner := &bytes.Buffer{}
	_ = mtproto.WriteUint32(clientInner, clientDHInnerDataID)
	_ = mtproto.WriteInt128(clientInner, nonce)
	_ = mtproto.WriteInt128(clientInner, serverNonce)
	_ = mtproto.WriteInt64(clientInner, 0)
	_ = mtproto.WriteBytes(clientInner, gb.Bytes())
	clientPlain := withSHA1AndPadding(clientInner.Bytes())
	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)
	encryptedClient := make([]byte, len(clientPlain))
	crypto.NewAESIGE(tempKey, tempIV).CryptBlocks(encryptedClient, clientPlain)

	return clientDHState{
		nonce:        nonce,
		serverNonce:  serverNonce,
		newNonce:     newNonce,
		expectedKey:  expectedKey,
		finalRequest: encodeSetClientDH(nonce, serverNonce, encryptedClient),
	}
}

func encodeReqPQ(nonce [16]byte) []byte {
	buffer := &bytes.Buffer{}
	_ = mtproto.WriteUint32(buffer, reqPQMultiID)
	_ = mtproto.WriteInt128(buffer, nonce)
	return buffer.Bytes()
}

func decodeResPQ(t *testing.T, data []byte) ([16]byte, [16]byte, []byte, int64) {
	t.Helper()
	r := bytes.NewReader(data)
	constructor, err := mtproto.ReadUint32(r)
	if err != nil || constructor != resPQID {
		t.Fatalf("resPQ constructor = %08x, err %v", constructor, err)
	}
	nonce, err := mtproto.ReadInt128(r)
	if err != nil {
		t.Fatalf("read resPQ nonce: %v", err)
	}
	serverNonce, err := mtproto.ReadInt128(r)
	if err != nil {
		t.Fatalf("read resPQ server nonce: %v", err)
	}
	pq, err := mtproto.ReadBytes(r)
	if err != nil {
		t.Fatalf("read resPQ pq: %v", err)
	}
	vectorID, err := mtproto.ReadUint32(r)
	if err != nil || vectorID != mtproto.VectorConstructorID {
		t.Fatalf("fingerprint vector = %08x, err %v", vectorID, err)
	}
	count, err := mtproto.ReadInt32(r)
	if err != nil || count < 1 {
		t.Fatalf("fingerprint count = %d, err %v", count, err)
	}
	fingerprint, err := mtproto.ReadInt64(r)
	if err != nil {
		t.Fatalf("read fingerprint: %v", err)
	}
	return nonce, serverNonce, pq, fingerprint
}

func decodeServerDH(t *testing.T, data []byte, nonce, serverNonce [16]byte, newNonce [32]byte) *big.Int {
	t.Helper()
	r := bytes.NewReader(data)
	constructor, err := mtproto.ReadUint32(r)
	if err != nil || constructor != serverDHParamsOKID {
		t.Fatalf("server_DH_params constructor = %08x, err %v", constructor, err)
	}
	gotNonce, _ := mtproto.ReadInt128(r)
	gotServerNonce, _ := mtproto.ReadInt128(r)
	if gotNonce != nonce || gotServerNonce != serverNonce {
		t.Fatal("server_DH_params nonce mismatch")
	}
	encrypted, err := mtproto.ReadBytes(r)
	if err != nil {
		t.Fatalf("read encrypted_answer: %v", err)
	}
	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)
	plain := make([]byte, len(encrypted))
	crypto.NewAESIGEDecrypt(tempKey, tempIV).CryptBlocks(plain, encrypted)
	inner := bytes.NewReader(plain[sha1.Size:])
	innerConstructor, _ := mtproto.ReadUint32(inner)
	innerNonce, _ := mtproto.ReadInt128(inner)
	innerServerNonce, _ := mtproto.ReadInt128(inner)
	g, _ := mtproto.ReadUint32(inner)
	prime, err := mtproto.ReadBytes(inner)
	if err != nil {
		t.Fatalf("read dh prime: %v", err)
	}
	ga, err := mtproto.ReadBytes(inner)
	if err != nil {
		t.Fatalf("read g_a: %v", err)
	}
	_, _ = mtproto.ReadUint32(inner)
	consumed := len(plain[sha1.Size:]) - inner.Len()
	hash := sha1.Sum(plain[sha1.Size : sha1.Size+consumed])
	if !bytes.Equal(plain[:sha1.Size], hash[:]) {
		t.Fatal("server_DH_inner_data hash mismatch")
	}
	if innerConstructor != serverDHInnerDataID || innerNonce != nonce || innerServerNonce != serverNonce || g != handshakeGenerator {
		t.Fatal("invalid server_DH_inner_data metadata")
	}
	if new(big.Int).SetBytes(prime).Cmp(crypto.DHPrime) != 0 {
		t.Fatal("server returned unexpected DH prime")
	}
	return new(big.Int).SetBytes(ga)
}

func withSHA1AndPadding(data []byte) []byte {
	hash := sha1.Sum(data)
	result := append(append([]byte{}, hash[:]...), data...)
	if remainder := len(result) % 16; remainder != 0 {
		result = append(result, make([]byte, 16-remainder)...)
	}
	return result
}

func encodeSetClientDH(nonce, serverNonce [16]byte, encrypted []byte) []byte {
	buffer := &bytes.Buffer{}
	_ = mtproto.WriteUint32(buffer, setClientDHParamsID)
	_ = mtproto.WriteInt128(buffer, nonce)
	_ = mtproto.WriteInt128(buffer, serverNonce)
	_ = mtproto.WriteBytes(buffer, encrypted)
	return buffer.Bytes()
}

func verifyDHGenOK(t *testing.T, data []byte, client clientDHState, keyID crypto.KeyID) {
	t.Helper()
	r := bytes.NewReader(data)
	constructor, err := mtproto.ReadUint32(r)
	if err != nil || constructor != dhGenOKID {
		t.Fatalf("dh_gen_ok constructor = %08x, err %v", constructor, err)
	}
	nonce, _ := mtproto.ReadInt128(r)
	serverNonce, _ := mtproto.ReadInt128(r)
	nonceHash, err := mtproto.ReadInt128(r)
	if err != nil || r.Len() != 0 {
		t.Fatalf("read dh_gen_ok hash: %v", err)
	}
	if nonce != client.nonce || serverNonce != client.serverNonce {
		t.Fatal("dh_gen_ok nonce mismatch")
	}
	wantHash := crypto.ComputeNewNonceHash1Auth(client.newNonce, client.expectedKey[:])
	if nonceHash != wantHash {
		t.Fatalf("new_nonce_hash1 mismatch for key %d", keyID)
	}
}

func assertNoPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recovered := recover(); recovered != nil {
			t.Fatalf("panicked: %v", recovered)
		}
	}()
	fn()
}

var _ io.Reader = (*deterministicReader)(nil)
