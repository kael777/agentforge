# AgentForge API 后端

AgentForge 的独立后端工程，使用 Go、Fiber v3、GORM 和 MySQL，默认监听 `55555`。Docker、Nginx、systemd 和发布脚本位于仓库根目录的 `devops`，前端位于 `frontend/web`。

## 已包含

- 构造函数依赖注入，不使用业务全局单例。
- Handler、Service、Repository 分层。
- MySQL 8.4、GORM 连接池和显式 SQL migration。
- JWT 注册、登录和鉴权，token 带 `iss`/`aud`/`jti` 并在每个请求上校验。
- 按当前用户隔离的 Todo POST/GET 示例。
- `/healthz`、Request ID、结构化日志和优雅退出。
- 公网端口与管理端口分离：`/readyz`、`/version`、`/metrics` 只在管理端口暴露。
- 响应头脱敏、日志字段脱敏、登录限流、生产配置校验。
- 非生产环境自动加载 `.env`；系统环境变量优先。

## 安全边界

| 控制 | 位置 | 对应测试 |
| --- | --- | --- |
| `/healthz` 只返回 `{"status":"ok"}` | `internal/modules/health` | `TestLivenessExposesStatusOnly` |
| 管理端点不在公网路由 | `internal/server` | `TestPublicListenerDoesNotExposeManagementRoutes`、`TestLivePublicSurface` |
| 响应头无 `Server`/`X-Powered-By` | `internal/server/middleware.go` | `TestHardeningMiddlewareRemovesFingerprints` |
| 日志脱敏（密钥、DSN、token） | `internal/platform/logging/redact.go` | `TestSensitiveAttributesAreRedacted` 等 |
| token 校验算法/签发方/受众/过期 | `internal/modules/auth/middleware.go` | `TestMiddlewareRejectsInvalidTokens` |
| 登录限流 | `internal/server/middleware.go` | `TestAuthRateLimitStopsRepeatedAttempts` |
| 生产配置不可含糊 | `internal/config/config.go` | `TestLoadRejects*` |

新增安全控制时，请同时补上对应测试，否则该控制不算完成。

## 目录

```text
cmd/api                 HTTP 服务入口
cmd/migrate             数据库迁移入口
internal/app            composition root
internal/config         环境变量配置和校验
internal/modules/auth   注册、登录和 JWT
internal/modules/todo   POST/GET 分层示例
internal/platform       MySQL、日志和 migration
internal/server         Fiber、中间件和路由
internal/transport      HTTP 公共响应
```

## 本地启动

确保本机 MySQL 已启动并已创建 `agentforge` 数据库。首次启动：

```powershell
Copy-Item .env.example .env
# 修改 .env 中的 DATABASE_DSN 和 JWT_SECRET
go run ./cmd/migrate
go run ./cmd/api
```

`.env` 已被 `.gitignore` 排除。外部设置 `APP_ENV=production` 时程序完全跳过 `.env`，生产配置由 systemd、Docker 或 secret 管理器注入。

健康检查（公网端口与管理端口分离）：

```powershell
# 公网端口：只有最小存活探针
Invoke-RestMethod http://localhost:55555/healthz

# 管理端口：运维与采集用，不要对外暴露
Invoke-RestMethod http://localhost:55556/readyz
Invoke-RestMethod http://localhost:55556/version
Invoke-WebRequest http://localhost:55556/metrics
```

## POST / GET 示例

先注册并保存 JWT：

```powershell
$registerBody = @{
  email = "demo@example.com"
  password = "password123"
} | ConvertTo-Json

$auth = Invoke-RestMethod `
  -Method Post `
  -Uri "http://localhost:55555/api/v1/auth/register" `
  -ContentType "application/json" `
  -Body $registerBody

$headers = @{ Authorization = "Bearer $($auth.token)" }
```

创建 Todo：

```powershell
$todo = Invoke-RestMethod `
  -Method Post `
  -Uri "http://localhost:55555/api/v1/todos" `
  -Headers $headers `
  -ContentType "application/json" `
  -Body '{"title":"完成 Agent Demo"}'
```

查询 Todo：

```powershell
Invoke-RestMethod `
  -Method Get `
  -Uri "http://localhost:55555/api/v1/todos/$($todo.id)" `
  -Headers $headers
```

**公网端口 `:55555`**

| 方法 | 路径 | 鉴权 | 说明 |
| --- | --- | --- | --- |
| `GET` | `/healthz` | 否 | 只返回 `{"status":"ok"}`，不含版本、提交、环境、运行时长 |
| `POST` | `/api/v1/auth/register` | 否 | 注册并返回 JWT（限流） |
| `POST` | `/api/v1/auth/login` | 否 | 登录并返回 JWT（限流） |
| `GET` | `/api/v1/users/me` | 是 | 当前用户 |
| `POST` | `/api/v1/todos` | 是 | 创建 Todo |
| `GET` | `/api/v1/todos/:id` | 是 | 查询当前用户的 Todo |

**管理端口 `127.0.0.1:55556`**

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/readyz` | MySQL 就绪状态，用于部署门禁和摘流 |
| `GET` | `/version` | 服务名、版本、commit、构建时间、环境、运行时长 |
| `GET` | `/metrics` | Prometheus 指标 |

## 开发命令

```bash
go mod tidy
gofmt -w ./cmd ./internal
go test ./...
go vet ./...
go build ./cmd/api ./cmd/migrate
```

从后端目录使用 Docker：

```bash
docker compose -p agentforge -f ../../devops/docker/docker-compose.yml up --build
```

完整 Docker、跨平台构建、systemd、Nginx 和回滚说明见 `../../devops/README.md`。
