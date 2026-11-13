# DevOps

本目录集中管理 AgentForge 的构建、容器、Linux 服务和反向代理配置。以下命令默认从项目仓库根目录执行。

## Docker Compose

```bash
docker compose -p agentforge -f devops/docker/docker-compose.yml up --build
docker compose -p agentforge -f devops/docker/docker-compose.yml logs -f api
docker compose -p agentforge -f devops/docker/docker-compose.yml down
```

Compose 会依次启动 MySQL 8.4、执行 migration，再启动 `55555` 端口的 API 和 `8090` 端口的实时服务。MySQL 数据保存在命名 volume 中；只有明确需要清空本地数据时才执行：

```bash
docker compose -p agentforge -f devops/docker/docker-compose.yml down -v
```

MySQL 只绑定回环地址，并且映射到宿主机的 `13306`（不是 3306），避免和你本机已装的 MySQL 抢端口：

```bash
mysql -h 127.0.0.1 -P 13306 -uagentforge -p agentforge
```

示例账号密码只用于本地开发，生产环境不得直接使用，也不要把 MySQL 端口暴露到公网。

## 服务器基础设施（Docker）

`devops/docker/server/` 是"一台服务器上的数据组件"：MySQL 必选、Redis 可选（profile）。
和本地开发用的 `devops/docker/docker-compose.yml` 分开，那个会一次起 mysql + api + ws。

设计要点：

- **只发布到 127.0.0.1**。对外入口只有一个——宿主机 Nginx 的 443。
- **宿主端口可配**，默认 `13306`。服务器交付时常常已经带了 3306 的 MySQL（宝塔/apt/云镜像），
  换端口是为了不打架，也为了让人一眼看出这不是宿主机那个库。
- **密码走 secret 文件**，不进环境变量，`docker inspect` 里看不到。
- **Redis 默认不启动**，且不发布端口——应用当前不依赖它，多一个有状态组件就多一份备份责任。

```bash
cd devops/docker/server
cp .env.example .env && mkdir -p secrets
openssl rand -base64 32 > secrets/mysql_root.txt
openssl rand -base64 32 > secrets/mysql_app.txt

bash ../../scripts/port-check.sh        # 预检：端口占用、宿主已有 MySQL、0.0.0.0 发布
docker compose --env-file .env up -d --wait
```

`port-check.sh` 会直接指出三类问题：目标端口被占用、宿主已有同名服务在跑、
以及"某个容器把端口发布到了所有网卡"。

**宿主已有 MySQL 时二选一，不要都跑**：用它（改 DSN，不启容器）或停用它
（`systemctl disable --now mysql`）。两个都跑 = 内存双份 + 备份漏一个 + 排查时连错库。

细节见 [docker/server/README.md](./docker/server/README.md)。

## 构建与打包

### 常用命令

| 场景 | 命令 | 产物 |
|---|---|---|
| 只想要二进制（本地调试） | `build.bat linux amd64` | `dist/linux-amd64/{api,migrate,ws}` |
| 想要能直接部署的包 | `build.bat linux amd64 pack` | `dist/agentforge_<版本>_linux_amd64.tar.gz` |
| 全部平台 | `build.bat` | `dist/<os>-<arch>/` 五套 |
| Linux/macOS | `bash devops/scripts/build.sh linux amd64 pack` | 同上 |
| 只构建 Linux（Windows 快捷方式） | `build_linux.bat` | 等价于 `build.bat linux` |
| 构建当前 mac 架构 | `build_mac.sh` | 按 `uname -m` 选择 |

`pack` 打出的归档包含三个二进制加 `deploy/`：

```
api  migrate  ws
deploy/{env,nginx,systemd}
```

解开就能部署，不需要再单独传 systemd unit 和 Nginx 配置。

### 版本号从哪来

**默认不设，也不要设。** 脚本自己取 `git describe --tags --always --dirty`：

| git 状态 | 结果 |
|---|---|
| HEAD 正好在 tag 上 | `v0.2.0` |
| tag 之后有提交 | `v0.2.0-26-g36de941` |
| 工作区有未提交改动 | 末尾加 `-dirty` |
| 没有 tag / 不是 git 仓库 | 短 SHA 或 `dev` |

构建时会先打印解析结果，不用猜：

```
Version: v0.1.0-26-g36de941-dirty
Commit:  36de941
Time:    2026-11-12T10:01:26Z
```

这个版本号比手写的 `v0.1.0` 信息量大：能看出它是 tag 之后的第几次提交、对应哪个 commit、
以及是不是从**有未提交改动的工作区**构建出来的——后者在排查线上问题时很关键。

只有三种情况需要显式覆盖：

| 场景 | 做法 |
|---|---|
| 重建某个历史版本 | `VERSION=v0.1.0 bash devops/scripts/build.sh linux amd64 pack` |
| 没有 git 的环境（导出的源码包） | 自动退化为 `dev`，也可显式指定 |
| 正式发布 | 由 `release.yml` 用 tag 名，不经过本地脚本 |

### 要一个"正式版"的包

```powershell
git tag v0.2.0
.\devops\scripts\build.bat linux amd64 pack
# → dist\agentforge_v0.2.0_linux_amd64.tar.gz（没有 -dirty 后缀）
```

```bash
git tag v0.2.0
bash devops/scripts/build.sh linux amd64 pack
```

tag 必须打在**干净的**工作区上，否则文件名照样带 `-dirty`。

### 命名与正式发布一致

本地归档和 `release.yml` 产出的都是 `agentforge_<版本>_<os>_<arch>.<ext>`
（Windows 目标用 `.zip`，其余 `.tar.gz`）。

这样做的目的：本地验证用的包和 GitHub Release 下载的包不会是两种东西。

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

- `ci.yml`：后端与实时服务的 test/vet/build、前端 check、容器镜像构建。
- `ci.yml` 的 `security` job：`govulncheck` 扫描两个 Go module 中**可达**的依赖漏洞。
- `ci.yml` 的前端 job：`npm audit --omit=dev` 只对运行时依赖设门禁。
- `release.yml`：将跨平台 Go 二进制、React `dist` 和部署配置打成同一个不可变制品，生成 SHA256 并创建 GitHub Release。

镜像构建使用固定 digest 的基础镜像（`devops/docker/*.Dockerfile` 的 `FROM ...@sha256:`）。
升级基础镜像应该是显式动作，不应因为 tag 被重新指向而意外发生。

推送以下格式的 tag 会创建正式 release：

```bash
make publish VERSION=v0.1.0
```

Windows 不安装 Make 时使用：

```powershell
.\devops\scripts\publish.ps1 -Version v0.1.0
```
