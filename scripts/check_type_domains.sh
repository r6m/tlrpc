#!/usr/bin/env sh
set -eu

# MTProto IDs that should not be hardcoded in conn.go after typed split.
pattern='0x73f1f8dc|0x62d6b459|0x3072cfa1|0xf35c6d01|0x2144ca19|0x7d861a08|0xda69fb52|0x04deb57d'

if rg -n "$pattern" conn.go >/dev/null 2>&1; then
  echo "type-domain check failed: MTProto constructor IDs found in conn.go"
  rg -n "$pattern" conn.go || true
  exit 1
fi

echo "type-domain check passed"

if [ -d "gen" ]; then
  if rg -n "msg_container|msgs_ack|gzip_packed|rpc_result|msg_resend_req|msgs_state_req|msgs_state_info" gen >/dev/null 2>&1; then
    echo "type-domain check failed: MTProto envelope objects found in generated API package"
    rg -n "msg_container|msgs_ack|gzip_packed|rpc_result|msg_resend_req|msgs_state_req|msgs_state_info" gen || true
    exit 1
  fi
fi
