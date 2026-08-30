#!/usr/bin/env sh
set -eu

fail=0

# Removed runtime files must stay removed.
for path in conn.go conn_context.go conn_handler_state.go conn_reliability.go conn_send.go handshake.go reliability_runtime.go updates_runtime.go session/compat.go session/lru.go session/manager.go session/session.go gotd_hooks_enabled.go gotd_hooks_stub.go unknown_constructor.go; do
  if [ -e "$path" ]; then
    echo "legacy check failed: removed runtime file still exists: $path" >&2
    fail=1
  fi
done

# Removed public APIs must not return in production code.
if rg -n "With(Transport|SessionManager|MaxLayer|Layers|UnknownConstructorHandler)\\b|SessionFromContext\\b|ConnFromContext\\b|globalDispatcher\\b|func \\(s \\*Server\\) Register(Constructor|Method)\\b" \
	--glob '*.go' \
	--glob '!**/*_test.go' \
	--glob '!scripts/check_no_legacy.sh' \
	.; then
	echo "legacy check failed: removed public runtime API is present" >&2
	fail=1
fi

if rg -n "type (SessionStore|HandshakeHandler|Conn|Handler|Interceptor)\\b" types.go; then
	echo "legacy check failed: removed root type alias is present" >&2
	fail=1
fi

# Forbid legacy custom framing usage outside transport.
if rg -n "mtprotocodec|ReadPacket\\(|WritePacket\\(|ProtocolTag\\(" \
  --glob '!transport/**' \
  --glob '!docs/**' \
  --glob '!scripts/check_no_legacy.sh' \
  --glob '!**/*_test.go' \
  .; then
  echo "legacy check failed: custom framing should live only in transport" >&2
  fail=1
fi

if [ "$fail" -ne 0 ]; then
  exit 1
fi

echo "legacy check passed"
