#!/usr/bin/env bash
set -euo pipefail

CFG="${ALFRED_CONFIG:-/config/config.json}"
RCLONE_CFG="/tmp/rclone-alfred.conf"

mount_webdav_if_enabled() {
  [ -f "$CFG" ] || return 0
  local enabled url user pass mpath
  enabled=$(jq -r '.webdav_mount.enabled // false' "$CFG" 2>/dev/null || echo "false")
  [ "$enabled" = "true" ] || return 0

  url=$(jq -r '.webdav_mount.url // ""' "$CFG")
  user=$(jq -r '.webdav_mount.user // ""' "$CFG")
  pass=$(jq -r '.webdav_mount.pass // ""' "$CFG")
  mpath=$(jq -r '.webdav_mount.mount_path // "/host/mount/library"' "$CFG")

  if [ -z "$url" ]; then
    echo "[entrypoint] webdav_mount.enabled=true but url empty; skipping mount"
    return 0
  fi
  if [[ "$mpath" != /host/* ]]; then
    echo "[entrypoint] mount_path must be under /host for host visibility (got: $mpath)"
    exit 1
  fi

  mkdir -p "$mpath"
  pkill -f "rclone mount alfredwebdav:" >/dev/null 2>&1 || true

  {
    echo "[alfredwebdav]"
    echo "type = webdav"
    echo "url = $url"
    echo "vendor = other"
    [ -n "$user" ] && echo "user = $user"
    if [ -n "$pass" ]; then
      echo "pass = $(rclone obscure "$pass")"
    fi
  } > "$RCLONE_CFG"

  rclone --config "$RCLONE_CFG" mount alfredwebdav: "$mpath" \
    --allow-other \
    --default-permissions \
    --umask 002 \
    --uid 99 \
    --gid 100 \
    --dir-cache-time 10m \
    --vfs-cache-mode full \
    --vfs-cache-max-size 50G \
    --vfs-read-ahead 256M \
    --buffer-size 64M \
    --vfs-cache-poll-interval 1m \
    --daemon

  sleep 1
  mountpoint -q "$mpath" || { echo "[entrypoint] rclone mount failed at $mpath"; exit 1; }
  echo "[entrypoint] webdav mounted at $mpath from $url (host-visible via bind propagation)"
}

mount_webdav_if_enabled
exec /usr/local/bin/alfrededr -config "$CFG"
