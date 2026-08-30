#!/usr/bin/env bash
set -euo pipefail

root_dir=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$root_dir"

if ! git diff --quiet || ! git diff --cached --quiet; then
  echo "ERROR: git working tree is dirty. Commit or stash changes before release." >&2
  exit 1
fi

cat <<'CHECKS'
Running release checks:
  gofmt verification
  go vet -p=1 ./...
  go test -p=1 -count=1 ./...
  go test -p=1 -tags=gotd_integ ./transport -run TestGotd -count=1
  architecture guard scripts
  make lint
  go test -p=1 ./compat -run Scenario -count=1 -timeout 60s
CHECKS

format_files=$(gofmt -l .)
if [[ -n "${format_files}" ]]; then
  echo "ERROR: gofmt required:" >&2
  echo "${format_files}" >&2
  exit 1
fi
go vet -p=1 ./...
go test -p=1 -count=1 ./...
go test -p=1 -tags=gotd_integ ./transport -run TestGotd -count=1
./scripts/check_no_legacy.sh
./scripts/check_type_domains.sh
./scripts/check_no_semantics_creep.sh
make lint
go test -p=1 ./compat -run Scenario -count=1 -timeout 60s

cat <<'NEXT'
Checks passed.

Next steps:
  git tag -a v0.8.0 -m "v0.8.0"
  git push origin v0.8.0
  Create GitHub Release using CHANGELOG.md notes.
NEXT
