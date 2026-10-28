# AgentForge

[![AgentForge CI](https://github.com/kael777/agentforge/actions/workflows/ci.yml/badge.svg?branch=main)](https://github.com/kael777/agentforge/actions/workflows/ci.yml)
[![Secret Scan](https://github.com/kael777/agentforge/actions/workflows/secret-scan.yml/badge.svg?branch=main)](https://github.com/kael777/agentforge/actions/workflows/secret-scan.yml)

AgentForge 是用于学习和展示 Agent 全栈工程能力的独立项目。后端、前端、DevOps 和文档按职责分离，正式发布由 GitHub Actions 基于版本 Tag 构建。

## 目录

```text
agentforge/
├── .github/workflows/       CI 与完整 Release
├── backend/
│   ├── api/                 Go + Fiber + GORM + MySQL 后端
│   └── ws/                  Go + net/http WebSocket 实时服务
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

- Go 1.27.3+（`go.mod` 声明的最低工具链版本，低于它会自动下载对应工具链）
- Node.js 24+
- Docker 与 Docker Compose
- Windows 使用 PowerShell；Linux/macOS/CI 可使用 Make 和 Bash

## 第一次准备

Windows：

```powershell
cd path\to\agentforge
Copy-Item backend\api\.env.example backend\api\.env
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

实时服务需要单独启动（独立 module）：

```powershell
cd backend\ws
go run ./cmd/ws
```

端到端自检（API 与实时服务都在运行时）：

```powershell
.\devops\scripts\ws-smoke.ps1
```

图形界面调试：启动前端后打开 <http://localhost:5173/realtime-console.html>。
页面会自己完成登录取票、连接、订阅、发布和心跳。

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

| 模块 | 状态 | 说明 |
| --- | --- | --- |
| Go API 基线 | ✅ | Fiber + GORM + MySQL，分层、DI、优雅退出 |
| 安全基线 | ✅ | 公网/管理端口拆分、响应头与日志脱敏、限流、token 作用域校验、生产配置校验 |
| 可观测性（应用侧） | ✅ | `/metrics`：请求、延迟、并发、连接池、Go 运行时 |
| 可观测性（采集侧） | ⬜ | Alloy / Prometheus / Grafana / Trace 尚未搭建 |
| WebSocket 实时服务 | ✅ | 一次性票据、Origin 白名单、连接与速率限制、优雅关闭 |
| React/Vite 前端 | ✅ | 路由、请求、Zod 校验、测试与构建 |
| 部署配置 | ✅ | systemd、Nginx、Docker、跨平台构建（unit 未在 Linux 实机验证） |
| CI / Release | ✅ | 双 module 测试与构建、Tag 不可变制品 |
| 备份与恢复演练 | ⬜ | 未开始 |
| 只读 AI 排障 Agent | ⬜ | 未开始 |
| Agent Demo | ⬜ | 已归档到 `spike/agent-runtime` 分支，待基线稳定后重做 |

已知限制：

- WebSocket 票据的单次使用依赖进程内缓存，多实例部署需要改用共享存储。
- 关闭握手最长约 5 秒（`coder/websocket` 行为），因此排空预算不得小于该值。
- `.github/workflows/ci.yml` 使用 `go test -race`，但 Windows 开发机缺少 gcc，未能本地验证。

说明文档：后端接口见 `backend/api/README.md`，实时服务协议见 `backend/ws/README.md`，前端见 `frontend/web/README.md`，部署见 `devops/README.md`。
