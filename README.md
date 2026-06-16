# WUST Algo Tracker

WUST Algo Tracker 是 WUST ACM 算法训练数据平台的后端仓库，包含用户服务、核心数据服务、网关、AI 总结服务以及一套适用于 Ubuntu 服务器的部署脚本。

前端仓库：`https://github.com/WUSTACM/WUST-Algo-Frontend`

当前稳定版本：`v1.1.5`。版本变更记录见 [CHANGELOG.md](./CHANGELOG.md)。

版本规则：

- 小修小补：`v1.1.2 -> v1.1.3`
- 一组新功能：`v1.1.2 -> v1.2.0`
- 大改架构：`v1.x.x -> v2.0.0`

## 功能概览

- 用户系统：注册邀请码、登录、JWT 鉴权、角色管理、个人资料、头像链接、邮件通知。
- 后台管理：用户列表、角色调整、分组调整、密码重置、用户软删除；管理员账号受保护，后台不能直接授予管理员或修改管理员角色。
- 站内消息：两人私信、会话未读数、标记已读、教练/管理员群发消息。
- 团队系统：创建团队、队长管理团队、邀请成员、处理邀请、转移队长、队员退出团队、队长解散团队、团队成员刷题统计。
- OJ 数据抓取：支持 AtCoder、NowCoder、LuoGu、CodeForces、QOJ。
- 抓取可观测：记录手动/定时/绑定触发的抓取任务，展示排队、抓取中、完成、失败状态，并记录各 OJ 最近抓取时间和失败原因。
- 抓取审计：按用户和平台展示本次抓取条数、AC 去重数、提交去重数、异常/跳过记录数和最近错误。
- 数据统计：总提交、总 AC、时间段统计、平台拆分统计、热力图、用户排名、团队排名、统计口径说明和平台明细追溯。
- 统计缓存管理：管理员/教练可查看统计缓存 key、TTL，并清理全站或指定用户缓存。
- 运维审计：记录并查询用户服务与核心数据服务操作日志，覆盖改角色、删用户、改团队、清缓存、重爬数据和快照写入等操作。
- 功能快照：支持成就、成就全站比例和训练周报快照读写，便于前端减少重复计算并保持跨设备展示一致。
- 比赛数据：比赛列表、比赛详情、比赛排行榜。
- AI 总结：支持 OpenAI-compatible API，可配置 DeepSeek 等模型服务。
- 部署资产：Docker Compose 中间件、systemd 服务、配置渲染、状态检查脚本。

## 当前功能边界

当前版本不包含“两人数据对比”功能，也不暴露 `/v1/core/statistic/compare` 接口。数据统计能力保留在公开统计页、个人资料统计、用户排名和团队排名中。

## v1.1.5 更新

v1.1.5 聚焦运维可观测、统计解释和性能配合：

- 操作日志：新增用户服务操作日志查询接口 `/v1/user/system/operation-logs`。
- 操作日志：新增核心数据服务操作日志查询接口 `/v1/core/operation-logs`。
- 功能快照：新增 `feature_snapshots` 表和 `/v1/core/snapshot` 读写接口，支持 `weekly_report`、`achievement`。
- 全站成就快照：新增 `/v1/core/achievement/global-snapshot`，后端聚合成就解锁比例和站内百分位上下文，前端不再遍历全站用户计算。
- 快照清理：功能快照作为可再生成缓存保留 30 天，超过 5000 行时自动保留最近 3000 行并写入运维日志。
- 统计解释：统计口径说明补充平台明细、重爬队列和大型账号分批写入说明。
- 抓取审计：Codeforces、洛谷、牛客、QOJ、AtCoder 审计说明进一步解释和 OJ 主页不一致的常见原因。
- 备份保护：发布脚本会自动清理过期备份；备份目录超过默认 2048MiB 时会提示管理员并继续删除旧备份。
- 后台配合：前端 v1.1.5 在系统设置页合并展示服务状态、抓取任务、缓存状态、抓取审计和操作日志。

## v1.1.3 更新

v1.1.3 聚焦工程质量和权限补洞：

