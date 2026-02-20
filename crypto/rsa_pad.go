package crypto

import (
	"bytes"
	"crypto/aes"
	"crypto/rsa"
	"crypto/sha256"
	"errors"
	"math/big"
)

const (
	rsaPadDataLimit       = 144
	dataWithPaddingLength = 192
	dataWithHashLength    = dataWithPaddingLength + sha256.Size
	tempKeySize           = 32
	rsaLen                = 256
)

func reverseBytes(s []byte) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func fillBytes(b *big.Int, to []byte) bool {
	bits := b.BitLen()
	if (bits+7)/8 > len(to) {
		return false
	}
	b.FillBytes(to)
	return true
}

func rsaRawDecrypt(data []byte, key *rsa.PrivateKey, to []byte) bool {
	c := new(big.Int).SetBytes(data)
	m := new(big.Int).Exp(c, key.D, key.N)
	return fillBytes(m, to)
}

// decodeRSAPad implements MTProto RSA_PAD server-side decode.
func decodeRSAPad(data []byte, key *rsa.PrivateKey) ([]byte, error) {
	if len(data) != rsaLen {
		return nil, errors.New("crypto: invalid RSA_PAD length")
	}
	var encryptedData [rsaLen]byte
	if !rsaRawDecrypt(data, key, encryptedData[:]) {
		return nil, errors.New("crypto: RSA_PAD decrypt failed")
	}

	tempKeyXor := encryptedData[:tempKeySize]
	aesEncrypted := encryptedData[tempKeySize:]

	tempKey := make([]byte, tempKeySize)
	{
		aesEncryptedHash := sha256.Sum256(aesEncrypted)
		for i := 0; i < tempKeySize; i++ {
			tempKey[i] = tempKeyXor[i] ^ aesEncryptedHash[i]
		}
	}

	dataWithHash := make([]byte, len(aesEncrypted))
	{
		aesBlock, err := aes.NewCipher(tempKey)
		if err != nil {
			return nil, err
		}
		zeroIV := make([]byte, 32)
		DecryptIGE(dataWithHash, aesEncrypted, aesBlock, zeroIV)
	}

	dataWithPadding := make([]byte, dataWithPaddingLength)
	copy(dataWithPadding, dataWithHash[:dataWithPaddingLength])
	reverseBytes(dataWithPadding)

	hash := dataWithHash[dataWithPaddingLength:]
	{
		h := sha256.New()
		_, _ = h.Write(tempKey)
		_, _ = h.Write(dataWithPadding)
		if !bytes.Equal(hash, h.Sum(nil)) {
			return nil, errors.New("crypto: RSA_PAD hash mismatch")
		}
	}

	return dataWithPadding, nil
}
