#!/usr/bin/env bash
set -euo pipefail

umask 077

project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
env_file="${APIHUB_ENV_FILE:-$project_root/.env}"
admin_password_file="$project_root/.admin-password"

if [[ -e "$env_file" ]]; then
  printf '配置文件已存在，未覆盖：%s\n' "$env_file" >&2
  exit 1
fi

if [[ -n "${APIHUB_DATABASE_PASSWORD_FILE:-}" ]]; then
  if [[ ! -r "$APIHUB_DATABASE_PASSWORD_FILE" ]]; then
    printf '数据库密码文件不可读：%s\n' "$APIHUB_DATABASE_PASSWORD_FILE" >&2
    exit 1
  fi
  IFS= read -r database_password < "$APIHUB_DATABASE_PASSWORD_FILE"
else
  if [[ ! -t 0 ]]; then
    printf '请在终端中运行此脚本，或通过 APIHUB_DATABASE_PASSWORD_FILE 指定本机密码文件。\n' >&2
    exit 1
  fi
  read -r -s -p '请输入 PostgreSQL 密码（输入内容不会显示）：' database_password
  printf '\n'
fi

if [[ -z "$database_password" ]]; then
  printf 'PostgreSQL 密码不能为空。\n' >&2
  exit 1
fi

database_host="${APIHUB_DATABASE_HOST:-127.0.0.1}"
database_port="${APIHUB_DATABASE_PORT:-5432}"
database_name="${APIHUB_DATABASE_NAME:-apihub}"
database_user="${APIHUB_DATABASE_USER:-apihub}"

password_file="$(mktemp)"
config_file="$(mktemp "${env_file}.tmp.XXXXXX")"
cleanup() {
  rm -f "$password_file" "$config_file"
  unset database_password
}
trap cleanup EXIT

# libpq 密码文件中的反斜杠和冒号需要转义。
pgpass_password="${database_password//\\/\\\\}"
pgpass_password="${pgpass_password//:/\\:}"
printf '%s:%s:%s:%s:%s\n' "$database_host" "$database_port" "$database_name" "$database_user" "$pgpass_password" > "$password_file"
chmod 600 "$password_file"

printf '正在验证 PostgreSQL 连接...\n'
if ! PGPASSFILE="$password_file" PGCONNECT_TIMEOUT=5 psql \
  -h "$database_host" \
  -p "$database_port" \
  -U "$database_user" \
  -d "$database_name" \
  -Atqc 'SELECT 1' >/dev/null; then
  printf 'PostgreSQL 连接失败，未创建配置文件。请检查密码和数据库地址。\n' >&2
  exit 1
fi

encode_uri_component() {
  local input="$1"
  local output=""
  local character
  local index
  local hex
  LC_ALL=C
  for ((index = 0; index < ${#input}; index++)); do
    character="${input:index:1}"
    case "$character" in
      [a-zA-Z0-9.~_-]) output+="$character" ;;
      *)
        printf -v hex '%02X' "'$character"
        output+="%$hex"
        ;;
    esac
  done
  printf '%s' "$output"
}

database_password_encoded="$(encode_uri_component "$database_password")"
admin_password="$(openssl rand -hex 16)"
admin_password_hash="$(printf '%s' "$admin_password" | (cd "$project_root" && go run ./cmd/apihub-password))"
encryption_key="$(openssl rand -base64 32 | tr -d '\n')"
database_url="postgres://${database_user}:${database_password_encoded}@${database_host}:${database_port}/${database_name}?sslmode=disable"

{
  printf 'APIHUB_ADDRESS=%q\n' ':8080'
  printf 'APIHUB_DATABASE_URL=%q\n' "$database_url"
  printf 'APIHUB_REDIS_ADDR=%q\n' '127.0.0.1:6379'
  printf 'APIHUB_REDIS_PASSWORD=%q\n' ''
  printf 'APIHUB_REDIS_DB=%q\n' '0'
  printf 'APIHUB_ADMIN_PASSWORD_HASH=%q\n' "$admin_password_hash"
  printf 'APIHUB_ENCRYPTION_KEY=%q\n' "$encryption_key"
  printf 'APIHUB_WORKSPACE_SLUG=%q\n' 'default'
  printf 'APIHUB_WORKSPACE_NAME=%q\n' 'APIHub'
  printf 'APIHUB_ADMIN_EMAIL=%q\n' 'admin@localhost'
  printf 'APIHUB_PUBLIC_BASE_URL=%q\n' 'http://127.0.0.1:8080'
  printf 'APIHUB_ROLE=%q\n' 'all'
} > "$config_file"

chmod 600 "$config_file"
mv "$config_file" "$env_file"
printf '%s\n' "$admin_password" > "$admin_password_file"
chmod 600 "$admin_password_file"

unset database_password database_password_encoded database_url admin_password admin_password_hash encryption_key
printf '配置已创建：%s\n' "$env_file"
printf '管理员初始密码已保存：%s\n' "$admin_password_file"
printf '下一步运行：%s/scripts/build.sh，然后 scripts/init.sh 和 scripts/start.sh\n' "$project_root"
