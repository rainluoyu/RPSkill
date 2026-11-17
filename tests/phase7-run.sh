#!/usr/bin/env bash
# Engineering checks only: no real model or owner inbox receives a message.
set -euo pipefail
cd "$(dirname "${BASH_SOURCE[0]}")/.."
make build
go test ./...
go vet ./...
npm --prefix web test
if [[ -n "${RPS_P7_RELEASE:-}" ]]; then
  go run ./scripts/release --verify "$RPS_P7_RELEASE"
  go run ./scripts/release --smoke "$RPS_P7_RELEASE"
fi
printf '%s\n' 'Engineering checks passed; real launcher conversation and native non-Linux acceptance are separate.'
