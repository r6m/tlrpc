package transport

import (
	"bufio"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	mtprotocodec "github.com/r6m/tlrpc/transport/mtproto_codec"
)

type Protocol int

const (
	ProtocolUnknown Protocol = iota
	ProtocolAbridged
	ProtocolIntermediate
	ProtocolPaddedIntermediate
	ProtocolFull
)

type NegotiatorConfig struct {
	AllowObfuscation   bool
	RequireObfuscation bool
	Secret             []byte
	Protocol           Protocol // client preference
}

type Negotiator struct {
	config NegotiatorConfig
}

func NewNegotiator(config NegotiatorConfig) *Negotiator {
	return &Negotiator{config: config}
}

func (n *Negotiator) Negotiate(r *bufio.Reader, w *bufio.Writer) (io.ReadWriter, mtprotocodec.Codec, error) {
	if n.config.RequireObfuscation {
		return n.acceptObfuscated(r, w)
	}

	peek, err := r.Peek(4)
	if err != nil {
		return nil, nil, err
	}

	if peek[0] == 0xEF {
		_, _ = r.ReadByte()
		return &bufferedReadWriter{r: r, w: w}, &mtprotocodec.Abridged{}, nil
	}

	sig := binary.LittleEndian.Uint32(peek)
	switch sig {
	case (&mtprotocodec.Intermediate{}).ProtocolTag():
		_, _ = r.Discard(4)
		return &bufferedReadWriter{r: r, w: w}, &mtprotocodec.Intermediate{}, nil
	case (&mtprotocodec.PaddedIntermediate{}).ProtocolTag():
		_, _ = r.Discard(4)
		return &bufferedReadWriter{r: r, w: w}, &mtprotocodec.PaddedIntermediate{}, nil
	}

	length := binary.LittleEndian.Uint32(peek)
	if isPlausibleFullLength(length) {
		return &bufferedReadWriter{r: r, w: w}, &mtprotocodec.Full{}, nil
	}

	// Try obfuscation if allowed.
	if n.config.AllowObfuscation {
		return n.tryObfuscated(r, w)
	}

	return nil, nil, ErrInvalidObfuscatedHeader
}

func (n *Negotiator) acceptObfuscated(r *bufio.Reader, w *bufio.Writer) (io.ReadWriter, mtprotocodec.Codec, error) {
	init := make([]byte, 64)
	if _, err := io.ReadFull(r, init); err != nil {
		return nil, nil, err
	}
	streams, err := NewServerObfuscated(init, n.config.Secret)
	if err != nil {
		return nil, nil, err
	}
	codec, err := codecFromTag(streams.Tag)
	if err != nil {
		return nil, nil, err
	}
	brw := &bufferedReadWriter{r: r, w: w}
	stream := newObfuscatedStream(brw, brw, streams)
	return stream, codec, nil
}

func (n *Negotiator) tryObfuscated(r *bufio.Reader, w *bufio.Writer) (io.ReadWriter, mtprotocodec.Codec, error) {
	init := make([]byte, 64)
	if _, err := io.ReadFull(r, init); err != nil {
		return nil, nil, err
	}
	streams, err := NewServerObfuscated(init, n.config.Secret)
	if err != nil {
		return nil, nil, err
	}
	codec, err := codecFromTag(streams.Tag)
	if err != nil {
		return nil, nil, err
	}
	brw := &bufferedReadWriter{r: r, w: w}
	stream := newObfuscatedStream(brw, brw, streams)
	return stream, codec, nil
}

func codecFromTag(tag uint32) (mtprotocodec.Codec, error) {
	switch tag {
	case (&mtprotocodec.Intermediate{}).ProtocolTag():
		return &mtprotocodec.Intermediate{}, nil
	case (&mtprotocodec.PaddedIntermediate{}).ProtocolTag():
		return &mtprotocodec.PaddedIntermediate{}, nil
	case (&mtprotocodec.Abridged{}).ProtocolTag():
		return &mtprotocodec.Abridged{}, nil
	default:
		return nil, fmt.Errorf("transport: unknown obfuscated protocol tag 0x%08x", tag)
	}
}

var ErrClientProtocol = errors.New("transport: client protocol selection required")

func (n *Negotiator) ClientNegotiate(rw io.ReadWriter) (io.ReadWriter, mtprotocodec.Codec, error) {
	protocol := n.config.Protocol
	if protocol == ProtocolUnknown {
		return nil, nil, ErrClientProtocol
	}

	if n.config.RequireObfuscation || n.config.AllowObfuscation {
		var tag uint32
		switch protocol {
		case ProtocolAbridged:
			tag = (&mtprotocodec.Abridged{}).ProtocolTag()
		case ProtocolIntermediate:
			tag = (&mtprotocodec.Intermediate{}).ProtocolTag()
		case ProtocolPaddedIntermediate:
			tag = (&mtprotocodec.PaddedIntermediate{}).ProtocolTag()
		default:
			return nil, nil, ErrClientProtocol
		}

		header, streams, err := NewClientObfuscated(tag, n.config.Secret)
		if err != nil {
			return nil, nil, err
		}
		if _, err := rw.Write(header); err != nil {
			return nil, nil, err
		}
		stream := newObfuscatedStream(rw, rw, streams)
		codec, err := codecFromTag(tag)
		return stream, codec, err
	}

	// Plain protocol header.
	switch protocol {
	case ProtocolAbridged:
		if _, err := rw.Write([]byte{0xEF}); err != nil {
			return nil, nil, err
		}
		return rw, &mtprotocodec.Abridged{}, nil
	case ProtocolIntermediate:
		if _, err := rw.Write([]byte{0xEE, 0xEE, 0xEE, 0xEE}); err != nil {
			return nil, nil, err
		}
		return rw, &mtprotocodec.Intermediate{}, nil
	case ProtocolPaddedIntermediate:
		if _, err := rw.Write([]byte{0xDD, 0xDD, 0xDD, 0xDD}); err != nil {
			return nil, nil, err
		}
		return rw, &mtprotocodec.PaddedIntermediate{}, nil
	case ProtocolFull:
		return rw, &mtprotocodec.Full{}, nil
	default:
		return nil, nil, ErrClientProtocol
	}
}

func (n *Negotiator) ClientCodec(rw io.ReadWriter) (io.ReadWriter, mtprotocodec.Codec, error) {
	return n.ClientNegotiate(rw)
}

func isPlausibleFullLength(length uint32) bool {
	return length >= 12 && length <= 1<<24
}
