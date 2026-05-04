#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "${ROOT}/s-matrix-backend"
go test ./...
go build -o "${ROOT}/scripts/s-matrix" ./cmd/server
chmod +x "${ROOT}/scripts/s-matrix"
echo "Built ${ROOT}/scripts/s-matrix"
