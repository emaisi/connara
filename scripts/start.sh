#!/usr/bin/env bash
set -euo pipefail

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${APIHUB_ENV_FILE:-$project_root/.env}"

if [[ ! -f "$env_file" ]]; then
  printf '缺少配置文件：%s\n请先在终端运行 %s/scripts/configure.sh，安全生成本地配置。\n' "$env_file" "$project_root" >&2
  exit 1
fi



set -a
# shellcheck disable=SC1090
source "$env_file"
set +a

cd "$project_root"
exec "$project_root/bin/apihub"
