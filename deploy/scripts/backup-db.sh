#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${script_dir}/lib.sh"

load_env

require_command find
require_command pg_dump
require_command pg_restore
require_command sha256sum
require_command stat

DB_BACKUP_DIR="${DB_BACKUP_DIR:-${APP_ROOT}/backups/database}"
DB_BACKUP_RETENTION_DAYS="${DB_BACKUP_RETENTION_DAYS:-14}"
DB_BACKUP_KEEP_RECENT="${DB_BACKUP_KEEP_RECENT:-30}"

if [[ "${DB_BACKUP_KEEP_RECENT}" =~ ^[0-9]+$ ]] && (( DB_BACKUP_KEEP_RECENT > 0 )); then
  :
else
  echo "DB_BACKUP_KEEP_RECENT must be a positive integer"
  exit 1
fi

if [[ "${DB_BACKUP_RETENTION_DAYS}" =~ ^[0-9]+$ ]]; then
  :
else
  echo "DB_BACKUP_RETENTION_DAYS must be a non-negative integer"
  exit 1
fi

timestamp="$(date +%Y%m%d%H%M%S)"
backup_dir="${DB_BACKUP_DIR}/db-${timestamp}"

umask 077
mkdir -p "${backup_dir}"

dump_database() {
  local db_name="$1"
  local dump_file="${backup_dir}/${db_name}.dump"

  echo "Dumping ${db_name}..."
  PGPASSWORD="${POSTGRES_PASSWORD}" pg_dump \
    -h "${POSTGRES_HOST}" \
    -p "${POSTGRES_PORT}" \
    -U "${POSTGRES_USER}" \
    --format=custom \
    --no-owner \
    --no-privileges \
    -d "${db_name}" \
    -f "${dump_file}"

  pg_restore --list "${dump_file}" >/dev/null
  sha256sum "${dump_file}" >> "${backup_dir}/SHA256SUMS"
}

dump_database "${POSTGRES_USER_DB}"
dump_database "${POSTGRES_CORE_DB}"

ln -sfn "${backup_dir}" "${DB_BACKUP_DIR}/latest"

mapfile -t backup_dirs < <(find "${DB_BACKUP_DIR}" -mindepth 1 -maxdepth 1 -type d -name 'db-*' -printf '%T@ %p\n' | sort -rn | cut -d' ' -f2-)
now="$(date +%s)"
index=0
for dir in "${backup_dirs[@]}"; do
  index=$((index + 1))
  if (( index <= DB_BACKUP_KEEP_RECENT )); then
    continue
  fi
  mtime="$(stat -c %Y "${dir}")"
  age_days=$(((now - mtime) / 86400))
  if (( age_days >= DB_BACKUP_RETENTION_DAYS )); then
    echo "Removing expired database backup: ${dir}"
    rm -rf -- "${dir}"
  fi
done

echo "Database backup finished: ${backup_dir}"
