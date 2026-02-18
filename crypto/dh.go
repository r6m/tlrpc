package crypto

import (
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
)

var (
	ErrInvalidDH    = errors.New("crypto: invalid DH parameters")
	ErrInvalidNonce = errors.New("crypto: invalid nonce")
	ErrInvalidProof = errors.New("crypto: invalid proof")
)

var (
	DHPrime     = mustParseBig("c71caeb9c6b1c9048e6c522f70f13f73980d40238e3e21c14934d037563d930f48198a0aa7c14058229493d22530f4dbfa336f6e0ac925139543aed44cce7c3720fd51f69458705ac68cd4fe6b6b13abdc9746512969328454f18faf8c595f642477fe96bb2a941d5bcd1d4ac8cc49880708fa9b378e3c4f3a9060bee67cf9a4a4a695811051907e162753b56b0f6b410dba74d8a84b2a14b3144e0ef1284754fd17ed950d5965b4b9dd46582db1178d169c6bc465b0d6ff9ca3928fef5b9ae4e418fc15e83ebea0f87fa9ff5eed70050ded2849f47bf959d956850ce929851f0d8115f635b105ee2e4e15d04b2454bf6f4fadf034b10403119cd8e3b92fcc5b")
	DHGenerator = big.NewInt(3)
)

func mustParseBig(s string) *big.Int {
	n, ok := new(big.Int).SetString(s, 16)
	if !ok {
		panic("invalid big int: " + s)
	}
	return n
}

type DHParams struct {
	G       *big.Int
	P       *big.Int
	Ga      *big.Int
	A       *big.Int
	AuthKey *big.Int
}

func GenerateDHParams() (*DHParams, error) {
	a, err := rand.Int(rand.Reader, DHPrime)
	if err != nil {
		return nil, err
	}
	ga := new(big.Int).Exp(DHGenerator, a, DHPrime)
	return &DHParams{
		G:  DHGenerator,
		P:  DHPrime,
		Ga: ga,
		A:  a,
	}, nil
}

func (d *DHParams) ComputeAuthKey(gb *big.Int) {
	d.AuthKey = new(big.Int).Exp(gb, d.A, d.P)
}

func (d *DHParams) AuthKeyBytes() []byte {
	return padTo256(d.AuthKey)
}

func padTo256(n *big.Int) []byte {
	data := n.Bytes()
	if len(data) < 256 {
		padded := make([]byte, 256)
		copy(padded[256-len(data):], data)
		return padded
	}
	return data[len(data)-256:]
}

type Nonce struct {
	ServerNonce [16]byte
	NewNonce    [32]byte
}

func GenerateNonce() (*Nonce, error) {
	n := &Nonce{}
	if _, err := io.ReadFull(rand.Reader, n.ServerNonce[:]); err != nil {
		return nil, err
	}
	if _, err := io.ReadFull(rand.Reader, n.NewNonce[:]); err != nil {
		return nil, err
	}
	return n, nil
}

func ComputeNonceHash(newNonce, serverNonce [16]byte) [16]byte {
	h := sha1.New()
	h.Write(newNonce[:])
	h.Write(serverNonce[:])
	var hash [16]byte
	copy(hash[:], h.Sum(nil)[:16])
	return hash
}

func ComputeCheckSHA(newNonce [32]byte, authKey []byte) [20]byte {
	h := sha1.New()
	h.Write(newNonce[:])
	h.Write(authKey)
	var hash [20]byte
	copy(hash[:], h.Sum(nil))
	return hash
}

func ComputeMsgKey(authKey, data []byte) [16]byte {
	// Use first 32 bytes of auth_key
	h := sha256.Sum256(append(authKey[:32], data...))
	var msgKey [16]byte
	copy(msgKey[:], h[8:24])
	return msgKey
}