- 角色权限：后台角色调整不再允许直接授予管理员角色。
- 管理员保护：不能通过后台修改管理员账号角色，避免误操作或越权升级。
- 自身保护：管理员不能修改自己的角色，避免把最后一个管理员降级。
- 密码权限：本人修改密码必须提供旧密码；管理员只能重置非管理员账号密码，不能重置其他管理员。
- 群发消息：教练/管理员群发站内消息时重新查询数据库角色，不再只信任 JWT 中的旧角色。
- 错误提示配合：后端权限错误继续返回明确原因，前端 v1.1.3 会统一展示这些错误。
- CI/CD：新增 GitHub Actions 后端 CI 和手动发布工作流，支持构建、测试、打包、上传、服务器备份、重启和健康检查。
- 抓取可靠性：单平台刷新增加同平台任务去重、按平台限流和失败任务重试入口，避免误触导致重复抓取。
- 抓取一致性：全量刷新成功后按“用户 + 平台”替换旧提交，清理换绑或历史抓取 bug 留下的残留数据。
- CodeForces：提交抓取改为分页拉取，避免 jiangly 等大号历史提交抓不全。
- 大数据量入库：提交日志分批写入，避免 Postgres extended protocol `65535` 参数上限导致万级提交用户写入失败。

## v1.1.2 更新

v1.1.2 聚焦团队权限闭环：

- 新增队长转移接口 `/v1/user/team/owner/transfer`，只允许当前队长转移给同团队成员。
- 保留队员退出团队和队长解散团队能力，形成创建、邀请、管理、退出、解散的完整团队生命周期。
- 团队权限继续遵循“队长管理、队员自助退出、非成员只读”的规则。

## 技术栈

- Go 1.25.11
- Kratos
- PostgreSQL
- Redis
- RabbitMQ
- Consul
- GORM
- systemd
- Docker Compose

## 服务结构

```text
app/
├── user/       # 用户、角色、团队、注册邀请码、站内私信
├── core_data/  # OJ 绑定、爬虫、提交记录、统计、比赛
├── gateway/    # API gateway
└── agent/      # AI 总结服务
```

默认部署目录：

```text
/opt/wust-algo/
├── tracker/
├── frontend/
├── bin/
├── conf/
└── infra/
```

## 快速部署

以下步骤适用于 Ubuntu 22.04。

### 1. 创建运行用户

```bash
sudo adduser acm_tracker
sudo usermod -aG sudo acm_tracker
getent group docker >/dev/null && sudo usermod -aG docker acm_tracker
```

### 2. 准备目录

```bash
sudo mkdir -p /opt/wust-algo
sudo chown -R acm_tracker:acm_tracker /opt/wust-algo
sudo -iu acm_tracker
cd /opt/wust-algo
```

### 3. 获取代码

```bash
git clone https://github.com/WUSTACM/WUST-Algo-tracker.git tracker
git clone https://github.com/WUSTACM/WUST-Algo-Frontend.git frontend
```

也可以将打包好的源码传到服务器后解压到 `tracker` 和 `frontend`。

### 4. 安装 Go

```bash
cd /tmp
wget https://go.dev/dl/go1.25.11.linux-amd64.tar.gz
sudo rm -rf /usr/local/go
sudo tar -C /usr/local -xzf go1.25.11.linux-amd64.tar.gz
echo 'export PATH=/usr/local/go/bin:$PATH' >> ~/.bashrc
source ~/.bashrc
go version
```

如果服务器访问 `go.dev` 较慢，可以使用可信镜像源下载同版本 Go。

### 5. 部署后端

```bash
cd /opt/wust-algo/tracker
cp deploy/.env.example deploy/.env
nano deploy/.env
bash deploy/scripts/deploy-backend.sh
```

脚本会启动 PostgreSQL、Redis、RabbitMQ、Consul，并安装以下 systemd 服务：

- `wust-user`
- `wust-core-data`
- `wust-gateway`
- `wust-agent`，仅当 `ENABLE_AGENT=1` 时启用

### 6. 部署前端

```bash
cd /opt/wust-algo/frontend
cp deploy/.env.example deploy/.env
nano deploy/.env
bash deploy/scripts/deploy-frontend.sh
```

