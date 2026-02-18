#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

fail=0

fail_msg() {
  echo "ERROR: $1" >&2
  fail=1
}

# Disallow semantic server subsystems.
for dir in \
  "internal/messages" \
  "internal/storage" \
  "internal/migrations" \
  "migrations" \
  "storage" \
  "db" \
  "database"; do
  if [[ -d "${ROOT_DIR}/${dir}" ]]; then
    fail_msg "semantic storage directory not allowed: ${dir}"
  fi
done

# Guard compat-server registrations to the minimal allowlist.
allowed_servers=(
  "RegisterHelpServer"
  "RegisterUpdatesServer"
  "RegisterUsersServer"
  "RegisterAuthServer"
)

compat_server_file="${ROOT_DIR}/cmd/compat-server/main.go"
if [[ -f "${compat_server_file}" ]]; then
  registrations=$(rg -n "Register[A-Za-z0-9]+Server" "${compat_server_file}" | sed -E 's/.*(Register[A-Za-z0-9]+Server).*/\1/' | sort -u)
  while IFS= read -r reg; do
    [[ -z "${reg}" ]] && continue
    allowed=false
    for ok in "${allowed_servers[@]}"; do
      if [[ "${reg}" == "${ok}" ]]; then
        allowed=true
        break
      fi
    done
    if [[ "${allowed}" == "false" ]]; then
      fail_msg "compat-server registers non-minimal service: ${reg}"
    fi
  done <<< "${registrations}"
fi

# Guard against messaging semantics in compat server/tests.
if rg -n "Messages(GetDialogs|GetHistory|SendMessage)Request|messages\.(getDialogs|getHistory|sendMessage)" \
  "${ROOT_DIR}/cmd/compat-server" "${ROOT_DIR}/compat" -g"*.go" > /dev/null; then
  fail_msg "messaging semantics detected in compat code (messages.*)"
fi

if [[ "${fail}" -ne 0 ]]; then
  exit 1
fi
