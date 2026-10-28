# AgentForge Realtime（WebSocket 服务）

独立于 `backend/api` 的 Go module，使用 `net/http` + `github.com/coder/websocket`。
Fiber 只服务 HTTP API，实时连接不共用它的超时与关停语义。

## 为什么要有独立进程

- WebSocket 连接寿命以小时计，HTTP 的超时与优雅关停模型不适用。
- 两个服务能力边界不同：API 直连数据库，实时服务不碰数据库。
- 分开后可以单独重启、单独限流、单独扩容，故障不互相传导。

## 鉴权：一次性票据

**不接受** `Authorization` 头里的长期 token，也不接受把 token 放在 query 里。
流程是：

```
1. 客户端 POST /api/v1/ws/ticket       （Bearer token，API 服务）
2. API 返回 {ticket, expires_in}        （30 秒有效、单次使用、aud=agentforge-ws）
3. 客户端 GET /ws?ticket=...            （实时服务）
4. 实时服务校验签名、签发方、受众、有效期，并消费 jti
```

ticket 是 HS256 JWT，用**独立于访问 token 的密钥** `WS_TICKET_SECRET` 签名，
所以泄漏任何一个都不会让另一个失效。

单次使用靠进程内 jti 缓存实现。多实例部署时必须换成共享存储，否则同一张
ticket 可以在每个实例上各用一次。

> ticket 出现在 URL 里，因此 `/ws` 的访问日志只记录 path，不记录 query。
> Nginx 侧同理（见 `devops/nginx/agentforge.conf` 的独立 log_format）。

## 握手检查顺序

顺序是有意的：任何一项失败都在协议升级之前返回普通 HTTP 状态码，不占用连接名额。

| 顺序 | 检查 | 失败状态 | 指标 reason |
|---|---|---|---|
| 1 | 方法必须是 GET | 405 | `method` |
| 2 | Origin 必须在白名单内（缺失也算失败） | 403 | `origin` |
| 3 | ticket 有效且未被使用 | 401 | `ticket` |
| 4 | 全局 / 单用户 / 单 IP 连接数 | 429 或 503 | `limit_*` / `shutting_down` |

`server.go` 关闭了库自带的 Origin 校验（`InsecureSkipVerify`），因为库把
“没有 Origin”视为合法；本服务的策略更严格，由 `originAllowed` 统一判定。

## 协议

客户端 → 服务端：

```json
{"type":"subscribe","topic":"demo"}
{"type":"unsubscribe","topic":"demo"}
{"type":"publish","topic":"demo","data":{"hello":"world"}}
{"type":"ping"}
```

服务端 → 客户端：

```json
{"type":"subscribed","topic":"demo"}
{"type":"unsubscribed","topic":"demo"}
{"type":"message","topic":"demo","data":{"hello":"world"}}
{"type":"pong","ts":1761000000}
{"type":"error","code":"UNKNOWN_TYPE"}
```

错误码：`INVALID_MESSAGE`、`INVALID_TOPIC`、`INVALID_PAYLOAD`、`UNKNOWN_TYPE`。
topic 只允许 `[a-z0-9][a-z0-9_-]{0,31}`。

## 关闭码

| 码 | 含义 | 触发条件 |
|---|---|---|
| 1000 | 正常关闭 | 客户端主动关闭 |
| 1001 | 服务端重启 | SIGTERM 排空 |
| 1008 | 策略违规 | 单连接消息速率超限 |
| 1009 | 消息过大 | 超过 `WS_MAX_MESSAGE_BYTES` |
| 1011 | 异常关闭 | 读写失败 |
| 1013 | 稍后重试 | 发送队列满（慢消费者） |
| 4001 | 空闲超时 | 心跳无响应或长时间无消息 |

## 连接保护

- 读限制 `WS_MAX_MESSAGE_BYTES`（默认 32 KiB），只接受文本帧。
- 心跳：服务端每 `WS_PING_INTERVAL` 发送 ping，pong 失败即关闭。
- 慢消费者：每条连接有 `WS_SEND_QUEUE_SIZE` 大小的发送队列，满了直接关闭，
  **不会**阻塞读循环，也不允许队列无限增长。
- 单连接消息速率：令牌桶 `WS_MESSAGE_RATE` / `WS_MESSAGE_BURST`。
- 连接上限：全局、单用户、单 IP 三层。

## 配置

见 [.env.example](./.env.example)。关键项：

| 变量 | 说明 |
|---|---|
| `WS_ADDR` | 公网监听，默认 `:8090` |
| `WS_MANAGEMENT_ADDR` | 管理端口，默认 `127.0.0.1:8091` |
| `WS_ALLOWED_ORIGINS` | 精确 Origin 白名单，禁止通配符 |
| `WS_TICKET_SECRET` | 必须与 API 服务一致，且不同于 `JWT_SECRET` |
| `WS_TRUSTED_PROXIES` | 允许提供 `X-Forwarded-For` 的代理地址，默认空 |

## 本地启动

```powershell
cd backend\ws
Copy-Item .env.example .env
go run ./cmd/ws
```

自检（需要 API 与实时服务都在运行）：

```powershell
.\devops\scripts\ws-smoke.ps1
```

它会完成：登录取 token → 换取 ticket → 连接 → 订阅 → 发布 → 心跳，
全部通过时输出 `RESULT: PASS`。

## 测试

```powershell
go test ./...          # 含握手拒绝、ticket 重放、超限、慢消费者、优雅关闭
go test -race ./...
```

> 关闭握手最长约 5 秒（库在等待对端回帧），所以 `SHUTDOWN_TIMEOUT`
> 必须大于 5 秒，默认给 20 秒。
