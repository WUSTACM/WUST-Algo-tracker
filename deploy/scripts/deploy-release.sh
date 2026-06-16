#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${script_dir}/lib.sh"

load_env

export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:${PATH}"

FRONTEND_DIR="${FRONTEND_DIR:-${APP_ROOT}/frontend}"
BACKUP_ROOT="${BACKUP_ROOT:-${APP_ROOT}/backups}"
BACKUP_RETENTION_DAYS="${BACKUP_RETENTION_DAYS:-14}"
BACKUP_KEEP_RECENT="${BACKUP_KEEP_RECENT:-10}"
BACKUP_MAX_SIZE_MB="${BACKUP_MAX_SIZE_MB:-2048}"
BACKUP_EMERGENCY_KEEP_RECENT="${BACKUP_EMERGENCY_KEEP_RECENT:-5}"
HEALTH_URL="${HEALTH_URL:-http://${DOMAIN:-127.0.0.1}:${NGINX_PORT:-8088}/}"
STAMP="$(date +%Y%m%d%H%M%S)"
BACKUP_DIR="${BACKUP_ROOT}/release-${STAMP}"

echo "Running preflight checks..."
bash "${script_dir}/preflight.sh"

require_command curl
require_command docker
require_command envsubst
require_command go
require_command nginx
require_command npm
require_command psql
require_command sudo
require_command systemctl

if [[ -n "${SUDO_PASSWORD:-}" ]]; then
  printf "%s\n" "${SUDO_PASSWORD}" | sudo -S -v >/dev/null
else
  sudo -v
fi

if [[ ! -d "${FRONTEND_DIR}" ]]; then
  echo "Frontend directory not found: ${FRONTEND_DIR}"
  echo "Set FRONTEND_DIR=/path/to/WUST-Algo-Frontend or keep the default ${APP_ROOT}/frontend."
  exit 1
fi

backup_path() {
  local src="$1"
  local dst="$2"
  if [[ -e "${src}" ]]; then
    mkdir -p "$(dirname "${dst}")"
    cp -a "${src}" "${dst}"
  fi
}

cleanup_backups() {
  local -a release_backups frontend_backups
  local index path size_mb

  if [[ ! -d "${BACKUP_ROOT}" ]]; then
    return
  fi

  mapfile -t release_backups < <(find "${BACKUP_ROOT}" -maxdepth 1 -mindepth 1 -type d -name 'release-*' -printf '%T@ %p\n' | sort -rn | cut -d' ' -f2-)
  index=0
  for path in "${release_backups[@]}"; do
    index=$((index + 1))
    if (( index > BACKUP_KEEP_RECENT )) && find "${path}" -maxdepth 0 -mtime "+${BACKUP_RETENTION_DAYS}" | grep -q .; then
      echo "Removing expired release backup: ${path}"
      rm -rf -- "${path}"
    fi
  done

  mapfile -t frontend_backups < <(find "${BACKUP_ROOT}" -maxdepth 1 -type f -name 'frontend-dist-*.tgz' -printf '%T@ %p\n' | sort -rn | cut -d' ' -f2-)
  index=0
  for path in "${frontend_backups[@]}"; do
    index=$((index + 1))
    if (( index > BACKUP_KEEP_RECENT )) && find "${path}" -maxdepth 0 -mtime "+${BACKUP_RETENTION_DAYS}" | grep -q .; then
      echo "Removing expired frontend backup: ${path}"
      rm -f -- "${path}"
    fi
  done

  size_mb="$(du -sm "${BACKUP_ROOT}" | awk '{print $1}')"
  if (( size_mb <= BACKUP_MAX_SIZE_MB )); then
    return
  fi

  echo "WARNING: backup directory uses ${size_mb}MiB, over limit ${BACKUP_MAX_SIZE_MB}MiB."
  echo "WARNING: removing older backups while keeping at least ${BACKUP_EMERGENCY_KEEP_RECENT} recent release/frontend backups."

  {
    index=0
    for path in "${release_backups[@]}"; do
      index=$((index + 1))
      if (( index > BACKUP_EMERGENCY_KEEP_RECENT )) && [[ -e "${path}" ]]; then
        find "${path}" -maxdepth 0 -printf '%T@ %p\n'
      fi
    done

    index=0
    for path in "${frontend_backups[@]}"; do
      index=$((index + 1))
      if (( index > BACKUP_EMERGENCY_KEEP_RECENT )) && [[ -e "${path}" ]]; then
        find "${path}" -maxdepth 0 -printf '%T@ %p\n'
      fi
    done
  } | sort -n | while read -r _ path; do
    size_mb="$(du -sm "${BACKUP_ROOT}" | awk '{print $1}')"
    if (( size_mb <= BACKUP_MAX_SIZE_MB )); then
      break
    fi
    echo "WARNING: removing backup to reduce disk usage: ${path}"
    rm -rf -- "${path}"
  done
}

echo "Creating release backup at ${BACKUP_DIR}..."
mkdir -p "${BACKUP_DIR}/tracker" "${BACKUP_DIR}/frontend" "${BACKUP_DIR}/systemd" "${BACKUP_DIR}/nginx"
backup_path "${APP_ROOT}/bin" "${BACKUP_DIR}/bin"
backup_path "${APP_ROOT}/conf" "${BACKUP_DIR}/conf"
backup_path "${APP_ROOT}/infra/docker-compose.yml" "${BACKUP_DIR}/infra/docker-compose.yml"
backup_path "${FRONTEND_DIR}/dist" "${BACKUP_DIR}/frontend/dist"
backup_path "/etc/nginx/sites-available/${NGINX_SITE_NAME:-wust-algo}" "${BACKUP_DIR}/nginx/${NGINX_SITE_NAME:-wust-algo}"
for unit in wust-user.service wust-core-data.service wust-gateway.service wust-agent.service wust-db-backup.service wust-db-backup.timer; do
  backup_path "/etc/systemd/system/${unit}" "${BACKUP_DIR}/systemd/${unit}"
done

echo "Deploying backend services..."
bash "${script_dir}/deploy-backend.sh"

echo "Deploying frontend..."
(
  cd "${FRONTEND_DIR}"
  bash deploy/scripts/deploy-frontend.sh
)

echo "Running health checks..."
run_sudo systemctl --no-pager --full status wust-user.service wust-core-data.service wust-gateway.service >/dev/null
if [[ "${ENABLE_AGENT}" == "1" ]]; then
  run_sudo systemctl --no-pager --full status wust-agent.service >/dev/null
fi
run_sudo nginx -t >/dev/null
curl -fsS --max-time 10 -o /dev/null "${HEALTH_URL}"

echo "Release deployment finished."
echo "Backup: ${BACKUP_DIR}"
echo "Health URL: ${HEALTH_URL}"
echo "Cleaning old backups under ${BACKUP_ROOT}..."
cleanup_backups
