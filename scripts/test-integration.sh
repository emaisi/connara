#!/usr/bin/env bash
set -euo pipefail
project_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
pg_bin="${APIHUB_TEST_PG_BIN:-$(pg_config --bindir)}"
for tool in initdb pg_ctl; do [[ -x "$pg_bin/$tool" ]] || { echo "Missing $pg_bin/$tool; set APIHUB_TEST_PG_BIN" >&2; exit 1; }; done
work_dir="$(mktemp -d /tmp/apihub-integration.XXXXXX)"
read -r pg_port redis_port < <(python3 - <<'PYPORT'
import socket
with socket.socket() as pg, socket.socket() as redis:
    pg.bind(('127.0.0.1',0)); redis.bind(('127.0.0.1',0))
    print(pg.getsockname()[1],redis.getsockname()[1])
PYPORT
)
cleanup() {
  [[ ! -f "$work_dir/redis.pid" ]] || kill "$(cat "$work_dir/redis.pid")" 2>/dev/null || true
  "$pg_bin/pg_ctl" -D "$work_dir/pg" -m immediate stop >/dev/null 2>&1 || true
  rm -rf "$work_dir"
}
trap cleanup EXIT INT TERM
"$pg_bin/initdb" -D "$work_dir/pg" -A trust -U apihub_test --no-locale --encoding=UTF8 >/dev/null
"$pg_bin/pg_ctl" -D "$work_dir/pg" -l "$work_dir/pg.log" -o "-h 127.0.0.1 -p $pg_port -k $work_dir" start >/dev/null
redis-server --bind 127.0.0.1 --port "$redis_port" --save '' --appendonly no --daemonize yes --dir "$work_dir" --pidfile "$work_dir/redis.pid" --logfile "$work_dir/redis.log"
export APIHUB_TEST_DATABASE_URL="postgres://apihub_test@127.0.0.1:$pg_port/postgres?sslmode=disable"
export APIHUB_TEST_REDIS_ADDR="127.0.0.1:$redis_port"
cd "$project_root"
go test -race -count=1 ./...
