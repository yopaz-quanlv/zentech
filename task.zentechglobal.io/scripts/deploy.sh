#!/usr/bin/env bash
set -euo pipefail

HOST="${HOST:-zentech}"
DOMAIN="${DOMAIN:-task.zentechglobal.io}"
SERVICE="${SERVICE:-task-zentechglobal.service}"
REMOTE_WEB_ROOT="${REMOTE_WEB_ROOT:-/var/www/task.zentechglobal.io}"
REMOTE_BIN="${REMOTE_BIN:-/opt/task-zentechglobal/bin/task-server}"
REMOTE_TMP_BASE="${REMOTE_TMP_BASE:-/tmp/task-zentechglobal-deploy}"

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
FRONTEND_DIR="$ROOT_DIR/frontend"
BACKEND_DIR="$ROOT_DIR/backend"
LOCAL_OUT="$ROOT_DIR/.deploy"
LOCAL_BIN="$LOCAL_OUT/task-server-linux-amd64"

MODE="deploy"
CONFIRM="0"
DRY_RUN="0"
SKIP_BUILD="0"

usage() {
  cat <<USAGE
Usage: $(basename "$0") [options]

Options:
  --yes             Actually deploy to production. Required unless --check, --dry-run, or --build-only is used.
  --check           Check current server/Nginx/service/health status only.
  --dry-run         Build and show/upload simulation with rsync dry-run; does not replace files or restart service.
  --build-only      Build frontend and backend locally only.
  --skip-build      Reuse existing frontend/dist and .deploy/task-server-linux-amd64.
  -h, --help        Show this help.

Environment overrides:
  HOST              SSH host. Default: zentech
  DOMAIN            Public domain. Default: task.zentechglobal.io
  SERVICE           systemd service. Default: task-zentechglobal.service
  REMOTE_WEB_ROOT   Static frontend root. Default: /var/www/task.zentechglobal.io
  REMOTE_BIN        Backend binary path. Default: /opt/task-zentechglobal/bin/task-server
USAGE
}

log() {
  printf '\n==> %s\n' "$*"
}

run() {
  printf '+ %q' "$1"
  shift || true
  printf ' %q' "$@"
  printf '\n'
  "$@"
}

ssh_run() {
  ssh "$HOST" "$@"
}

require_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "Missing required command: $1" >&2
    exit 1
  }
}

check_server() {
  log "Checking SSH, Nginx, service, and health endpoint on $HOST"
  ssh_run "hostname; date; nginx -t; systemctl is-active nginx; systemctl is-active $SERVICE; ss -ltnp | grep ':18082' || true"

  log "Checking public endpoints"
  curl -fsS "https://$DOMAIN/healthz"
  printf '\n'
  curl -fsSI "https://$DOMAIN/" | sed -n '1,8p'
}

build_local() {
  if [[ "$SKIP_BUILD" == "1" ]]; then
    [[ -d "$FRONTEND_DIR/dist" ]] || {
      echo "Missing $FRONTEND_DIR/dist; run without --skip-build first." >&2
      exit 1
    }
    [[ -x "$LOCAL_BIN" ]] || {
      echo "Missing $LOCAL_BIN; run without --skip-build first." >&2
      exit 1
    }
    return
  fi

  require_cmd npm
  require_cmd go

  local app_version
  app_version="${VITE_APP_VERSION:-$(git -C "$ROOT_DIR" rev-parse --short HEAD 2>/dev/null || date +%Y%m%d%H%M%S)}"

  log "Building frontend"
  (cd "$FRONTEND_DIR" && npm ci && VITE_APP_VERSION="$app_version" npm run build)

  log "Building Linux backend binary"
  mkdir -p "$LOCAL_OUT"
  (cd "$BACKEND_DIR" && GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o "$LOCAL_BIN" .)
  chmod 755 "$LOCAL_BIN"
}

deploy_remote() {
  require_cmd rsync
  require_cmd ssh
  require_cmd scp
  require_cmd curl

  local ts remote_tmp
  ts="$(date +%Y%m%d%H%M%S)"
  remote_tmp="$REMOTE_TMP_BASE-$ts"

  if [[ "$DRY_RUN" == "1" ]]; then
    log "Dry-run upload preview for frontend"
    rsync -azni --delete --human-readable "$FRONTEND_DIR/dist/" "$HOST:$REMOTE_WEB_ROOT/"

    log "Dry-run backend target"
    ssh_run "test -x '$REMOTE_BIN' && ls -lh '$REMOTE_BIN'; systemctl is-active '$SERVICE'"
    return
  fi

  if [[ "$CONFIRM" != "1" ]]; then
    echo "Refusing to deploy without --yes. Use --dry-run or --check for non-mutating checks." >&2
    exit 1
  fi

  log "Uploading release to $HOST:$remote_tmp"
  ssh_run "rm -rf '$remote_tmp' && mkdir -p '$remote_tmp/frontend' '$remote_tmp/bin'"
  rsync -az --delete "$FRONTEND_DIR/dist/" "$HOST:$remote_tmp/frontend/"
  scp "$LOCAL_BIN" "$HOST:$remote_tmp/bin/task-server"

  log "Installing release and restarting $SERVICE"
  ssh_run "
    set -euo pipefail
    ts='$ts'
    test -d '$remote_tmp/frontend'
    test -x '$remote_tmp/bin/task-server'
    cp '$REMOTE_BIN' '$REMOTE_BIN.bak-'\"\$ts\"
    install -m 755 '$remote_tmp/bin/task-server' '$REMOTE_BIN'
    rsync -a --delete '$remote_tmp/frontend/' '$REMOTE_WEB_ROOT/'
    systemctl restart '$SERVICE'
    systemctl --no-pager --full status '$SERVICE' | sed -n '1,30p'
    rm -rf '$remote_tmp'
  "

  log "Verifying public health"
  curl -fsS "https://$DOMAIN/healthz"
  printf '\n'
  curl -fsSI "https://$DOMAIN/" | sed -n '1,8p'
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --yes)
      CONFIRM="1"
      ;;
    --check)
      MODE="check"
      ;;
    --dry-run)
      DRY_RUN="1"
      ;;
    --build-only)
      MODE="build-only"
      ;;
    --skip-build)
      SKIP_BUILD="1"
      ;;
    -h|--help)
      usage
      exit 0
      ;;
    *)
      echo "Unknown option: $1" >&2
      usage >&2
      exit 1
      ;;
  esac
  shift
done

case "$MODE" in
  check)
    require_cmd ssh
    require_cmd curl
    check_server
    ;;
  build-only)
    build_local
    ;;
  deploy)
    build_local
    deploy_remote
    ;;
esac
