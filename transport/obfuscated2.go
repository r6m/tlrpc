package transport

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
)

var (
	ErrInvalidObfuscatedHeader = errors.New("transport: invalid obfuscated header")
)

var disallowedPrefixes = []uint32{
	0x44414548, // "HEAD"
	0x54534f50, // "POST"
	0x20544547, // "GET "
	0x4954504f, // "OPTI"
	0xEEEEEEEE,
	0xDDDDDDDD,
	0x02010316,
	0x00000000,
}

func isDisallowedHeader(header []byte) bool {
	if len(header) < 4 {
		return true
	}
	v := binary.LittleEndian.Uint32(header[:4])
	for _, disallowed := range disallowedPrefixes {
		if v == disallowed {
			return true
		}
	}
	if header[0] == 0xef {
		return true
	}
	if len(header) >= 8 {
		if header[4] == 0 && header[5] == 0 && header[6] == 0 && header[7] == 0 {
			return true
		}
	}
	return false
}

type ObfuscatedStreams struct {
	Encrypt cipher.Stream
	Decrypt cipher.Stream
	Tag     uint32
}

func NewClientObfuscated(tag uint32, secret []byte) (header []byte, streams ObfuscatedStreams, err error) {
	init := make([]byte, 64)
	for {
		if _, err = rand.Read(init); err != nil {
			return nil, ObfuscatedStreams{}, err
		}
		if !isDisallowedHeader(init) {
			break
		}
	}
	binary.LittleEndian.PutUint32(init[56:60], tag)

	encryptKey := append([]byte(nil), init[8:40]...)
	encryptIV := append([]byte(nil), init[40:56]...)
	decryptKey := append([]byte(nil), reverseBytes(init)[8:40]...)
	decryptIV := append([]byte(nil), reverseBytes(init)[40:56]...)

	encryptKey = applySecret(encryptKey, secret)
	decryptKey = applySecret(decryptKey, secret)

	encrypt, err := newCTR(encryptKey, encryptIV)
	if err != nil {
		return nil, ObfuscatedStreams{}, err
	}
	decrypt, err := newCTR(decryptKey, decryptIV)
	if err != nil {
		return nil, ObfuscatedStreams{}, err
	}

	encryptedInit := make([]byte, len(init))
	copy(encryptedInit, init)
	encrypt.XORKeyStream(encryptedInit, encryptedInit)
	copy(init[56:64], encryptedInit[56:64])

	return init, ObfuscatedStreams{Encrypt: encrypt, Decrypt: decrypt, Tag: tag}, nil
}

func NewServerObfuscated(header []byte, secret []byte) (streams ObfuscatedStreams, err error) {
	if len(header) != 64 {
		return ObfuscatedStreams{}, ErrInvalidObfuscatedHeader
	}
	if isDisallowedHeader(header) {
		return ObfuscatedStreams{}, ErrInvalidObfuscatedHeader
	}

	decryptKey := append([]byte(nil), header[8:40]...)
	decryptIV := append([]byte(nil), header[40:56]...)
	encryptKey := append([]byte(nil), reverseBytes(header)[8:40]...)
	encryptIV := append([]byte(nil), reverseBytes(header)[40:56]...)

	encryptKey = applySecret(encryptKey, secret)
	decryptKey = applySecret(decryptKey, secret)

	encrypt, err := newCTR(encryptKey, encryptIV)
	if err != nil {
		return ObfuscatedStreams{}, err
	}
	decrypt, err := newCTR(decryptKey, decryptIV)
	if err != nil {
		return ObfuscatedStreams{}, err
	}

	plain := make([]byte, len(header))
	copy(plain, header)
	decrypt.XORKeyStream(plain, plain)
	tag := binary.LittleEndian.Uint32(plain[56:60])

	return ObfuscatedStreams{Encrypt: encrypt, Decrypt: decrypt, Tag: tag}, nil
}

func applySecret(key []byte, secret []byte) []byte {
	if len(secret) == 0 {
		return key
	}
	h := sha256.New()
	_, _ = h.Write(key)
	_, _ = h.Write(secret)
	sum := h.Sum(nil)
	return sum[:32]
}

func newCTR(key, iv []byte) (cipher.Stream, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewCTR(block, iv), nil
}

func reverseBytes(data []byte) []byte {
	out := make([]byte, len(data))
	for i := range data {
		out[i] = data[len(data)-1-i]
	}
	return out
}

// obfuscatedStream wraps a reader/writer with AES-CTR.
type obfuscatedStream struct {
	r io.Reader
	w io.Writer
	e cipher.Stream
	d cipher.Stream
}

func (s *obfuscatedStream) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.d.XORKeyStream(p[:n], p[:n])
	}
	return n, err
}

func (s *obfuscatedStream) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	s.e.XORKeyStream(buf, buf)
	return s.w.Write(buf)
}

func newObfuscatedStream(r io.Reader, w io.Writer, streams ObfuscatedStreams) io.ReadWriter {
	return &obfuscatedStream{r: r, w: w, e: streams.Encrypt, d: streams.Decrypt}
}
