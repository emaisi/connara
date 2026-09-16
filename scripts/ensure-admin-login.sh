#!/usr/bin/env bash
set -euo pipefail

umask 077

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${APIHUB_ENV_FILE:-$project_root/.env}"
password_file="$project_root/.admin-password"

if grep -q '^APIHUB_ADMIN_PASSWORD_HASH=.' "$env_file"; then
  exit 0
fi

admin_password="$(openssl rand -hex 16)"
admin_password_hash="$(printf '%s' "$admin_password" | (cd "$project_root" && go run ./cmd/apihub-password))"
config_file="$(mktemp "${env_file}.tmp.XXXXXX")"
cleanup() {
  rm -f "$config_file"
  unset admin_password admin_password_hash
}
trap cleanup EXIT

awk '!/^APIHUB_ADMIN_TOKEN=/ && !/^APIHUB_ADMIN_PASSWORD_HASH=/' "$env_file" > "$config_file"
printf 'APIHUB_ADMIN_PASSWORD_HASH=%q\n' "$admin_password_hash" >> "$config_file"
chmod 600 "$config_file"
mv "$config_file" "$env_file"
printf '%s\n' "$admin_password" > "$password_file"
chmod 600 "$password_file"
rm -f "$project_root/.admin-token"

unset admin_password admin_password_hash
printf '管理端已切换为账号密码登录。初始密码保存在：%s\n' "$password_file"
