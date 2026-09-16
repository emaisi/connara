#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"
go vet ./...
go test -race ./...
npm --prefix web run check
if [[ "${APIHUB_VULN_SCAN:-0}" == 1 ]]; then
  GOTOOLCHAIN="$(go env GOVERSION)" go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  npm --prefix web audit --registry=https://registry.npmjs.org
fi
