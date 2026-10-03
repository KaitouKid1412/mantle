#!/usr/bin/env bash
# Install mantle from this checkout (make install):
#   - the launcher into ~/.mantle/bin/mantle, linked from ~/.local/bin/mantle;
#   - the private source clone ~/.mantle/src (origin = this checkout, branch user);
#   - a mantle-ui build from user, installed as a version and made current.
# Re-running is idempotent. Flags are passed to the installer:
#   scripts/install.sh -branch main -link-dir ~/.local/bin
# MANTLE_HOME overrides ~/.mantle.
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

if ! command -v go >/dev/null 2>&1; then
  cat >&2 <<'EOF'
mantle needs a Go toolchain (1.27 or newer): it builds itself, and /mantle
rebuilds it from source.
Install Go from https://go.dev/dl/ (or `brew install go`), then run `make install` again.
EOF
  exit 1
fi
if ! command -v git >/dev/null 2>&1; then
  echo "mantle needs git: install it, then run \`make install\` again." >&2
  exit 1
fi

exec go run ./internal/selfmod/cmd/mantle-install "$@"
