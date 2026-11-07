#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

BACKEND_PID=""
FRONTEND_PID=""

cleanup() {
  echo ""
  echo "Encerrando frontend e backend..."
  if [[ -n "$FRONTEND_PID" ]] && kill -0 "$FRONTEND_PID" 2>/dev/null; then
    kill "$FRONTEND_PID" 2>/dev/null || true
  fi
  if [[ -n "$BACKEND_PID" ]] && kill -0 "$BACKEND_PID" 2>/dev/null; then
    kill "$BACKEND_PID" 2>/dev/null || true
  fi
  wait 2>/dev/null || true
}

trap cleanup SIGINT SIGTERM EXIT

if command -v docker >/dev/null 2>&1; then
  echo "[db] Garantindo que o PostgreSQL está ativo..."
  docker compose up -d postgres
fi

echo "[back] Iniciando backend Go em http://127.0.0.1:8080 ..."
go run ./cmd/api &
BACKEND_PID=$!

# Aguarda o backend responder em /readyz (até 15s) antes de subir o Vite
for _ in {1..30}; do
  if ! kill -0 "$BACKEND_PID" 2>/dev/null; then
    echo "[back] O processo do backend encerrou inesperadamente."
    exit 1
  fi
  if curl -fsS http://127.0.0.1:8080/readyz >/dev/null 2>&1; then
    echo "[back] Backend pronto!"
    break
  fi
  sleep 0.5
done

echo "[front] Iniciando frontend Vite em http://127.0.0.1:5173 ..."
npm --prefix frontend run dev &
FRONTEND_PID=$!

wait -n "$BACKEND_PID" "$FRONTEND_PID"

