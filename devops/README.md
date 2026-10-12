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
/srv/gofiber-gorm-next/
├── releases/20261012-001/
│   ├── api
│   ├── migrate
│   └── web/dist/
└── current -> releases/20261012-001
```

首次部署时创建低权限用户，复制生产环境配置和 unit：

```bash
sudo useradd --system --home /srv/gofiber-gorm-next --shell /usr/sbin/nologin gofiber
sudo mkdir -p /etc/gofiber-gorm-next
sudo cp devops/env/gofiber-gorm-next.env.example /etc/gofiber-gorm-next/gofiber-gorm-next.env
sudo chmod 0600 /etc/gofiber-gorm-next/gofiber-gorm-next.env
sudoedit /etc/gofiber-gorm-next/gofiber-gorm-next.env

sudo cp devops/systemd/gofiber-gorm-next.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now gofiber-gorm-next
sudo systemctl status gofiber-gorm-next
sudo journalctl -u gofiber-gorm-next -f
```

unit 在启动 API 前运行幂等 migration。生产环境配置使用 `HTTP_ADDR=127.0.0.1:55555`，避免绕过 Nginx 直接访问后端。

发布时上传一个完整 release，切换软链接后重启：

```bash
sudo ln -sfn /srv/gofiber-gorm-next/releases/20261012-002 /srv/gofiber-gorm-next/current
sudo systemctl restart gofiber-gorm-next
```

健康检查失败时把 `current` 指回上一版本再重启，即可同时回滚二进制和网站。数据库 migration 必须向后兼容，因为代码回滚不会自动回滚数据库结构。

## Nginx

`nginx/gofiber-gorm-next.conf` 默认用于 React 与 API 同域：

- `/api/*`、`/healthz`、`/readyz` 代理到 `127.0.0.1:55555`。
- 其他路径读取 React `web/dist`。
- 未匹配到静态文件时回退到 `index.html`，支持前端路由刷新。

```bash
sudo cp devops/nginx/gofiber-gorm-next.conf /etc/nginx/sites-available/gofiber-gorm-next
sudo ln -s /etc/nginx/sites-available/gofiber-gorm-next /etc/nginx/sites-enabled/gofiber-gorm-next
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
