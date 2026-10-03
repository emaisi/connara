#!/bin/bash
# Fixed Linux launcher: requires systemd user manager and bubblewrap at the
# administrator-installed paths. No user code or request options are arguments.
set -euo pipefail
if [[ ${1:-} == --inside ]]; then
  [[ $# == 2 && $2 == /* ]] || exit 64
  cgroup_path="$(/usr/bin/awk -F: '$1 == "0" { print $3 }' /proc/self/cgroup)"
  [[ $cgroup_path == /user.slice/* ]] || exit 70
  exec /usr/bin/bwrap --unshare-all --die-with-parent --new-session --uid 65534 --gid 65534 \
    --clearenv --setenv TZ UTC --ro-bind "$2" /worker --proc /proc --dir /limits \
    --ro-bind "/sys/fs/cgroup$cgroup_path/memory.max" /limits/memory.max \
    --ro-bind "/sys/fs/cgroup$cgroup_path/memory.swap.max" /limits/memory.swap.max \
    --chdir / -- /worker
fi
[[ $# == 1 && $1 == /* && -x $1 ]] || exit 64
export XDG_RUNTIME_DIR="/run/user/$(/usr/bin/id -u)"
export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"
exec /usr/bin/systemd-run --user --quiet --pipe --wait --collect --unit="connara-code-$$" \
  -p MemoryMax=128M -p MemorySwapMax=0 -p TasksMax=32 -p NoNewPrivileges=yes \
  -p KillMode=control-group -p RuntimeMaxSec=2s -p TimeoutStopSec=100ms \
  /bin/bash "${BASH_SOURCE[0]}" --inside "$1"