### 7. 标准发布流程

日常迭代建议使用一键发布脚本，它会先运行 preflight 检查部署命令、关键环境变量、目录权限和前端仓库路径，再备份当前 `bin/conf/dist/systemd/nginx`，依次部署后端、部署前端、检查 systemd、检查 Nginx 和首页 HTTP 状态：

```bash
cd /opt/wust-algo/tracker
git pull
cd /opt/wust-algo/frontend
git pull
cd /opt/wust-algo/tracker
bash deploy/scripts/preflight.sh
bash deploy/scripts/deploy-release.sh
```

`deploy-release.sh` 会自动执行同一组 preflight 检查；单独运行 `preflight.sh` 适合在正式发布前提前定位环境变量、端口占用或目录权限问题。

可选环境变量：

- `FRONTEND_DIR=/opt/wust-algo/frontend`：前端仓库路径，默认使用 `${APP_ROOT}/frontend`。
- `BACKUP_ROOT=/opt/wust-algo/backups`：发布备份目录。
- `BACKUP_RETENTION_DAYS=14`：发布备份和手动前端备份的默认保留天数。
- `BACKUP_KEEP_RECENT=10`：无论是否超过保留天数，至少保留最近多少份备份。
- `BACKUP_MAX_SIZE_MB=2048`：备份目录超过该大小时，脚本会提示管理员并自动删除更旧备份。
- `BACKUP_EMERGENCY_KEEP_RECENT=5`：容量超限自动清理时，至少保留最近多少份发布备份和前端备份。
- `HEALTH_URL=http://127.0.0.1:8088/`：发布后的健康检查地址。
- `SUDO_PASSWORD=...`：非交互环境可用它提前完成 `sudo` 认证；交互终端不需要设置。

### 8. GitHub Actions 发布

仓库内置两个工作流：

- `Backend CI`：push、pull request 或手动触发时运行核心测试并编译 `user`、`core_data`、`gateway`，并上传 `wust-algo-backend-bin` artifact。
- `Manual Deploy`：手动触发，拉取前后端指定分支，打包上传到服务器，然后调用 `deploy/scripts/deploy-release.sh`。

使用 `Manual Deploy` 前需要在 GitHub 仓库 Secrets 中配置：

- `DEPLOY_HOST`：服务器 IP 或域名，例如 `10.99.16.19`。
- `DEPLOY_USER`：部署用户，例如 `acm_tracker`。
- `DEPLOY_SSH_PASSWORD`：部署用户 SSH 密码。
- `DEPLOY_SUDO_PASSWORD`：部署用户 sudo 密码。
- `DEPLOY_APP_ROOT`：可选，默认 `/opt/wust-algo`。

触发路径：GitHub 仓库页面 -> `Actions` -> `Manual Deploy` -> `Run workflow`。默认部署 `main`，也可以指定前端和后端分支或 tag。

## 配置说明

后端配置位于 `deploy/.env`。常用字段：

- `POSTGRES_*`：PostgreSQL 连接信息。
- `REDIS_*`：Redis 连接信息。
- `RABBITMQ_*`：RabbitMQ 连接信息。
- `CONSUL_*`：Consul 服务发现地址。
- `USER_HTTP_ADDR` / `CORE_HTTP_ADDR`：用户服务和核心数据服务 HTTP 地址。
- `GATEWAY_ADDR`：网关监听地址。
- `ENABLE_AGENT`：是否启用 AI 总结服务。
- `AI_BASE_URL` / `AI_MODEL` / `AI_API_KEY`：OpenAI-compatible AI 配置。
- `SMTP_*`：邮件发送配置。

DeepSeek 示例：

```env
ENABLE_AGENT=1
AI_BASE_URL=https://api.deepseek.com
AI_MODEL=deepseek-chat
AI_API_KEY=replace-with-your-api-key
```

不要提交真实 `.env`、API Key、SMTP 授权码或数据库密码。

## 注册和管理员

注册默认需要邀请码，默认值在后端系统配置中初始化为：

```text
wustacm666
```

管理员可在后台系统设置中修改邀请码。

首次创建管理员：

