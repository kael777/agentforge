#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
BACKEND_DIR="${ROOT_DIR}/backend/api"
FRONTEND_DIR="${ROOT_DIR}/frontend/web"
COMPOSE_FILE="${ROOT_DIR}/devops/docker/docker-compose.yml"

if [[ ! -f "${BACKEND_DIR}/.env" ]]; then
  cp "${BACKEND_DIR}/.env.example" "${BACKEND_DIR}/.env"
fi
if [[ ! -f "${FRONTEND_DIR}/.env" ]]; then
  cp "${FRONTEND_DIR}/.env.example" "${FRONTEND_DIR}/.env"
fi
if [[ ! -d "${FRONTEND_DIR}/node_modules" ]]; then
  (cd "${FRONTEND_DIR}" && npm ci)
fi

docker compose -p agentforge -f "${COMPOSE_FILE}" up -d --wait mysql
(cd "${BACKEND_DIR}" && go run ./cmd/migrate)

backend_pid=""
frontend_pid=""
cleanup() {
  [[ -n "${backend_pid}" ]] && kill "${backend_pid}" 2>/dev/null || true
  [[ -n "${frontend_pid}" ]] && kill "${frontend_pid}" 2>/dev/null || true
}
trap cleanup EXIT INT TERM

(cd "${BACKEND_DIR}" && go run ./cmd/api) &
backend_pid=$!
(cd "${FRONTEND_DIR}" && npm run dev) &
frontend_pid=$!

echo "Backend: http://localhost:55555"
echo "Frontend: http://localhost:5173"
echo "Press Ctrl+C to stop application processes; run 'make down' to stop MySQL."

wait "${backend_pid}" "${frontend_pid}"
