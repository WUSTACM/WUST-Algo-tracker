[Unit]
Description=Run WUST Algo PostgreSQL Backup

[Timer]
OnCalendar=${DB_BACKUP_TIMER_ON_CALENDAR}
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
