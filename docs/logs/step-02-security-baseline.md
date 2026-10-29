# Step 2：后端安全基线、实时服务与模板化

> 日期：2026-10-28
> 环境：Windows 开发机、Docker Desktop、本地 MySQL
> 结果：完成。两个 module 测试全绿，govulncheck 0 可达漏洞，CI 绿

## 1. 起点

仓库当时是一个通用脚手架：`/healthz`、`/readyz`、JWT 注册登录、Todo 示例。
工作区里还有两个**未提交**的包——LLM provider 抽象与工具执行层（含 190 行路径沙箱），
没有接进任何路由，属于"带测试的死代码"。

问题不是代码质量，是方向：精力花在自研 Agent 框架上，而这个仓库的定位是
**可复用的安全脚手架**。方向文档要求它成为 Go Fiber 模板的首个验证对象，
重点是脱敏、最小暴露面、管理端口、优雅退出。

## 2. 交付清单

| 内容 | 结果 |
|---|---|
| 目录与命名统一 | `backend/gofiber-gorm-next` → `backend/api`，服务名 `agentforge-api` |
| API 安全基线 | 端口拆分、`/healthz` 最小化、响应头与日志脱敏、限流、token 作用域 |
| WebSocket 服务 | 独立 module、票据鉴权、连接保护、优雅关闭 |
| 三个模板 | 一条命令生成独立项目，CI 每次回归 |
| 安全文档 | 威胁模型 + ADR-0002 |
| 工具链 | Go 1.27.3，govulncheck 0 可达漏洞 |

## 3. 关键决策

**暴露面拆成两个监听口。** 公网只留 `/healthz` 与业务路由；`/readyz`、`/version`、
`/metrics` 移到 `127.0.0.1:55556`。理由：版本号与环境信息对攻击者的价值，
远大于对运维的价值；而运维需要这些信息时，应该在一个不该被公网访问的地方拿到它。

**HTTP 与 WebSocket 拆两个进程。** 超时模型冲突（API 按请求-响应设 10s/30s，
长连接会被周期性切断）、关停语义冲突（`Shutdown` 不等 hijack 的连接）、
能力边界模糊（长连接服务不该同时握数据库凭据）。完整论证与被否决的七个方案见 ADR-0002。

**票据而不是复用访问 token。** 访问 token 24 小时有效，WebSocket 握手需要一次性凭据。
票据 30 秒、单次使用、独立受众（`aud=agentforge-ws`）、独立密钥。

**模板不复制代码。** 三个服务本身就是模板，生成器负责改名与裁剪。
维持两份副本必然漂移，而这个仓库的安全控制全落在真实服务上。

**自己写 JWT 中间件。** 见第 4.5 节。

**先单实例。** Loki、Prometheus、Kuma 都是单点，第一版不做集群——先把链路跑通，
把备份与恢复演练做完，再谈高可用。

## 4. API 安全基线

### 4.1 端口拆分

`internal/app` 产出两个 Fiber 实例，`cmd/api` 用两个 `net.Listener` 分别绑定：

```go
publicListener, err := net.Listen("tcp", cfg.HTTP.Address)
managementListener, err := net.Listen("tcp", cfg.HTTP.ManagementAddress)
```

先 Listen 再起 goroutine：绑定失败要在启动时报错，而不是在服务汇报 ready 之后
才在后台炸掉。

| 端口 | 路由 |
|---|---|
| `:55555` 公网 | `/healthz`、`/api/v1/*` |
| `127.0.0.1:55556` 管理 | `/readyz`、`/version`、`/metrics` |

配置校验强制两条：管理端口必须是回环地址（非回环要显式 `MANAGEMENT_ALLOW_NON_LOOPBACK=true`），
且不能与公网端口相同。

### 4.2 `/healthz` 只回答"进程活着"

改造前返回 7 个字段：service、version、commit、build_time、env、uptime、time。
现在只有一个：

```json
{"status":"ok"}
```

用测试锁死字段数，而不是锁死字符串：

