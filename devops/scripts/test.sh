#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

(
  cd "${ROOT_DIR}/backend/gofiber-gorm-next"
  go test ./...
)

(
  cd "${ROOT_DIR}/frontend/web"
  npm run test
)
