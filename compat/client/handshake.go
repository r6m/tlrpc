package client

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"

	"github.com/r6m/tlrpc/crypto"
	"github.com/r6m/tlrpc/mtproto"
)

const (
	reqPQMultiID      uint32 = 0xbe7e8ef1
	resPQID           uint32 = 0x05162463
	reqDHParamsID     uint32 = 0xd712e4be
	pQInnerDataID     uint32 = 0x83c95aec
	serverDHParamsOK  uint32 = 0xd0e8075c
	serverDHInnerData uint32 = 0xb5890dba
	setClientDHParams uint32 = 0xf5045f1f
	clientDHInnerData uint32 = 0x6643b654
	dhGenOK           uint32 = 0x3bcbf734
)

// For the server's hard-coded pq value 0x17ED48941A08F981.
var (
	fixedP = []byte{0x49, 0x4c, 0x55, 0x3b}
	fixedQ = []byte{0x53, 0x91, 0x10, 0x73}
)

func (c *Client) performHandshake(ctx context.Context) (*SessionInfo, error) {
	_ = ctx
	nonce := [16]byte{0x01, 0x02, 0x03, 0x04}
	var newNonce [32]byte
	for i := range newNonce {
		newNonce[i] = byte(0x10 + i)
	}

	reqPQ, err := serializeTL(func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, reqPQMultiID); err != nil {
			return err
		}
		return mtproto.WriteInt128(w, nonce)
	})
	if err != nil {
		return nil, err
	}
	if err := writeUnencrypted(c.conn, c.msgID(), reqPQ); err != nil {
		return nil, fmt.Errorf("write req_pq_multi: %w", err)
	}
	resPQMsg, err := readUnencrypted(c.conn)
	if err != nil {
		return nil, fmt.Errorf("read resPQ: %w", err)
	}

	var serverNonce [16]byte
	var keyFingerprint int64
	{
		r := bytes.NewReader(resPQMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read resPQ ctor: %w", err)
		}
		if ctor != resPQID {
			return nil, errors.New("compat client: unexpected resPQ ctor")
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read resPQ nonce: %w", err)
		}
		if gotNonce != nonce {
			return nil, errors.New("compat client: nonce mismatch")
		}
		serverNonce, err = mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read resPQ server nonce: %w", err)
		}
		if _, err := mtproto.ReadBytes(r); err != nil {
			return nil, fmt.Errorf("read resPQ pq: %w", err)
		}
		var fingerprints []int64
		next, err := mtproto.ReadUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read resPQ fingerprints prefix: %w", err)
		}
		if next == mtproto.VectorConstructorID {
			n, err := mtproto.ReadInt32(r)
			if err != nil {
				return nil, fmt.Errorf("read resPQ fingerprints count: %w", err)
			}
			if n < 0 {
				return nil, errors.New("compat client: negative fingerprints count")
			}
			for i := 0; i < int(n); i++ {
				fp, err := mtproto.ReadInt64(r)
				if err != nil {
					return nil, fmt.Errorf("read resPQ fingerprint[%d]: %w", i, err)
				}
				fingerprints = append(fingerprints, fp)
			}
		} else {
			// Legacy compat path: marker uint64(1), then Vector<long>.
			hi, err := mtproto.ReadUint32(r)
			if err != nil {
				return nil, fmt.Errorf("read resPQ legacy marker: %w", err)
			}
			marker := uint64(next) | (uint64(hi) << 32)
			if marker != 1 {
				return nil, fmt.Errorf("compat client: unexpected resPQ marker/vector prefix: %08x", next)
			}
			if err := mtproto.ReadVector(r, func() error {
				fp, err := mtproto.ReadInt64(r)
				if err != nil {
					return err
				}
				fingerprints = append(fingerprints, fp)
				return nil
			}); err != nil {
				return nil, err
			}
		}
		if len(fingerprints) == 0 {
			return nil, errors.New("compat client: resPQ returned no fingerprints")
		}
		keyFingerprint = fingerprints[0]
		if keyFingerprint != c.serverKey.ID {
			return nil, errors.New("compat client: server key fingerprint mismatch")
		}
	}

	pqInner, err := serializeTL(func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, pQInnerDataID); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, []byte{0x17, 0xED, 0x48, 0x94, 0x1A, 0x08, 0xF9, 0x81}); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedP); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedQ); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		return mtproto.WriteInt256(w, newNonce)
	})
	if err != nil {
		return nil, err
	}
	encryptedPQInner, err := rsa.EncryptPKCS1v15(rand.Reader, &c.serverKey.Key.PublicKey, pqInner)
	if err != nil {
		return nil, err
	}

	reqDH, err := serializeTL(func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, reqDHParamsID); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedP); err != nil {
			return err
		}
		if err := mtproto.WriteBytes(w, fixedQ); err != nil {
			return err
		}
		if err := mtproto.WriteInt64(w, keyFingerprint); err != nil {
			return err
		}
		return mtproto.WriteBytes(w, encryptedPQInner)
	})
	if err != nil {
		return nil, err
	}
	if err := writeUnencrypted(c.conn, c.msgID(), reqDH); err != nil {
		return nil, fmt.Errorf("write req_DH_params: %w", err)
	}
	serverDHParamsMsg, err := readUnencrypted(c.conn)
	if err != nil {
		return nil, fmt.Errorf("read server_DH_params_ok: %w", err)
	}

	var encryptedAnswer []byte
	{
		r := bytes.NewReader(serverDHParamsMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_params ctor: %w", err)
		}
		if ctor != serverDHParamsOK {
			return nil, errors.New("compat client: unexpected server_DH_params ctor")
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_params nonce: %w", err)
		}
		if gotNonce != nonce {
			return nil, errors.New("compat client: nonce mismatch on server_DH_params")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_params server_nonce: %w", err)
		}
		if gotServerNonce != serverNonce {
			return nil, errors.New("compat client: server nonce mismatch")
		}
		encryptedAnswer, err = mtproto.ReadBytes(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_params encrypted_answer: %w", err)
		}
		if len(encryptedAnswer) == 0 || len(encryptedAnswer)%16 != 0 {
			return nil, fmt.Errorf("invalid encrypted_answer length: %d", len(encryptedAnswer))
		}
	}

	tempKey, tempIV := crypto.DeriveTempKeyIV(newNonce, serverNonce)
	serverDHPlain := make([]byte, len(encryptedAnswer))
	crypto.NewAESIGEDecrypt(tempKey, tempIV).CryptBlocks(serverDHPlain, encryptedAnswer)

	var dhPrime *big.Int
	var gVal uint32
	var gA *big.Int
	{
		r, err := parseServerDHInnerData(serverDHPlain)
		if err != nil {
			return nil, err
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_inner_data nonce: %w", err)
		}
		if gotNonce != nonce {
			return nil, errors.New("compat client: server_DH_inner nonce mismatch")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_inner_data server_nonce: %w", err)
		}
		if gotServerNonce != serverNonce {
			return nil, errors.New("compat client: server_DH_inner server nonce mismatch")
		}
		gVal, err = mtproto.ReadUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_inner_data g: %w", err)
		}
		dhPrimeBytes, err := mtproto.ReadBytes(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_inner_data dh_prime: %w", err)
		}
		dhPrime = new(big.Int).SetBytes(dhPrimeBytes)
		gABytes, err := mtproto.ReadBytes(r)
		if err != nil {
			return nil, fmt.Errorf("read server_DH_inner_data g_a: %w", err)
		}
		gA = new(big.Int).SetBytes(gABytes)
	}

	// Compute g_b and auth_key with server range checks.
	g := new(big.Int).SetUint64(uint64(gVal))
	minGB := new(big.Int).Lsh(big.NewInt(1), uint(dhPrime.BitLen()-64))
	maxGB := new(big.Int).Sub(dhPrime, minGB)
	var (
		b  *big.Int
		gb *big.Int
	)
	for {
		b, err = rand.Int(rand.Reader, dhPrime)
		if err != nil {
			return nil, err
		}
		gb = new(big.Int).Exp(g, b, dhPrime)
		if gb.Cmp(minGB) > 0 && gb.Cmp(maxGB) < 0 {
			break
		}
	}
	authKey := new(big.Int).Exp(gA, b, dhPrime)
	authKeyBytes := padTo256(authKey)
	c.authKeyID = crypto.KeyID(crypto.ComputeAuthKeyID(authKeyBytes))
	copy(c.authKey[:], authKeyBytes)

	clientDHPlain, err := serializeTL(func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, clientDHInnerData); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt64(w, 0); err != nil { // retry_id
			return err
		}
		return mtproto.WriteBytes(w, gb.Bytes())
	})
	if err != nil {
		return nil, err
	}
	if rem := len(clientDHPlain) % 16; rem != 0 {
		clientDHPlain = append(clientDHPlain, make([]byte, 16-rem)...)
	}
	encryptedClientDH := make([]byte, len(clientDHPlain))
	crypto.NewAESIGE(tempKey, tempIV).CryptBlocks(encryptedClientDH, clientDHPlain)

	setClientDH, err := serializeTL(func(w io.Writer) error {
		if err := mtproto.WriteUint32(w, setClientDHParams); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, nonce); err != nil {
			return err
		}
		if err := mtproto.WriteInt128(w, serverNonce); err != nil {
			return err
		}
		return mtproto.WriteBytes(w, encryptedClientDH)
	})
	if err != nil {
		return nil, err
	}
	if err := writeUnencrypted(c.conn, c.msgID(), setClientDH); err != nil {
		return nil, fmt.Errorf("write set_client_DH_params: %w", err)
	}
	dhGenMsg, err := readUnencrypted(c.conn)
	if err != nil {
		return nil, fmt.Errorf("read dh_gen_ok: %w", err)
	}
	{
		r := bytes.NewReader(dhGenMsg.Data)
		ctor, err := mtproto.ReadUint32(r)
		if err != nil {
			return nil, fmt.Errorf("read dh_gen_ok ctor: %w", err)
		}
		if ctor != dhGenOK {
			return nil, errors.New("compat client: unexpected dh_gen ctor")
		}
		gotNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read dh_gen_ok nonce: %w", err)
		}
		if gotNonce != nonce {
			return nil, errors.New("compat client: dh_gen nonce mismatch")
		}
		gotServerNonce, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read dh_gen_ok server_nonce: %w", err)
		}
		if gotServerNonce != serverNonce {
			return nil, errors.New("compat client: dh_gen server_nonce mismatch")
		}
		gotHash, err := mtproto.ReadInt128(r)
		if err != nil {
			return nil, fmt.Errorf("read dh_gen_ok new_nonce_hash1: %w", err)
		}
		wantHash := crypto.ComputeNewNonceHash1Auth(newNonce, authKeyBytes)
		if gotHash != wantHash {
			return nil, errors.New("compat client: new_nonce_hash1 mismatch")
		}
	}

	c.serverSalt = 0
	c.sessionID = randInt64()

	return &SessionInfo{
		AuthKeyID:  c.authKeyID,
		AuthKey:    c.authKey,
		ServerSalt: c.serverSalt,
		SessionID:  c.sessionID,
	}, nil
}

func randInt64() int64 {
	var buf [8]byte
	_, _ = io.ReadFull(rand.Reader, buf[:])
	return int64(binary.LittleEndian.Uint64(buf[:]))
}

func parseServerDHInnerData(data []byte) (*bytes.Reader, error) {
	rd := bytes.NewReader(data)
	ctor, err := mtproto.ReadUint32(rd)
	if err == nil && ctor == serverDHInnerData {
		return rd, nil
	}
	// MTProto 2.0: exchange answer may be prefixed by SHA1 hash.
	if len(data) > 24 {
		rd = bytes.NewReader(data[20:])
		ctor, err = mtproto.ReadUint32(rd)
		if err == nil && ctor == serverDHInnerData {
			return rd, nil
		}
	}
	return nil, errors.New("compat client: unexpected server_DH_inner_data ctor")
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