```go
if len(body) != 1 || body["status"] != "ok" { t.Fatalf(...) }
```

这样以后有人加字段会立刻失败。

### 4.3 响应头

中间件在**进入时设置、返回前再删一次**：

```go
func hardenResponse() fiber.Handler {
    return func(c fiber.Ctx) error {
        setHardeningHeaders(c)   // nosniff / DENY / no-referrer / no-store
        err := c.Next()
        stripFingerprints(c)     // 删除 Server / X-Powered-By / X-Runtime ...
        return err
    }
}
```

删两次的原因：进入时删的是已有头，但如果某个 handler 或依赖自己又设了 `Server`，
只删一次会漏。测试里故意让 handler 设置 `Server` 再断言最终响应为空。

`Cache-Control: no-store` 是安全项不是性能项：API 响应是用户私有数据，
被中间缓存命中就是数据泄漏。

### 4.4 日志脱敏

做成 `slog.Handler` 装饰器，而不是"记得调用脱敏函数"：

```go
func NewWithHandler(h slog.Handler) *slog.Logger {
    return slog.New(&redactingHandler{inner: h})
}
```

`logging.New()` 默认套上，不存在忘记的路径。三层规则：

| 层 | 处理 |
|---|---|
| 键名 | `authorization`、`cookie`、`*token`、`*secret`、`*password`、`*dsn` → `[REDACTED]` |
| 自由文本 | Bearer、JWS（`eyJ...`）、MySQL DSN、`password=` 片段 → 替换 |
| 结构化值 | `slog.KindAny`（error、fmt.Stringer）拍平成字符串再洗 |

第三层是关键：错误对象直接编码会绕过键名检查。实测中 MySQL 的
`Access denied for user 'u'@'...' (using password: YES)` 被成功改写。

访问日志固定 5 个字段（method/path/status/耗时/request_id），**不记 query、不记 header**。

### 4.5 鉴权：自己写中间件

换掉了 `gofiber/contrib/v3/jwt`，两个原因：

1. 它的 `Claims` 是配置里的同一个对象，**跨请求共享**，并发下有竞争。
2. 需要显式控制校验项，而不是依赖默认值。

```go
options := []jwt.ParserOption{
    jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
    jwt.WithIssuer(cfg.Issuer),
    jwt.WithAudience(cfg.Audience),
    jwt.WithExpirationRequired(),
    jwt.WithLeeway(30 * time.Second),
}
```

算法在 keyfunc 里再查一次（`token.Method.Alg() != HS256` 就拒绝），阻断算法混淆。
token 增加 `jti` 为将来单条吊销留余地。

### 4.6 限流与账号枚举

**限流**：注册与登录各自一个内存限流器，默认 10 次/分钟/来源 IP。

`X-Forwarded-For` 只在配置了可信代理时采信：

```go
TrustProxy: len(cfg.HTTP.TrustedProxies) > 0,
TrustProxyConfig: fiber.TrustProxyConfig{Proxies: cfg.HTTP.TrustedProxies},
```

否则任何客户端都能伪造源 IP 绕过限流。

**枚举时序**：账号不存在时也跑一次 bcrypt 对比（对着固定的 dummy hash），
否则"不存在"几微秒返回、"密码错"几十毫秒返回，本身就是账号枚举的旁路。

### 4.7 配置 fail-fast

生产环境启动即拒绝：

| 检查 | 原因 |
|---|---|
| `HTTP_ALLOWED_ORIGINS` 不能是 `*` | 通配 Origin 等于没有 CORS |
| `JWT_SECRET` 不能是仓库占位值 | 占位值是公开的 |
| `WS_TICKET_SECRET` 不能等于 `JWT_SECRET` | 一个泄漏不该连坐另一个 |
| 票据 TTL ≤ 2 分钟 | 防止签出长期凭据 |
| 管理端口必须是回环 | 见 4.1 |

## 5. WebSocket 服务

### 5.1 独立进程

`backend/ws` 是独立 Go module，用 `net/http` + `coder/websocket`，不共享 API 的代码。

