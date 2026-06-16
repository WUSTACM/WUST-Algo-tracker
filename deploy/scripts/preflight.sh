#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
# shellcheck source=lib.sh
source "${script_dir}/lib.sh"

load_env

FRONTEND_DIR="${FRONTEND_DIR:-${APP_ROOT}/frontend}"

errors=0
warnings=0

fail() {
  echo "ERROR: $*"
  errors=$((errors + 1))
}

warn() {
  echo "WARN: $*"
  warnings=$((warnings + 1))
}

ok() {
  echo "OK: $*"
}

check_command() {
  local name="$1"
  if command -v "$name" >/dev/null 2>&1; then
    ok "command ${name}: $(command -v "$name")"
  else
    fail "missing command: ${name}"
  fi
}

require_env_value() {
  local name="$1"
  if [[ -z "${!name:-}" ]]; then
    fail "missing required env: ${name}"
  fi
}

warn_placeholder() {
  local name="$1"
  local value="${!name:-}"
  if [[ "${value}" == replace-with-* || "${value}" == "change-me" || "${value}" == "changeme" ]]; then
    warn "${name} still looks like a placeholder"
  fi
}

check_writable_dir() {
  local dir="$1"
  local label="$2"
  if [[ -d "${dir}" ]]; then
    if [[ -w "${dir}" ]]; then
      ok "${label} is writable: ${dir}"
    else
      fail "${label} is not writable by $(id -un): ${dir}"
    fi
    return
  fi

  local parent
  parent="$(dirname "${dir}")"
  if [[ -d "${parent}" && -w "${parent}" ]]; then
    warn "${label} does not exist yet but parent is writable: ${dir}"
  else
    fail "${label} does not exist and parent is not writable: ${dir}"
  fi
}

check_port_hint() {
  local label="$1"
  local addr="$2"
  local port="${addr##*:}"
  if [[ -z "${port}" || "${port}" == "${addr}" ]]; then
    warn "cannot parse port for ${label}: ${addr}"
    return
  fi
  if command -v ss >/dev/null 2>&1 && ss -ltn "( sport = :${port} )" | tail -n +2 | grep -q .; then
    warn "${label} port ${port} is already listening; this is expected during redeploys"
  fi
}

echo "Running WUST Algo backend preflight..."
echo "Repository: ${repo_dir}"
echo "Deploy dir: ${deploy_dir}"
echo "App root: ${APP_ROOT:-<unset>}"

for cmd in bash curl docker envsubst go npm pg_dump pg_restore psql sudo systemctl; do
  check_command "${cmd}"
done

if command -v docker >/dev/null 2>&1; then
  if docker compose version >/dev/null 2>&1; then
    ok "docker compose is available"
  else
    fail "docker compose plugin is not available"
  fi
fi

if command -v nginx >/dev/null 2>&1; then
  ok "command nginx: $(command -v nginx)"
else
  warn "nginx is not installed or not in PATH; release health checks may fail on frontend hosts"
fi

required_env=(
  APP_ROOT
  APP_USER
  POSTGRES_HOST
  POSTGRES_PORT
  POSTGRES_USER
  POSTGRES_PASSWORD
  POSTGRES_USER_DB
  POSTGRES_CORE_DB
  REDIS_HOST
  REDIS_PORT
  REDIS_PASSWORD
  RABBITMQ_HOST
  RABBITMQ_PORT
  RABBITMQ_MANAGEMENT_PORT
  RABBITMQ_USER
  RABBITMQ_PASSWORD
  RABBITMQ_VHOST
  CONSUL_HOST
  CONSUL_PORT
  USER_HTTP_ADDR
  USER_GRPC_ADDR
  CORE_HTTP_ADDR
  CORE_GRPC_ADDR
  AGENT_HTTP_ADDR
  AGENT_GRPC_ADDR
  GATEWAY_ADDR
  JWT_SECRET
  ENABLE_AGENT
)

for name in "${required_env[@]}"; do
  require_env_value "${name}"
done

if [[ -n "${JWT_SECRET:-}" ]]; then
  if (( ${#JWT_SECRET} < 32 )); then
    fail "JWT_SECRET must be at least 32 characters"
  fi
  warn_placeholder JWT_SECRET
fi

case "${ENABLE_AGENT:-}" in
  0 | 1) ok "ENABLE_AGENT=${ENABLE_AGENT}" ;;
  *) fail "ENABLE_AGENT must be 0 or 1, got ${ENABLE_AGENT:-<unset>}" ;;
esac

if [[ "${ENABLE_AGENT:-0}" == "1" ]]; then
  for name in AI_BASE_URL AI_MODEL AI_API_KEY SMTP_HOST SMTP_PORT SMTP_USERNAME SMTP_PASSWORD SMTP_FROM; do
    require_env_value "${name}"
    warn_placeholder "${name}"
  done
else
  warn "agent service is disabled; AI summary and email delivery will be skipped"
fi

if [[ -n "${APP_ROOT:-}" ]]; then
  check_writable_dir "${APP_ROOT}" "APP_ROOT"
  check_writable_dir "${APP_ROOT}/bin" "backend bin directory"
  check_writable_dir "${APP_ROOT}/conf" "config directory"
  check_writable_dir "${APP_ROOT}/infra" "infra directory"
  check_writable_dir "${DB_BACKUP_DIR:-${APP_ROOT}/backups/database}" "database backup directory"
fi

if [[ -n "${FRONTEND_DIR:-}" ]]; then
  if [[ -d "${FRONTEND_DIR}" ]]; then
    ok "FRONTEND_DIR exists: ${FRONTEND_DIR}"
  else
    fail "FRONTEND_DIR does not exist: ${FRONTEND_DIR}"
  fi
fi

for item in \
  "user-http:${USER_HTTP_ADDR:-}" \
  "user-grpc:${USER_GRPC_ADDR:-}" \
  "core-http:${CORE_HTTP_ADDR:-}" \
  "core-grpc:${CORE_GRPC_ADDR:-}" \
  "agent-http:${AGENT_HTTP_ADDR:-}" \
  "agent-grpc:${AGENT_GRPC_ADDR:-}" \
  "gateway:${GATEWAY_ADDR:-}"; do
  check_port_hint "${item%%:*}" "${item#*:}"
done

echo "Preflight finished with ${errors} error(s), ${warnings} warning(s)."
if ((errors > 0)); then
  exit 1
fi
