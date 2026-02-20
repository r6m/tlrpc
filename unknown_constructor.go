package tlrpc

import (
	"context"
	"encoding/hex"
	"fmt"
)

type contextKeyTransportMode struct{}
type contextKeyUnknownPhase struct{}

// UnknownConstructorHandler is invoked when the server sees a constructor ID
// that has no registered TL constructor mapping.
type UnknownConstructorHandler func(ctx context.Context, ctor uint32, payload []byte)

func withTransportMode(ctx context.Context, mode string) context.Context {
	if mode == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKeyTransportMode{}, mode)
}

// TransportModeFromContext returns negotiated transport mode (if known).
func TransportModeFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(contextKeyTransportMode{}); v != nil {
		if mode, ok := v.(string); ok {
			return mode
		}
	}
	return ""
}

func withUnknownConstructorPhase(ctx context.Context, phase string) context.Context {
	if phase == "" {
		return ctx
	}
	return context.WithValue(ctx, contextKeyUnknownPhase{}, phase)
}

// UnknownConstructorPhaseFromContext returns decode phase ("handshake"/"encrypted"/...).
func UnknownConstructorPhaseFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v := ctx.Value(contextKeyUnknownPhase{}); v != nil {
		if phase, ok := v.(string); ok {
			return phase
		}
	}
	return ""
}

func (s *Server) reportUnknownConstructor(ctx context.Context, ctor uint32, payload []byte) {
	if s == nil || s.unknownConstructorHandler == nil {
		return
	}
	buf := make([]byte, len(payload))
	copy(buf, payload)
	s.unknownConstructorHandler(ctx, ctor, buf)
}

func isUnknownConstructorError(err error) bool {
	rpcErr, ok := IsRPCError(err)
	return ok && rpcErr.ErrorCode == int32(NotFound) && rpcErr.ErrorMessage == "UNKNOWN_CONSTRUCTOR"
}

func payloadPrefixHex(payload []byte, max int) string {
	if max <= 0 || len(payload) == 0 {
		return ""
	}
	if len(payload) > max {
		payload = payload[:max]
	}
	return hex.EncodeToString(payload)
}

func unknownConstructorSummary(ctx context.Context, ctor uint32, payload []byte) string {
	return fmt.Sprintf(
		"unknown ctor=0x%08x phase=%s layer=%d auth_key_id=%d transport=%s payload_prefix=%s",
		ctor,
		UnknownConstructorPhaseFromContext(ctx),
		LayerFromContext(ctx),
		AuthKeyIDFromContext(ctx),
		TransportModeFromContext(ctx),
		payloadPrefixHex(payload, 32),
	)
}