选 `coder/websocket` 而不是 `gorilla` 或 fiber 的 contrib：基于 `http.ResponseWriter`，
context 语义完整（`Read(ctx)` 超时即取消），关闭码定义清晰。独立进程后没有理由再背
fasthttp 的 hijack 路径。

### 5.2 一次性票据

```
POST /api/v1/ws/ticket  (Bearer)  →  {ticket, expires_in: 30}
GET  /ws?ticket=...                →  101 Switching Protocols
```

- HS256 JWT，`aud=agentforge-ws`，`jti` 保证单次使用
- 密钥 `WS_TICKET_SECRET` 与 `JWT_SECRET` 不同，启动期强制
- WS 侧校验签名、iss、aud、exp，并检查**最长寿命**（≤2 分钟），防止签出长效票
- jti 存进程内缓存，用后即失效

**为什么不把 token 放 query：** query 会进各级日志和浏览器历史。票据短命 + 单次，
即使泄漏也无效；长期 token 则不然。

代价要写清楚：**单次使用依赖进程内缓存，多实例部署时同一张票能在每个实例各用一次**。

### 5.3 握手顺序

顺序是有意的，任何一步失败都在协议升级**之前**返回普通 HTTP 状态码，不占连接名额：

| 顺序 | 检查 | 失败码 |
|---|---|---|
| 1 | 方法必须是 GET | 405 |
| 2 | Origin 精确白名单（缺失也拒绝） | 403 |
| 3 | 票据有效且未使用 | 401 |
| 4 | 全局 / 单用户 / 单 IP 连接上限 | 429 / 503 |

关了库自带的 Origin 校验（`InsecureSkipVerify: true`），因为它把"没有 Origin"
视为合法；本服务的策略更严格，由 `originAllowed` 统一判定。

### 5.4 连接保护

| 保护 | 做法 |
|---|---|
| 消息大小 | `SetReadLimit(32 KiB)`，超限 1009 |
| 帧类型 | 只收文本帧，二进制 1003 |
| 速率 | 单连接令牌桶（20/秒，突发 40），超限 1008 |
| 心跳 | 服务端每 30s ping，pong 失败 4001 |
| 慢消费者 | 有界发送队列（256），满了 1013 关闭，**绝不阻塞读循环** |
| topic | `^[a-z0-9][a-z0-9_-]{0,31}$` |

慢消费者那条是重点：读循环负责产出消息，如果在入队时阻塞，一个不读的客户端
就能拖住整个连接的处理。

### 5.5 关闭语义

| 码 | 含义 |
|---|---|
| 1000 | 客户端主动关闭 |
| 1001 | 服务端重启（排空） |
| 1008 | 速率超限 |
| 1009 | 消息过大 |
| 1013 | 慢消费者 |
| 4001 | 空闲超时（自定义） |

关闭帧**必须先写出去再取消 context**：先 cancel 会让库直接拆连接，
对端只看到 EOF、拿不到关闭码——而客户端正是靠这个码决定要不要重连。

## 6. 模板化

三个服务本身就是模板，`devops/scripts/new-project.py` 负责改名与裁剪：

- Go 模板：重写 `go.mod` module path、所有 internal import、服务名、Dockerfile 的 ldflags
- Web 模板：改 `package.json` name、标题，带上 `nginx.conf` 与 `.dockerignore`
- 生成 `.env` 时把占位密钥换成随机值

CI 的 `templates` job 每次生成三个项目、跑测试与构建、并断言生成物里**不再出现**
源仓库路径。这条断言第一次跑就抓到了生成器的缺陷（见第 7 节）。

## 7. 踩的坑

