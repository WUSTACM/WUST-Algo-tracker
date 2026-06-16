[Unit]
Description=WUST Algo PostgreSQL Backup
After=docker.service

[Service]
Type=oneshot
User=${APP_USER}
WorkingDirectory=${APP_ROOT}/tracker
EnvironmentFile=${APP_ROOT}/tracker/deploy/.env
ExecStart=${APP_ROOT}/tracker/deploy/scripts/backup-db.sh
PrivateTmp=true
NoNewPrivileges=true