```bash
cd /opt/wust-algo/tracker
bash deploy/scripts/init-admin.sh your_username
```

## 站内私信接口

私信由 `user` 服务提供，所有接口都需要登录态 JWT。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/v1/user/message/conversations?page=1&pageSize=20` | 获取当前用户的会话列表 |
| `GET` | `/v1/user/message/thread?userId=2&page=1&pageSize=30` | 获取我和指定用户的聊天记录 |
| `POST` | `/v1/user/message/send` | 给指定用户发送私信 |
| `POST` | `/v1/user/message/read` | 标记与指定用户的会话已读 |
| `GET` | `/v1/user/message/unread-count` | 获取当前用户未读私信总数 |
| `POST` | `/v1/user/message/broadcast` | 教练和管理员群发站内消息 |

发送私信请求体示例：

```json
{
  "receiverId": 2,
  "content": "你好，方便交流一下训练计划吗？"
}
```

群发消息请求体示例：

```json
{
  "content": "今晚 20:00 训练赛，请大家准时参加。"
}
```

私信规则：

- 不能给自己发消息。
- 接收者必须存在。
- 内容会去除首尾空白，长度限制为 `1-1000` 字符。
- 两人会话使用稳定的 `pair_key`，A-B 与 B-A 会落到同一个会话。
- 普通用户不能群发；群发仅允许管理员和教练。
- 删除用户不会级联删除历史消息，前端可降级显示已注销用户。

相关数据库表由 GORM `AutoMigrate` 自动创建：

- `direct_message_threads`
- `direct_messages`

## 团队接口

团队由 `user` 服务提供，除团队详情外均需要登录态 JWT。队长负责团队管理，普通队员只能退出自己的团队。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/v1/user/team/detail?groupId=1` | 获取团队详情和成员列表 |
| `POST` | `/v1/user/team/create` | 创建团队，创建者自动成为队长 |
| `POST` | `/v1/user/team/update` | 队长编辑团队名称、头像和描述 |
| `POST` | `/v1/user/team/invite` | 队长邀请无团队用户 |
| `POST` | `/v1/user/team/member/remove` | 队长移除非队长成员 |
| `POST` | `/v1/user/team/owner/transfer` | 队长将队长身份转移给其他团队成员 |
| `POST` | `/v1/user/team/leave` | 普通队员退出当前团队 |
| `POST` | `/v1/user/team/disband` | 队长解散团队 |
| `GET` | `/v1/user/team/invites` | 获取当前用户待处理团队邀请 |
| `POST` | `/v1/user/team/invite/respond` | 同意或拒绝团队邀请 |

团队规则：

- 用户同一时间只能加入一个团队。
- 队长可将队长身份转移给当前团队内的其他成员；转移后原队长立即失去团队管理权限。
- 队长不能通过退出接口离开团队；如果需要删除团队，应调用解散接口。
- 解散团队会将所有成员 `group_id` 重置为 `0`，并关闭该团队待处理邀请。
- 队员退出团队后自身 `group_id` 重置为 `0`，需要重新接受邀请才能加入团队。
- 旧团队若缺少 `owner_id`，后端会按团队内最小用户 ID 自动补齐队长。

## OJ 抓取状态接口

`core_data` 会通过 RabbitMQ 执行 OJ 抓取，并将任务状态和平台可信度写入数据库。

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `POST` | `/v1/core/spider/update` | 手动刷新指定用户 OJ 数据，返回 `jobId`；传 `platform` 时只刷新单个平台 |
| `GET` | `/v1/core/spider/job?jobId=1` | 查询单个抓取任务 |
| `GET` | `/v1/core/spider/jobs?scope=mine&status=running` | 查询抓取任务列表 |
| `GET` | `/v1/core/spider/status?userId=1` | 查询用户各 OJ 最近抓取状态 |
| `GET` | `/v1/core/spider/audit?userId=1` | 查询用户各 OJ 抓取审计数据：原始抓取、有效写入、去重、异常和最近错误 |
| `POST` | `/v1/core/spider/retry` | 重试失败的抓取任务，本人、教练和管理员可用 |
| `POST` | `/v1/core/spider/rebuild-all` | 管理员/教练触发全站全量重爬 |

