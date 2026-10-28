#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEMO_ROOT="$(cd "${SCRIPT_DIR}/../.." && pwd)"
BACKEND_DIR="${DEMO_ROOT}/backend/api"
OUTPUT_ROOT="${DEMO_ROOT}/dist"
cd "$BACKEND_DIR"

target="${1:-all}"
target_arch="${2:-amd64}"
version="${VERSION:-$(git describe --tags --always --dirty 2>/dev/null || echo dev)}"
commit="${COMMIT:-$(git rev-parse --short HEAD 2>/dev/null || echo unknown)}"
build_time="${BUILD_TIME:-$(date -u +%Y-%m-%dT%H:%M:%SZ)}"

build_target() {
  local goos="$1"
  local goarch="$2"
  local extension=""
  if [[ "$goos" == "windows" ]]; then
    extension=".exe"
  fi

  local output_dir="${OUTPUT_ROOT}/${goos}-${goarch}"
  mkdir -p "$output_dir"
  echo "Building ${goos}/${goarch}..."

  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Version=${version} -X github.com/kael777/agentforge/backend/api/internal/buildinfo.Commit=${commit} -X github.com/kael777/agentforge/backend/api/internal/buildinfo.BuildTime=${build_time}" \
    -o "${output_dir}/api${extension}" ./cmd/api
  CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build -trimpath \
    -ldflags "-s -w" \
    -o "${output_dir}/migrate${extension}" ./cmd/migrate
}

case "$target" in
  all)
    build_target windows amd64
    build_target linux amd64
    build_target linux arm64
    build_target darwin amd64
    build_target darwin arm64
    ;;
  windows|linux|darwin)
    build_target "$target" "$target_arch"
    ;;
  *)
    echo "Unsupported target: $target" >&2
    echo "Usage: bash build.sh [all|windows|linux|darwin] [amd64|arm64]" >&2
    exit 2
    ;;
esac

echo "Build completed. Output: ${OUTPUT_ROOT}"