| 坑 | 现象 | 处理 |
|---|---|---|
| `/healthz` 泄露版本 | 返回 7 个字段 | 收敛为最小响应，元数据移到管理端口 |
| 前端按旧结构解析 | 后端改了，前端仍按 `service/version` 解析，运行时误报"后端未连接" | 同步 schema 与测试 fixture |
| `contrib/jwt` 的 Claims 跨请求共享 | 并发隐患 | 自己写中间件 |
| `app.Test` 不经过真实服务端 | 测不出 fasthttp 是否加了 `Server` 头 | 加真实监听端口的冒烟测试 |
| 关闭握手死锁 | 阻塞的 `Read` 持有读锁，`conn.Close` 要等满读超时 | 关闭帧异步投递，排空交给 hub 限时 |
| 握手/关闭竞态 | `CloseAll` 在 `conn` 还没赋值时关闭，socket 泄漏、连接计数只增不减 | `Attach` 与 `Close` 互斥，未 attach 立即关 socket |
| 库的关闭握手最长 5 秒 | 关停预算不足会切断在线连接 | `SHUTDOWN_TIMEOUT=20s`、systemd `TimeoutStopSec=25s` |
| govulncheck 报 12 条 stdlib 漏洞 | 工具链落后 6 个补丁版本 | `go.mod` 声明最低工具链 `go 1.27.3` |
| govulncheck 二进制版本不匹配 | 升 Go 后报 `package requires newer Go version` | 重新 `go install`，CI 每次装最新 |
| Gitleaks 报 6 条"密钥" | 全是测试常量与 `.env.example` 占位值 | `.gitleaks.toml` 用精确正则放行，不整类文件放行 |
| 生成项目残留源仓库 | Dockerfile 的 OCI `source` 标签仍指向本仓库 | CI 断言抓到，修生成器 |
| Vite 代理前缀误伤 | `/ws-console.html` 被 `/ws` 规则转给了实时服务 | 页面改名 `realtime-console.html` |

最后一条与倒数第二条是同一类问题：**前缀匹配 / 字符串替换的边界**。
断言和改名都比"记得"可靠。

## 8. 验收

```bash
# 1. 测试与静态检查
(cd backend/api && go test ./... && go vet ./...)
(cd backend/ws  && go test ./... && go vet ./...)
(cd frontend/web && npm ci && npm run check)

# 2. 依赖可达漏洞
go install golang.org/x/vuln/cmd/govulncheck@latest
(cd backend/api && "$(go env GOPATH)/bin/govulncheck" ./...)
(cd backend/ws  && "$(go env GOPATH)/bin/govulncheck" ./...)

# 3. 最小暴露面（需要 API 在跑）
curl -sI http://127.0.0.1:55555/healthz | grep -iE 'server|x-powered-by'   # 无输出
curl -s  http://127.0.0.1:55555/healthz                                    # {"status":"ok"}
for p in readyz metrics version; do
  printf '%-8s %s\n' "$p" "$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:55555/$p)"
done                                                                        # 404 404 404

# 4. 登录限流
for i in $(seq 1 12); do
  curl -s -o /dev/null -w '%{http_code} ' -X POST http://127.0.0.1:55555/api/v1/auth/login \
    -H 'Content-Type: application/json' -d '{"email":"a@b.com","password":"x"}'
done                                                                        # 前 10 次 401，之后 429

# 5. 端到端（需要 API + WS 都在跑）
python devops/scripts/ws-demo.py
```

| 项 | 结果 |
|---|---|
| 两个 module 的 test / vet / gofmt | 全绿 |
| govulncheck | 两处均 0 可达漏洞 |
| 三个模板生成 | 测试与构建通过，无源仓库残留 |
| 响应头 | 无 `Server` / `X-Powered-By` |
| 公网管理端点 | `/readyz` `/metrics` `/version` 均 404 |
| 登录限流 | 第 11 次起 429 |
| WebSocket 端到端 | 取票、订阅、广播、心跳、票据重放拒绝 |

## 9. 遗留

- 票据单次使用依赖进程内缓存，多实例需改 Redis 或回源校验
- 访问 token 无吊销机制，登出后仍有效至过期（24h）
- 镜像层漏洞扫描未接入，只做了模块级依赖扫描
- 管理端口在容器里绑 `0.0.0.0`（不发布），未做容器级别的端口隔离验证
