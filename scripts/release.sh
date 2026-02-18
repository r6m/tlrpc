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
  gofmt -w .
  go vet ./...
  go test -count=1 ./...
  make lint
  go test ./compat -run Scenario -count=1 -timeout 60s
CHECKS


gofmt -w .
go vet ./...
go test -count=1 ./...
make lint
go test ./compat -run Scenario -count=1 -timeout 60s

cat <<'NEXT'
Checks passed.

Next steps:
  git tag -a v0.1.0 -m "v0.1.0"
  git push origin v0.1.0
  Create GitHub Release using CHANGELOG.md notes.
NEXT
