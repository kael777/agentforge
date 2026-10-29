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
├── templates/               三个模板的说明与生成入口
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

## 技术选型

**应用层**

| 软件 | 用途 | 为什么用它 |
|---|---|---|
| Go 1.27 | 后端语言 | 单二进制部署、并发模型适合长连接、静态类型便于长期维护 |
| Fiber v3 | HTTP 框架 | 基于 fasthttp，中间件生态完整；相比 Gin 的超时与关停控制更明确 |
| GORM | ORM | 简单 CRUD 少写样板；复杂查询仍可退回原生 SQL |
| MySQL 8.4 | 数据库 | 通用、运维熟悉；本项目的隔离模型靠应用层保证，不需要 PostgreSQL 的高级特性 |
| golang-jwt + bcrypt | 认证 | 标准实现，不自己发明密码学 |
| coder/websocket | WebSocket | 基于 `net/http`，context 语义完整，关闭码清晰；比在 fasthttp 上 hijack 更可控 |
| client_golang | 指标 | 标准 exposition 格式，直方图与采集器不用自己维护 |
| log/slog | 日志 | 标准库，结构化输出，便于统一脱敏 |

**前端**

| 软件 | 用途 | 为什么用它 |
|---|---|---|
| React 19 + TypeScript | UI | 类型贯穿到接口边界，改接口时编译期就能发现 |
| Vite | 构建 | 开发热更新快，生产构建产物简单（纯静态） |
| TanStack Query | 服务端状态 | 缓存、重试、失效策略内置，不用自己写 loading/error 状态机 |
| Zod | 运行时校验 | 接口返回值与环境变量在运行时也校验，不只靠类型 |
| Vitest + oxlint | 测试与检查 | 与 Vite 同源配置，启动快 |

**工程与质量**

| 软件 | 用途 | 为什么用它 |
|---|---|---|
| GitHub Actions | CI | 与仓库同处一个平台，Tag 直接触发 Release |
| Gitleaks | 密钥扫描 | 扫历史而不只是工作区；用精确放行而非整类文件放行 |
| govulncheck | 依赖漏洞 | 只报**可达**调用路径，噪音远小于普通 SCA 扫描 |
| npm audit | 前端依赖 | 只对运行时依赖设门禁，避免构建工具告警阻塞无关 PR |

**运维基建**（Step 3～7 引入，见 `docs/plans/`）

| 软件 | 用途 | 为什么用它 |
|---|---|---|
| WireGuard | 跨机组网 | 完全开源、无外部依赖；三台机器两组公钥即可，出问题能逐层排查 |
| Nginx | 入口 | TLS 终止、静态资源、反代；比在应用里处理 TLS 更成熟 |
| systemd | 进程管理 | 崩溃重启、资源限制、`ProtectSystem` 等加固项开箱可用 |
| Pipewright | 发布 | 固定制品 + 审批 + 回滚；替代手工 SSH 部署 |
| Loki + Alloy | 日志 | 索引标签而非全文，存储成本低；Alloy 一个进程同时收日志和指标 |
| Prometheus | 指标 | 拉/推模型成熟，与应用的 `/metrics` 直接对接 |
| Grafana | 看板 | 数据源与 dashboard JSON 可版本化，不靠手点 |
| Uptime Kuma | 探活与告警 | 三层探针（存活/就绪/业务）+ 飞书通知 |
| Restic | 备份 | 去重、加密、可校验；`--stdin-from-command` 能发现上游失败 |
| Cloudflare | 入口防护 | 源站只接受 CF 网段，隐藏真实 IP |

### 几个关键取舍

- **WireGuard 而不是 Tailscale**：不依赖第三方控制面，三台机器的密钥与路由完全自己掌握；代价是加机器要手改配置。
- **HTTP 与 WebSocket 拆成两个进程**：超时与关停语义不同，混在一起会互相拖累。理由与备选方案见 [ADR-0002](docs/adr/0002-http-ws-split-and-ticket-auth.md)。
- **自己写 JWT 中间件**：`gofiber/contrib/jwt` 的 `Claims` 是跨请求共享的同一个对象，存在并发隐患；自己写能显式校验算法、`iss`、`aud`、`exp`。
- **先单实例，不先做高可用**：Loki、Prometheus、Kuma 都是单点。先把链路跑通、把备份和恢复演练做完，再谈集群。
- **不引入 WAF**：应用层不做抗 DDoS，明确依赖 Cloudflare。



## 作为模板使用

`backend/api`、`backend/ws`、`frontend/web` 同时就是这个仓库的三个模板，
没有第二份副本。生成一个独立项目：

```bash
python devops/scripts/new-project.py --template api --name my-service \
  --target ../my-service --module github.com/acme/my-service --init-git
```

生成结果自包含：改好 module path 与所有 import、带上自己的 Dockerfile、
生成随机密钥的 `.env`。模板包含什么、如何验证见 [templates/README.md](templates/README.md)。

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

说明文档：后端接口见 `backend/api/README.md`，实时服务协议见 `backend/ws/README.md`，前端见 `frontend/web/README.md`，部署见 `devops/README.md`；安全边界与「每条控制对应哪个测试」见 [docs/security/THREAT-MODEL.md](docs/security/THREAT-MODEL.md)。
