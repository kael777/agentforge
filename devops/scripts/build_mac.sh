#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
machine="$(uname -m)"
case "$machine" in
  arm64|aarch64) arch="arm64" ;;
  *) arch="amd64" ;;
esac

exec bash "${ROOT_DIR}/build.sh" darwin "${1:-$arch}"
