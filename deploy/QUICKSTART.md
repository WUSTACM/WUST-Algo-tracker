# WUST Algo Server Quickstart

This quickstart assumes Ubuntu 22.04 and the default deployment root `/opt/wust-algo`.

## 1. Prepare System Packages

```bash
sudo apt update
sudo apt install -y git curl nginx gettext-base postgresql-client build-essential
```

Create the deployment user:

```bash
sudo adduser acm_tracker
sudo usermod -aG sudo acm_tracker
getent group docker >/dev/null && sudo usermod -aG docker acm_tracker
```

Install Docker with the Compose plugin if it is not already available:

```bash
docker compose version
```

Install Node.js 22 for frontend builds:

```bash
curl -fsSL https://deb.nodesource.com/setup_22.x | sudo -E bash -
sudo apt install -y nodejs
```

Install Go matching `go.mod`:

```bash
cd /tmp
wget https://go.dev/dl/go1.25.11.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.11.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
```

## 2. Clone Repositories

```bash
sudo mkdir -p /opt/wust-algo
sudo chown -R acm_tracker:acm_tracker /opt/wust-algo

sudo -iu acm_tracker
cd /opt/wust-algo
git clone https://github.com/WUSTACM/WUST-Algo-tracker.git tracker
git clone https://github.com/WUSTACM/WUST-Algo-Frontend.git frontend
```

## 3. Deploy Backend

```bash
cd /opt/wust-algo/tracker
cp deploy/.env.example deploy/.env
nano deploy/.env
bash deploy/scripts/deploy-backend.sh
```

Keep `ENABLE_AGENT=0` until OpenAI-compatible AI settings and SMTP settings are real.
For DeepSeek, set `AI_BASE_URL=https://api.deepseek.com`, `AI_MODEL=deepseek-chat`, and `AI_API_KEY`.
Set `JWT_SECRET` to a random value of at least 32 characters before starting the services.

The backend deploy installs `wust-db-backup.timer`, which runs PostgreSQL dumps into `/opt/wust-algo/backups/database` by default. Run a backup manually with:

```bash
cd /opt/wust-algo/tracker
bash deploy/scripts/backup-db.sh
systemctl list-timers wust-db-backup.timer
```

To verify a dump before a restore window:

```bash
pg_restore --list /opt/wust-algo/backups/database/latest/algo_user.dump >/dev/null
pg_restore --list /opt/wust-algo/backups/database/latest/algo_core_data.dump >/dev/null
```

To restore, stop the app services, create fresh target databases, then run `pg_restore --clean --if-exists --no-owner --no-privileges -d <database> <dump-file>` for each dump.

## 4. Deploy Frontend

```bash
cd /opt/wust-algo/frontend
cp deploy/.env.example deploy/.env
nano deploy/.env
bash deploy/scripts/deploy-frontend.sh
```

Set `DOMAIN` in `deploy/.env` before running the frontend script.

For routine releases after both repositories already exist on the server, use the full release wrapper:

```bash
cd /opt/wust-algo/tracker
git pull
cd /opt/wust-algo/frontend
git pull
cd /opt/wust-algo/tracker
bash deploy/scripts/preflight.sh
bash deploy/scripts/deploy-release.sh
```

The wrapper runs the same preflight checks automatically, creates a backup under `/opt/wust-algo/backups`, deploys backend and frontend, reloads services, tests Nginx, and checks the configured health URL.
Run `deploy/scripts/preflight.sh` by itself when you want to verify commands, required environment variables, writable deployment directories, frontend path, and obvious port conflicts before a release window.

## 5. Create First Admin

Register a normal account from the website, then run:

```bash
cd /opt/wust-algo/tracker
bash deploy/scripts/init-admin.sh your_username
```

## 6. Check Status

```bash
cd /opt/wust-algo/tracker
bash deploy/scripts/status.sh
curl http://127.0.0.1:8080/v1/user/group/list
```

Check spider jobs when users report stale OJ data:

```bash
curl -H "Authorization: Bearer <jwt>" 'http://127.0.0.1:8088/api/core/spider/jobs?scope=all&page=1&pageSize=20'
curl -H "Authorization: Bearer <jwt>" 'http://127.0.0.1:8088/api/core/spider/status?userId=4'
```
