# gnet/gws Transport Notes

This document captures a future plan to add optional transports backed by:
- gnet: https://github.com/panjf2000/gnet
- gws: https://github.com/lxzan/gws

The goal is to keep the current net-based TCP/WebSocket transports as the default and add high-performance alternatives that still satisfy `transport` interfaces.

## Compatibility Summary

It is possible to implement `transport.Transport`, `transport.Listener`, and `transport.Conn` using gnet and gws, but they are event-loop based and do not expose `net.Conn` semantics. An adapter layer is required.

Key compatibility points:
- `ReadMessage`/`WriteMessage`: Must be implemented via per-connection queues and event callbacks.
- `SetDeadline`: Must be emulated via timers on the adapter.
- `Context`: Must be canceled on close by the adapter.
- MTProto framing: Must be handled in the event callbacks using the same length-prefixed framing in `transport/common.go`.

## Proposed Structure

Add new files with explicit names (or build tags if desired):
- `transport/tcp_gnet.go` (gnet-based TCP transport)
- `transport/websocket_gws.go` (gws-based WebSocket transport)

Keep existing implementations in:
- `transport/tcp.go`
- `transport/websocket.go`

## gnet Transport Design

### Listener Adapter

`gnet.EventHandler` provides callbacks:
- `OnOpened`: allocate connection state
- `OnClosed`: cancel context, close queues
- `React`/`OnTraffic`: read bytes, parse frames

The listener should:
- run gnet server loop
- push new adapter connections into a channel for `Accept()`
- expose `Close()` to stop the server and cancel accepts

### Connection Adapter

`gnet.Conn` does not implement `net.Conn`. The adapter should expose:
- `ReadMessage()`: read from an inbound channel populated by `OnTraffic`
- `WriteMessage()`: enqueue message for event-loop write
- `SetDeadline()`: store deadline timestamps and enforce in adapter logic
- `Context()`: context canceled on connection close

### Framing

In `OnTraffic`, buffer bytes and parse frames with the same MTProto framing:
- 4-byte little-endian length (includes the 4 bytes)
- payload bytes
- optional padding to 4-byte boundary

When a full frame is available, push the payload into the inbound queue.

### Backpressure

Define inbound and outbound bounded queues.
- If inbound queue is full, drop connection or apply backpressure.
- If outbound queue is full, return an error from `WriteMessage`.

### Deadlines

Implement read/write deadlines in the adapter with timers:
- read deadline: if no inbound frame by deadline, return timeout error
- write deadline: if outbound queue or write not completed by deadline, return timeout error

## gws Transport Design

### Listener Adapter

gws is HTTP-based and event-driven. The listener should:
- start an HTTP server with gws handler
- upgrade to WebSocket on request
- require `binary` subprotocol (optional, but align with current implementation)

### Connection Adapter

Expose the same adapter semantics as gnet:
- `ReadMessage` from an inbound queue filled by gws message callbacks
- `WriteMessage` sends binary frames
- `Context` canceled on close
- `SetDeadline` emulated using timers

### Message Type Handling

Reject non-binary frames in the adapter:
- return an error on `ReadMessage` for text frames

## Error/Shutdown Semantics

The adapter must ensure:
- `Context()` is canceled on close
- `Accept()` unblocks on listener close
- `ReadMessage()` and `WriteMessage()` return errors when connection is closed

## Dependency Plan

Add optional dependencies:
- `github.com/panjf2000/gnet/v2`
- `github.com/lxzan/gws`

Consider build tags if keeping these optional:
- `//go:build gnet`
- `//go:build gws`

## Suggested Tests

Mirror existing tests:
- transport frame round-trip
- TCP/gnet listen/dial read/write
- WebSocket/gws listen/dial read/write
- Context cancellation on close

## Risks

- Adapter complexity: deadlines and backpressure need careful handling.
- Event-loop constraints: avoid blocking in callbacks.
- Error propagation: map gnet/gws errors to transport errors consistently.

## Recommendation

Add gnet/gws transports as optional alternatives while keeping the net-based implementations as the default for simplicity.
