#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${APIHUB_ENV_FILE:-$project_root/.env}"
set -a
source "$env_file"
set +a
exec "$project_root/bin/apihub-init"
