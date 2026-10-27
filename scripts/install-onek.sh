#!/usr/bin/env bash
# The CLI is now onkt; this name stays so existing workflows keep working.
set -euo pipefail
exec "$(dirname "$0")/install-onkt.sh" "$@"