func ComputeKDF(authKey []byte, msgKey [16]byte, isClient bool) (key, iv []byte) {
	// MTProto 2.0 AES key/IV derivation
	// auth_key is 256 bytes, we use first 32 bytes and last 32 bytes
	authKeyFirst32 := authKey[:32]
	authKeyLast32 := authKey[len(authKey)-32:]

	h1 := sha256.Sum256(append(msgKey[:], authKeyFirst32...))
	h2 := sha256.Sum256(append(authKeyLast32, msgKey[:]...))

	key = make([]byte, 32)
	iv = make([]byte, 32)
	copy(key[:12], h1[:12])
	copy(key[12:20], h2[12:20])
	copy(key[20:24], h1[20:24])
	copy(key[24:32], h2[20:28])

	copy(iv[:8], h1[12:20])
	copy(iv[8:16], h2[:12])
	copy(iv[16:24], h1[24:32])
	copy(iv[24:32], h2[28:32])

	return key, iv
}

type PQ struct {
	P *big.Int
	Q *big.Int
}

func FactorizePQ(pq *big.Int) (*PQ, error) {
	if pq == nil {
		return nil, ErrInvalidDH
	}

	p := new(big.Int)
	q := new(big.Int)

	if !factorize(pq, p, q) {
		return nil, ErrInvalidDH
	}

	return &PQ{P: p, Q: q}, nil
}

func factorize(n, p, q *big.Int) bool {
	if n.BitLen() > 64 {
		return false
	}

	nInt := n.Int64()

	for i := int64(2); i*i <= nInt; i++ {
		if nInt%i == 0 {
			p.SetInt64(i)
			q.SetInt64(nInt / i)
			return true
		}
	}
	return false
}

