# Transport Layer

The transport package provides the **carrier transports** (TCP/WebSocket) and the **MTProto transport protocol codecs** (Abridged/Intermediate/Padded Intermediate/Full). Carrier transports expose a byte stream; MTProto transport protocols define how MTProto packets are framed within that stream.

## Overview

Responsibilities:
- Establish network connections (TCP/WebSocket)
- Negotiate MTProto transport protocol
- Apply MTProto obfuscation (obfuscated2 AES-CTR)
- Read/write MTProto packets with correct framing

## MTProto Transport Protocols

Implemented codecs:
- Abridged
- Intermediate
- Padded Intermediate
- Full

## Obfuscation

`obfuscated2` is supported. It is optional for TCP and required for WebSocket. The obfuscation header embeds the protocol tag used for negotiation.

## WebSocket Requirements

- `Sec-WebSocket-Protocol: binary` is required
- WebSocket frames are treated as a duplex byte stream (frame boundaries are ignored)
- MTProto framing is done by the MTProto transport codec

## Usage

```go
srv := tlrpc.NewServer()
tcp := &transport.TCPTransport{}
ws := &transport.WebSocketTransport{}

tcpLis, _ := tcp.Listen(":9000")
wsLis, _ := ws.Listen(":9001")

go srv.ServeTransport(tcpLis)
go srv.ServeTransport(wsLis)
```

The `Conn` interface represents MTProto packets (not WebSocket frames or raw TCP reads).