任务状态：

- `queued`：已入队，等待消费。
- `running`：正在抓取。
- `success`：本次任务完成。
- `failed`：本次任务存在失败平台。

数据同步状态规则：

- 绑定 OJ 后从未成功抓取，后端状态为 `never`，前端展示为“未同步”。
- 最近成功抓取超过 24 小时，后端会标记 `isStale=true`，前端展示为“未同步”。
- 当前正在抓取时，后端状态为 `running`，前端展示为“抓取中”。
- 最近一次成功抓取且未过期时，后端状态为 `success`，前端展示为“已同步”。
- 抓取失败会保留上次成功时间，并记录最近失败原因，前端展示为“未同步”。
- 失败原因只建议给本人、管理员和教练展示。

刷新请求示例：

```json
{
  "userId": 4,
  "platform": "NowCoder"
}
```

`platform` 为空或不传时执行全量刷新。

抓取防误触规则：

- 同一用户同一平台已有 `queued/running` 任务时，新的同平台请求不会重复入队，会返回现有 `jobId`。
- 全量刷新会与所有单平台刷新互斥；单平台刷新只与自身平台和全量刷新互斥。
- 手动刷新按 `userId + platform` 做 60 秒限流，不同平台互不影响。
- 失败任务可通过后台或 `/v1/core/spider/retry` 重试；重试仍遵循重复任务保护。
- CodeForces 单平台刷新使用 30 分钟冷却窗口，手动刷新、失败重试、同账号重复绑定和队列最终执行层都会检查该窗口。
- 全量刷新、定时刷新和全站重爬会自动跳过 30 分钟内已开始抓取过的 CodeForces 数据，不影响其他 OJ 平台刷新。
- CodeForces 确实换绑为另一个用户名时会清理旧同步状态并允许立即抓取，避免旧账号冷却误伤新账号数据初始化。
- 全量刷新采用平台级替换同步：平台抓取成功后删除该用户该平台旧提交，再写入本次完整提交，避免账号换绑或历史分页 bug 产生残留。
- 抓取结果会校验 `submit_id` 和提交时间，并对同一批次重复 `submit_id` 去重；全部无效时拒绝写入，保留旧数据并记录失败原因。
- CodeForces 使用分页抓取全量提交，不依赖一次性超大 `count`。
- AtCoder 翻页会按提交 ID 去重并检测游标前进；NowCoder API/HTML 分页失败会显式报错；LuoGu/QOJ 会校验 HTTP 状态和提交时间，避免静默写入半截数据。
- NowCoder 训练 API 只支持数字用户 ID；历史非数字绑定会跳过该补充 API，仅使用公开 HTML 提交页，并建议用户在绑定页改为牛客用户编号以获得更完整数据。

相关数据库表由 GORM `AutoMigrate` 自动创建：

- `spider_refresh_jobs`
- `spider_sync_statuses`

## 统计口径、明细与缓存

本站统计以 `submit_logs` 为事实来源，AC 数按 `platform + problem key` 去重；当题目标识为空时回退到提交 ID。各 OJ 主页的数字可能使用自己的展示口径，因此不保证完全一致：

- 洛谷：本站保留抓取接口返回的全部记录，包括 U/T/SP 等题目。
- Codeforces：本站基于 `user.status` API，题目标识包含 contestId、index、name。
- 全站重爬、单平台刷新和缓存刷新过程中，短时间内可能看到统计数字变化。

