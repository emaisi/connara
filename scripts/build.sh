#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$project_root"
npm --prefix web run build
mkdir -p bin
for command in apihub apihub-init apihub-password apihub-rotate-keys; do
  go build -trimpath -o "bin/$command" "./cmd/$command"
done
