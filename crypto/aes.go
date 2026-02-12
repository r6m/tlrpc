package crypto

import (
	"crypto/aes"
	"crypto/cipher"
)

const (
	aesBlockSize  = 16
	aes256KeySize = 32
	igeIVSize     = 32
)

// NewAESIGE creates AES-256-IGE cipher.
// It returns an encrypting BlockMode. For decryption, use DecryptIGE.
func NewAESIGE(key, iv []byte) cipher.BlockMode {
	if len(key) != aes256KeySize {
		panic("crypto: AES-256-IGE requires 32-byte key")
	}
	if len(iv) != igeIVSize {
		panic("crypto: AES-256-IGE requires 32-byte iv")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	mode := &ige{block: block}
	copy(mode.iv[:], iv)
	return mode
}

// NewAESIGEDecrypt creates AES-256-IGE cipher for decryption.
func NewAESIGEDecrypt(key, iv []byte) cipher.BlockMode {
	if len(key) != aes256KeySize {
		panic("crypto: AES-256-IGE requires 32-byte key")
	}
	if len(iv) != igeIVSize {
		panic("crypto: AES-256-IGE requires 32-byte iv")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		panic(err)
	}
	mode := &ige{block: block, decrypt: true}
	copy(mode.iv[:], iv)
	return mode
}

// EncryptIGE encrypts src into dst using AES-IGE.
func EncryptIGE(dst, src []byte, block cipher.Block, iv []byte) {
	cryptIGE(dst, src, block, iv, false)
}

// DecryptIGE decrypts src into dst using AES-IGE.
func DecryptIGE(dst, src []byte, block cipher.Block, iv []byte) {
	cryptIGE(dst, src, block, iv, true)
}

type ige struct {
	block   cipher.Block
	iv      [igeIVSize]byte
	decrypt bool
}

func (m *ige) BlockSize() int {
	return aesBlockSize
}

func (m *ige) CryptBlocks(dst, src []byte) {
	cryptIGEState(dst, src, m.block, &m.iv, m.decrypt)
}

func cryptIGE(dst, src []byte, block cipher.Block, iv []byte, decrypt bool) {
	if len(iv) != igeIVSize {
		panic("crypto: AES-IGE requires 32-byte iv")
	}
	var ivState [igeIVSize]byte
	copy(ivState[:], iv)
	cryptIGEState(dst, src, block, &ivState, decrypt)
}

func cryptIGEState(dst, src []byte, block cipher.Block, iv *[igeIVSize]byte, decrypt bool) {
	if block.BlockSize() != aesBlockSize {
		panic("crypto: AES-IGE requires 16-byte block size")
	}
	if len(src)%aesBlockSize != 0 {
		panic("crypto: AES-IGE requires full blocks")
	}
	if len(dst) < len(src) {
		panic("crypto: AES-IGE output smaller than input")
	}

	prevCipher := iv[:aesBlockSize]
	prevPlain := iv[aesBlockSize:]

	var tmp [aesBlockSize]byte
	var srcBlock [aesBlockSize]byte
	var cipherBlock [aesBlockSize]byte

	for i := 0; i < len(src); i += aesBlockSize {
		copy(srcBlock[:], src[i:i+aesBlockSize])

		if decrypt {
			xorBlock(tmp[:], srcBlock[:], prevPlain)
			block.Decrypt(tmp[:], tmp[:])
			xorBlock(dst[i:i+aesBlockSize], tmp[:], prevCipher)

			copy(prevPlain, dst[i:i+aesBlockSize])
			copy(prevCipher, srcBlock[:])
			continue
		}

		xorBlock(tmp[:], srcBlock[:], prevCipher)
		block.Encrypt(tmp[:], tmp[:])
		xorBlock(dst[i:i+aesBlockSize], tmp[:], prevPlain)

		copy(prevPlain, srcBlock[:])
		copy(cipherBlock[:], dst[i:i+aesBlockSize])
		copy(prevCipher, cipherBlock[:])
	}
}

func xorBlock(dst, a, b []byte) {
	for i := 0; i < aesBlockSize; i++ {
		dst[i] = a[i] ^ b[i]
	}
}