统计说明和明细追溯接口：

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/v1/core/statistic/explanation` | 获取公开统计口径说明 |
| `GET` | `/v1/core/statistic/platform-detail?userId=4&platform=CodeForces&mode=ac&page=1&pageSize=30` | 查看平台 AC 题明细 |
| `GET` | `/v1/core/statistic/platform-detail?userId=4&platform=CodeForces&mode=submit&page=1&pageSize=30` | 查看平台提交记录明细 |
| `GET` | `/v1/core/statistic/cache-status?userId=-1` | 管理员/教练查看统计缓存状态 |
| `POST` | `/v1/core/statistic/cache-clear` | 管理员/教练清理全站或指定用户统计缓存 |

`platform-detail` 会返回平台汇总、计入口径、AC 题列表或提交记录，包含 problem key、是否计入 AC、审计说明、首次 AC 时间、提交次数等字段，用于排查 OJ 主页与本站统计差异。

`spider/audit` 会按平台返回：

- `lastRawFetchedCount`：最近一次抓取的原始返回量，约等于有效写入量加跳过量。
- `lastFetchedCount`：最近一次清洗后有效写入的提交数。
- `rawSubmitCount`：当前库内该平台原始提交记录数。
- `distinctSubmitCount`：按 problem key 去重后的提交题数。
- `acceptedSubmitCount`：状态为 AC/Accepted/正确/OK 的提交次数。
- `distinctAcCount`：最终统计使用的去重 AC 题数。
- `filteredDuplicateCount`：在题目维度统计时被去重掉的重复提交数。
- `filteredAbnormalCount`：抓取清洗阶段跳过或库内检测到的异常记录数。
- `countPolicy` / `auditNotes`：面向排查的口径说明和当前平台审计结论。

缓存接口中 `userId=-1` 表示全站统计缓存；传具体用户 ID 时清理该用户相关统计和提交缓存。

## 常用维护命令

查看状态：

```bash
cd /opt/wust-algo/tracker
bash deploy/scripts/status.sh
```

重启服务：

```bash
sudo systemctl restart wust-user
sudo systemctl restart wust-core-data
sudo systemctl restart wust-gateway
```

查看日志：

```bash
sudo journalctl -u wust-user -n 100 --no-pager
sudo journalctl -u wust-core-data -n 100 --no-pager
```

验证核心接口：

```bash
curl -i http://127.0.0.1:8088/
curl -i 'http://127.0.0.1:8088/api/core/statistic/period?userId=-1'
curl -i 'http://127.0.0.1:8088/api/user/group/list'
```

私信、抓取任务和后台接口未登录时应返回未授权。

重新部署后端：

```bash
cd /opt/wust-algo/tracker
git pull
bash deploy/scripts/deploy-backend.sh
```

完整发布推荐使用：

```bash
bash deploy/scripts/deploy-release.sh
```

## 本地构建

```bash
go build -o /tmp/wust-user ./app/user/cmd/user
go build -o /tmp/wust-core-data ./app/core_data/cmd/core_data
go build -o /tmp/wust-agent ./app/agent/cmd/agent
```

常规测试：

```bash
go test ./app/core_data/internal/data/dal ./app/core_data/internal/biz/service ./app/core_data/internal/spider/platform ./app/core_data/task ./app/user/internal/service ./app/user/internal/data/dal
```

当前重点覆盖：

- AC 状态识别与 `platform + problem` 去重。
- 排名和统计使用的基础去重规则。
- CodeForces 分页抓取。
- 全量抓取结果清洗、去重和无效数据保护。
- 团队队长权限判断。
- 注册邀请码校验。
- 私信内容长度和空内容校验。
- 单平台抓取任务互斥规则。

外部数据库、OJ 和第三方 AI 的集成测试默认跳过。如需运行：

```bash
RUN_INTEGRATION_TESTS=1 go test ./app/core_data/internal/data/dal ./app/core_data/test
```

建议提交前执行：

```bash
go test ./app/user/...
go build -o /tmp/wust-user ./app/user/cmd/user
```

## 统计口径

AC 统计按 `user_id + platform + problem` 去重，避免同一用户在同一平台同一题多次 AC 被重复计入。系统账号 `admin` 不参与公开排名，其他管理员账号正常计入排名。

团队排名中，成员数显示团队真实人数；团队刷题数按团队成员贡献汇总。

## 安全注意事项

- 修改生产环境 JWT secret 后再公开暴露服务。
- 不要将 `deploy/.env` 提交到 Git。
- 管理员账号不应使用弱密码。
- 生产环境建议只开放 Nginx 入口，数据库、Redis、RabbitMQ、Consul 不要直接暴露到公网。

## 致谢

本项目基于无锡学院相关开源项目继续开发，感谢原项目在 GitHub 上贡献的源码。
