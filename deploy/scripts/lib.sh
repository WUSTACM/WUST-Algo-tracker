#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
deploy_dir="$(cd "${script_dir}/.." && pwd)"
# shellcheck disable=SC2034
repo_dir="$(cd "${deploy_dir}/.." && pwd)"

export PATH="/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin:${PATH}"

load_env() {
  if [[ ! -f "${deploy_dir}/.env" ]]; then
    cp "${deploy_dir}/.env.example" "${deploy_dir}/.env"
    echo "Created ${deploy_dir}/.env from .env.example. Edit it, then rerun this command."
    exit 1
  fi

  set -a
  # shellcheck disable=SC1090,SC1091
  source "${deploy_dir}/.env"
  set +a
}

require_command() {
  local name="$1"
  if ! command -v "$name" >/dev/null 2>&1; then
    echo "Missing required command: ${name}"
    exit 1
  fi
}

run_sudo() {
  if [[ -n "${SUDO_PASSWORD:-}" ]]; then
    printf "%s\n" "${SUDO_PASSWORD}" | sudo -S "$@"
  else
    sudo "$@"
  fi
}

sudo_write_template() {
  local src="$1"
  local dst="$2"
  local tmp
  tmp="$(mktemp)"
  envsubst < "$src" > "$tmp"
  run_sudo install -m 0644 "$tmp" "$dst"
  rm -f "$tmp"
}
