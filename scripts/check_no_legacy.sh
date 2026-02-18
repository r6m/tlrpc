#!/usr/bin/env sh
set -eu

fail=0

# Forbid legacy transport option usage.
if rg -n "WithTransport\\(" .; then
  echo "legacy check failed: WithTransport is not allowed. Use ServeTransport with listeners instead." >&2
  fail=1
fi

# Forbid MTProto constructor IDs hardcoded in conn.go.
mtproto_ids='0x73f1f8dc|0x62d6b459|0x3072cfa1|0xf35c6d01|0x2144ca19|0x7d861a08|0xda69fb52|0x04deb57d'
if rg -n "$mtproto_ids" conn.go >/dev/null 2>&1; then
  echo "legacy check failed: MTProto constructor IDs found in conn.go" >&2
  rg -n "$mtproto_ids" conn.go || true
  fail=1
fi

# Forbid removed global registry APIs outside their definition/tests.
if rg -n "tlrpc\\.Register(Constructor|Method)\\b|globalDispatcher" \
  --glob '!dispatcher.go' \
  --glob '!dispatcher_test.go' \
  --glob '!scripts/check_no_legacy.sh' \
  --glob '!**/*_test.go' \
  .; then
  echo "legacy check failed: removed global registry APIs are not allowed" >&2
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
