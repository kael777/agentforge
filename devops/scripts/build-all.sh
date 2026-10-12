#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

bash "${ROOT_DIR}/devops/scripts/build.sh"

(
  cd "${ROOT_DIR}/frontend/web"
  npm run build
)

echo "Full-stack build completed."
