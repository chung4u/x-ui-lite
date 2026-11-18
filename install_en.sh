#!/usr/bin/env bash
# Compatibility entry point for the maintained X-UI Lite installer.
set -euo pipefail
dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
if [[ -f "$dir/scripts/install.sh" && -f "$dir/config/version" ]]; then
    exec bash "$dir/scripts/install.sh" "$@"
fi
installer="$(mktemp)"
trap 'rm -f "$installer"' EXIT
curl -fsSL --retry 3 "https://github.com/${XUI_REPOSITORY:-chung4u/x-ui-lite}/releases/latest/download/install.sh" -o "$installer"
bash "$installer" "$@"