func ReadInt128(r io.Reader) ([16]byte, error) {
	var buf [16]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

func WriteInt128(w io.Writer, v [16]byte) error {
	_, err := w.Write(v[:])
	return err
}

func ReadInt256(r io.Reader) ([32]byte, error) {
	var buf [32]byte
	_, err := io.ReadFull(r, buf[:])
	return buf, err
}

func WriteInt256(w io.Writer, v [32]byte) error {
	_, err := w.Write(v[:])
	return err
}

func ReadBigInt(r io.Reader, size int) (*big.Int, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(r, buf); err != nil {
		return nil, err
	}
	return new(big.Int).SetBytes(buf), nil
}

func WriteBigInt(w io.Writer, n *big.Int, size int) error {
	data := n.Bytes()
	if len(data) < size {
		padded := make([]byte, size)
		copy(padded[size-len(data):], data)
		_, err := w.Write(padded)
		return err
	}
	_, err := w.Write(data[len(data)-size:])
	return err
}

type TLBytes []byte

func ReadTLBytes(r io.Reader) ([]byte, error) {
	var lenByte [1]byte
	if _, err := io.ReadFull(r, lenByte[:]); err != nil {
		return nil, err
	}

	length := int(lenByte[0])
	if length >= 254 {
		var lenBytes [3]byte
		if _, err := io.ReadFull(r, lenBytes[:]); err != nil {
			return nil, err
		}
		length = int(lenBytes[0]) | int(lenBytes[1])<<8 | int(lenBytes[2])<<16
	}

	data := make([]byte, length)
	if _, err := io.ReadFull(r, data); err != nil {
		return nil, err
	}

	padding := (4 - ((length + 1) % 4)) % 4
	if padding > 0 {
		dummy := make([]byte, padding)
		if _, err := io.ReadFull(r, dummy); err != nil {
			return nil, err
		}
	}

	return data, nil
}

func WriteTLBytes(w io.Writer, data []byte) error {
	length := len(data)

	if length < 254 {
		if _, err := w.Write([]byte{byte(length)}); err != nil {
			return err
		}
		padding := (4 - ((length + 1) % 4)) % 4
		if _, err := w.Write(data); err != nil {
			return err
		}
		if padding > 0 {
			_, err := w.Write(make([]byte, padding))
			return err
		}
	} else {
		if _, err := w.Write([]byte{254}); err != nil {
			return err
		}
		lenBytes := []byte{byte(length), byte(length >> 8), byte(length >> 16)}
		if _, err := w.Write(lenBytes); err != nil {
			return err
		}
		padding := (4 - (length % 4)) % 4
		if _, err := w.Write(data); err != nil {
			return err
		}
		if padding > 0 {
			_, err := w.Write(make([]byte, padding))
			return err
		}
	}
	return nil
}

func BytesToBigEndian(data []byte) *big.Int {
	return new(big.Int).SetBytes(data)
}

func BigEndianToBytes(n *big.Int, size int) []byte {
	data := n.Bytes()
	if len(data) < size {
		padded := make([]byte, size)
		copy(padded[size-len(data):], data)
		return padded
	}
	return data[len(data)-size:]
}

// DeriveTempKeyIV derives temporary AES key and IV for handshake encryption
// Key = SHA1(new_nonce + server_nonce) + first 12 bytes of SHA1(server_nonce + new_nonce)
// IV = bytes 12-32 of SHA1(server_nonce + new_nonce) + first 8 bytes of SHA1(new_nonce + new_nonce)
func DeriveTempKeyIV(newNonce [32]byte, serverNonce [16]byte) ([]byte, []byte) {
	h1 := sha1.New()
	h1.Write(newNonce[:])
	h1.Write(serverNonce[:])
	hash1 := h1.Sum(nil)

	h2 := sha1.New()
	h2.Write(serverNonce[:])
	h2.Write(newNonce[:])
	hash2 := h2.Sum(nil)

	key := make([]byte, 32)
	copy(key[:20], hash1)
	copy(key[20:32], hash2[:12])

	iv := make([]byte, 32)
	copy(iv[:20], hash2[12:])
	copy(iv[20:32], hash1[:12])

	return key, iv
}

// ComputeAuthKeyID computes auth_key_id as the last 8 bytes of SHA1(auth_key)
func ComputeAuthKeyID(authKey []byte) int64 {
	h := sha1.New()
	h.Write(authKey)
	sum := h.Sum(nil)
	return int64(binary.LittleEndian.Uint64(sum[len(sum)-8:]))
}

// ComputeNewNonceHash1 computes new_nonce_hash1 for dh_gen_ok
// new_nonce_hash1 = SHA1(new_nonce)[0:16] XOR SHA1(server_nonce)[0:16] XOR SHA1(new_nonce)[16:32]
func ComputeNewNonceHash1(newNonce [32]byte, serverNonce [16]byte) [16]byte {
	h1 := sha1.New()
	h1.Write(newNonce[:])
	hash1 := h1.Sum(nil)

	h2 := sha1.New()
	h2.Write(serverNonce[:])
	hash2 := h2.Sum(nil)

	var result [16]byte
	for i := 0; i < 16; i++ {
		var tail byte
		if i+16 < len(hash1) {
			tail = hash1[i+16]
		}
		result[i] = hash1[i] ^ hash2[i] ^ tail
	}
	return result
}

func ReadUint32(r io.Reader) (uint32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf[:]), nil
}

func WriteUint32(w io.Writer, v uint32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}

func ReadInt64(r io.Reader) (int64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int64(binary.LittleEndian.Uint64(buf[:])), nil
}

func WriteInt64(w io.Writer, v int64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], uint64(v))
	_, err := w.Write(buf[:])
	return err
}

func ReadInt32(r io.Reader) (int32, error) {
	var buf [4]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return int32(binary.LittleEndian.Uint32(buf[:])), nil
}

func WriteInt32(w io.Writer, v int32) error {
	var buf [4]byte
	binary.LittleEndian.PutUint32(buf[:], uint32(v))
	_, err := w.Write(buf[:])
	return err
}

func ReadUint64(r io.Reader) (uint64, error) {
	var buf [8]byte
	if _, err := io.ReadFull(r, buf[:]); err != nil {
		return 0, err
	}
	return binary.LittleEndian.Uint64(buf[:]), nil
}

func WriteUint64(w io.Writer, v uint64) error {
	var buf [8]byte
	binary.LittleEndian.PutUint64(buf[:], v)
	_, err := w.Write(buf[:])
	return err
}
