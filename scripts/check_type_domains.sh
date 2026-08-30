#!/usr/bin/env sh
set -eu

# MTProto wire/control ownership belongs under mtproto and internal/runtime.
if [ -e conn.go ]; then
  echo "type-domain check failed: legacy conn.go exists"
  exit 1
fi

echo "type-domain check passed"

if [ -d "gen" ]; then
  if rg -n "msg_container|msgs_ack|gzip_packed|rpc_result|rpc_drop_answer|rpc_answer_unknown|rpc_answer_dropped|msg_resend_req|msgs_state_req|msgs_state_info" gen >/dev/null 2>&1; then
    echo "type-domain check failed: MTProto envelope objects found in generated API package"
    rg -n "msg_container|msgs_ack|gzip_packed|rpc_result|rpc_drop_answer|rpc_answer_unknown|rpc_answer_dropped|msg_resend_req|msgs_state_req|msgs_state_info" gen || true
    exit 1
  fi
fi
