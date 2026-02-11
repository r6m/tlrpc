#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FIXTURES_DIR="$ROOT_DIR/testdata/fixtures"

mkdir -p "$FIXTURES_DIR"

cat > "$FIXTURES_DIR/README.txt" <<'EOF'
This directory is populated by scripts/setup-testdata.sh.
Add fixture generation steps here as schemas are added.
EOF
