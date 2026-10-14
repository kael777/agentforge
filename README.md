# AgentForge

AgentForge 是用于学习和展示 Agent 全栈工程能力的独立项目。后端、前端、DevOps 和文档按职责分离，正式发布由 GitHub Actions 基于版本 Tag 构建。

## 目录

```text
agentforge/
├── .github/workflows/       CI 与完整 Release
├── backend/
│   └── gofiber-gorm-next/   Go + Fiber + GORM + MySQL 后端
├── frontend/
│   └── web/                 通用 React + Vite 前端
├── devops/
│   ├── docker/              Dockerfile 与 Docker Compose
│   ├── env/                 生产环境变量示例
│   ├── nginx/               Nginx 配置
│   ├── scripts/             Windows、Linux 和 macOS 脚本
│   └── systemd/             systemd unit
├── docs/adr/                架构决策记录
├── dist/                    本地构建产物，不提交 Git
└── Makefile                 Linux、macOS 和 CI 的统一入口
```

## 环境要求

- Go 1.27+
- Node.js 24+
- Docker 与 Docker Compose
- Windows 使用 PowerShell；Linux/macOS/CI 可使用 Make 和 Bash

## 第一次准备

Windows：

```powershell
cd path\to\agentforge
Copy-Item backend\gofiber-gorm-next\.env.example backend\gofiber-gorm-next\.env
Copy-Item frontend\web\.env.example frontend\web\.env
Push-Location frontend\web
npm ci
Pop-Location
```

Linux/macOS：

```bash
make bootstrap
```

## 开发

Windows：

```powershell
.\devops\scripts\dev.ps1
```

Linux/macOS：

```bash
make dev
```

开发环境会启动 MySQL、执行 migration，然后启动：

- Go API：`http://localhost:55555`
- React Web：`http://localhost:5173`

停止应用进程后，使用以下命令停止 MySQL：

```powershell
docker compose -p agentforge -f devops\docker\docker-compose.yml down
```

## 测试与构建

Windows：

```powershell
.\devops\scripts\test.ps1
.\devops\scripts\build.ps1
.\devops\scripts\check.ps1
```

Linux/macOS/CI：

```bash
make test
make build
make check
```

`build` 会生成多个平台的 Go 二进制，并构建 React `dist`。

## 发布

正式 Release 由 GitHub Actions 构建，本地命令只负责校验、创建并推送版本 Tag：

```bash
make publish VERSION=v0.1.0
```

Windows 不安装 Make 时：

```powershell
.\devops\scripts\publish.ps1 -Version v0.1.0
```

CI 会将 Go 二进制、React `dist` 和部署配置组成同一个不可变制品，生成 SHA256 后创建 GitHub Release。发布不等于生产部署，生产部署仍需单独审批。

## 当前进度

- [x] Go/Fiber/GORM/MySQL 后端基线
- [x] React/Vite 通用前端脚手架
- [x] Docker、systemd、Nginx 和跨平台构建
- [x] 全栈 CI、Tag Release 和统一命令入口
- [x] Phase 0 架构决策记录
- [ ] Phase 1 最小命令行 Agent Demo

后端接口说明见 `backend/gofiber-gorm-next/README.md`，前端说明见 `frontend/web/README.md`，部署说明见 `devops/README.md`。
