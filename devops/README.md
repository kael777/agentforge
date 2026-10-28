# DevOps

本目录集中管理 AgentForge 的构建、容器、Linux 服务和反向代理配置。以下命令默认从项目仓库根目录执行。

## Docker Compose

```bash
docker compose -p agentforge -f devops/docker/docker-compose.yml up --build
docker compose -p agentforge -f devops/docker/docker-compose.yml logs -f api
docker compose -p agentforge -f devops/docker/docker-compose.yml down
```

Compose 会依次启动 MySQL 8.4、执行 migration，再启动 `55555` 端口的 API。MySQL 数据保存在命名 volume 中；只有明确需要清空本地数据时才执行：

```bash
docker compose -p agentforge -f devops/docker/docker-compose.yml down -v
```

示例账号密码只用于本地开发，生产环境不得直接使用，也不要把 MySQL `3306` 暴露到公网。

## 跨平台构建

Windows 构建全部目标：

```powershell
.\devops\scripts\build.bat
```

只构建 Linux amd64：

```powershell
.\devops\scripts\build_linux.bat
```

Linux/macOS：

```bash
bash devops/scripts/build.sh
bash devops/scripts/build.sh linux arm64
bash devops/scripts/build_mac.sh
```

脚本使用 `CGO_ENABLED=0`，生成 Windows amd64、Linux amd64/arm64、macOS amd64/arm64 的 `api` 和 `migrate`，产物统一写入仓库根目录的 `dist`。

## systemd 部署

建议采用不可变版本目录和 `current` 软链接：

```text
/srv/agentforge/
├── releases/20261012-001/
│   ├── api
│   ├── migrate
│   └── web/dist/
└── current -> releases/20261012-001
```

首次部署时创建低权限用户，复制生产环境配置和 unit：

```bash
sudo useradd --system --home /srv/agentforge --shell /usr/sbin/nologin agentforge
sudo mkdir -p /etc/agentforge
sudo cp devops/env/api.env.example /etc/agentforge/api.env
sudo chmod 0600 /etc/agentforge/api.env
sudoedit /etc/agentforge/api.env

sudo cp devops/systemd/agentforge-api.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now agentforge-api
sudo systemctl status agentforge-api
sudo journalctl -u agentforge-api -f
```

unit 在启动 API 前运行幂等 migration。生产环境配置使用 `HTTP_ADDR=127.0.0.1:55555`，避免绕过 Nginx 直接访问后端。

服务同时监听一个管理端口 `MANAGEMENT_ADDR=127.0.0.1:55556`，只提供 `/readyz`、`/version` 和 `/metrics`。该端口只绑定回环地址，部署时不要把 `55556` 写进安全组、Docker `ports` 或 Nginx `proxy_pass`。

实时服务使用独立的 unit 和配置文件：

```bash
sudo cp devops/env/ws.env.example /etc/agentforge/ws.env
sudo chmod 0600 /etc/agentforge/ws.env
sudo cp devops/systemd/agentforge-ws.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now agentforge-ws
```

它监听 `127.0.0.1:8090`（由 Nginx 暴露）和 `127.0.0.1:8091`（管理端口）。
`TimeoutStopSec=25s` 是有意留的余量：WebSocket 关闭握手最长约 5 秒，预算不足会在
退出时切断在线连接而不给关闭码。

发布时上传一个完整 release，切换软链接后重启：

```bash
sudo ln -sfn /srv/agentforge/releases/20261012-002 /srv/agentforge/current
sudo systemctl restart agentforge-api
```

健康检查失败时把 `current` 指回上一版本再重启，即可同时回滚二进制和网站。数据库 migration 必须向后兼容，因为代码回滚不会自动回滚数据库结构。

## Nginx

`nginx/agentforge.conf` 默认用于 React 与 API 同域：

- `/api/*`、`/healthz` 代理到 `127.0.0.1:55555`。
- `/ws` 代理到实时服务 `127.0.0.1:8090`，单独使用不带 query 的访问日志格式，
  因为 ticket 在 query 里。
- `/readyz`、`/version`、`/metrics` 不经过 Nginx，只在管理端口 `127.0.0.1:55556`
  （API）和 `127.0.0.1:8091`（实时服务）上可达。
- 其他路径读取 React `web/dist`。
- 未匹配到静态文件时回退到 `index.html`，支持前端路由刷新。

```bash
sudo cp devops/nginx/agentforge.conf /etc/nginx/sites-available/agentforge
sudo ln -s /etc/nginx/sites-available/agentforge /etc/nginx/sites-enabled/agentforge
sudo nginx -t
sudo systemctl reload nginx
```

生产环境需要修改 `server_name`、静态文件路径并配置 HTTPS。

其他场景：

1. API 独立域名：删除静态文件配置，把 `location /` 也代理到后端。
2. 前后端不同域：将前端域名加入后端 `HTTP_ALLOWED_ORIGINS`，多个域名用逗号分隔。

## CI/CD

GitHub Actions 受平台约束，workflow 必须位于仓库根目录 `.github/workflows`：

- `ci.yml`：后端 test/vet/build、前端 check 和 Compose 配置校验。
- `release.yml`：将跨平台 Go 二进制、React `dist` 和部署配置打成同一个不可变制品，生成 SHA256 并创建 GitHub Release。

推送以下格式的 tag 会创建正式 release：

```bash
make publish VERSION=v0.1.0
```

Windows 不安装 Make 时使用：

```powershell
.\devops\scripts\publish.ps1 -Version v0.1.0
```
