# ADR-0002：HTTP 与 WebSocket 拆分，以及一次性票据鉴权

- 状态：接受
- 日期：2026-10-28
- 关联：[ADR-0001](./0001-repository-and-platform-baseline.md)、[威胁模型](../security/THREAT-MODEL.md)

## 背景

Agent 类应用需要一条服务端主动推送的通道：任务进度、流式输出、多端协同都依赖它。HTTP 轮询在这里既浪费也更难保证顺序。

但在现有 Go API 进程里直接挂一条 `/ws` 路由有三个具体问题：

1. **超时模型冲突**。API 的 `ReadTimeout=10s`、`WriteTimeout=30s` 是按请求-响应设计的；WebSocket 连接寿命以小时计，共用这两个值会周期性切断健康连接。
2. **关停语义冲突**。`http.Server.Shutdown` 不会等待被 hijack 的连接。混在一个进程里，"先停接入、再排空连接、最后退出" 这个顺序没有清晰的所有者。
3. **能力边界模糊**。长连接服务如果同时握有数据库凭据和全部认证逻辑，它的攻击面就等于整个后端。

同时，浏览器侧的 WebSocket 不受同源策略保护：任何页面都能向任意地址发起握手。因此鉴权不能依赖 Cookie 自动携带。

## 决策

1. **拆成两个进程、两个 Go module**
   - `backend/api`（Fiber + GORM + MySQL）：业务读写、签发票据。
   - `backend/ws`（`net/http` + `coder/websocket`）：只维护连接与广播，**不连数据库**。
   - 两者不共享 Go 代码，只通过配置与令牌格式这一份契约通信。

2. **WebSocket 用一次性票据鉴权**
   ```
   POST /api/v1/ws/ticket   (Bearer token)  →  {ticket, expires_in: 30}
   GET  /ws?ticket=...                       →  101 Switching Protocols
   ```
   - 票据是 HS256 JWT，`aud=agentforge-ws`，`iss` 与访问 token 相同，`jti` 保证单次使用。
   - 密钥 `WS_TICKET_SECRET` 与 `JWT_SECRET` **必须不同**，由启动期校验强制。
   - 票据不接受过期后的补签；TTL 上限 2 分钟，由 API 与 WS 双向校验。

3. **Origin 精确白名单，缺失即拒绝**
   - 不使用通配符，不依赖 `Bearer` 头里的 token。
   - 关闭了 `coder/websocket` 自带的 Origin 校验（它把「没有 Origin」视为合法），改由 `originAllowed` 统一判定。

4. **每个服务都有公网与管理两个监听**
   - 公网：`/healthz` + 业务路由。
   - 管理：`/readyz`、`/version`、`/metrics`，默认绑定回环。

5. **WS 用 `net/http`，不复用 Fiber**
   - 独立进程后没有理由引入 fasthttp 的 hijack 路径；`coder/websocket` 原生基于 `http.ResponseWriter`，context 语义完整，关闭码明确。

## 备选方案

| 方案 | 为什么不选 |
|---|---|
| 同进程同端口挂 `/ws` | 超时与关停语义冲突；一个进程同时承担长连接与业务，扩容粒度被绑死 |
| 同进程不同端口 | 解决端口问题，但关停顺序、故障隔离、部署单元仍然混在一起 |
| WS 直接验证访问 token | WS 需要复制整套 JWT 校验（算法、iss/aud、将来还有吊销），两份实现会漂移；且长连接服务要面对长期有效的凭据 |
| Cookie 会话 | 跨域与 CSWSH 都更难处理，还需要 CSRF/CORS 联动；服务端到服务端客户端也不方便 |
| 首帧认证（连上后发 token） | 多一个未认证窗口，连接先占用名额再决定去留；限流与连接上限要为它让路 |
| 两个进程共享一个内部 Go module | 编译期耦合会把两个模板重新绑在一起，且模板本应能独立生成 |
| 票据放 `Sec-WebSocket-Protocol` 头 | 可行，但会进入部分代理的请求日志；query + 短时效 + 单次使用同样可接受，且实现更直观 |

## 结果

### 正面的

- **故障隔离**：连接风暴不会拖垮 API；API 重启不影响已建立的推送通道（在排空窗口内）。
- **权限最小化**：WS 进程没有数据库凭据，被攻破也拿不到业务数据。
- **凭据短命**：票据 30 秒、单次、受众隔离。即使出现在 Nginx 日志或被浏览器历史记录，也已失效。
- **可独立演进**：两个服务的限流、扩容、发布节奏互不牵连。
- **可测试**：每个控制都有对应的 Go 测试，端到端由 `ws-smoke.ps1` / `ws-demo.py` 覆盖。

### 负面的

- **多一次 HTTP 往返**（取票）。对建立连接这种低频操作可以接受。
- **多一个部署单元**：两个 systemd unit、两份环境变量、两个管理端口。
- **配置耦合**：`WS_TICKET_SECRET` 必须在两处一致；不一致的表现是握手 401，而不是启动失败。当前只在启动期校验各自长度与占位值，没有跨进程一致性检查。
- **日志里出现 URL 参数**：票据在 query 中，因此 `/ws` 的访问日志必须裁剪 query（已实现，Nginx 侧也用了独立 `log_format`）。

### 以后要改的

| 场景 | 需要的变化 |
|---|---|
| 水平扩容 | 票据 jti 缓存与所有连接计数改为 Redis 等共享存储；否则同一票据可被每个实例各用一次 |
| 强制下线 | 需要一个 jti 黑名单或极短 TTL + refresh |
| 多前端域名 | 扩展 `WS_ALLOWED_ORIGINS`，仍然保持精确匹配 |
| 服务端到服务端客户端 | 显式打开 `WS_ALLOW_EMPTY_ORIGIN`，并为该来源单独限流 |
| 数据库 TLS | 目前应用与 MySQL 之间是明文 TCP |

## 参考实现位置

| 关注点 | 文件 |
|---|---|
| 票据签发 | `backend/api/internal/modules/realtime/service.go` |
| 票据校验与重放缓存 | `backend/ws/internal/ticket/ticket.go` |
| 握手顺序与 Origin 判定 | `backend/ws/internal/realtime/server.go` |
| 连接生命周期、心跳、慢消费者 | `backend/ws/internal/realtime/session.go` |
| 连接上限与广播 | `backend/ws/internal/realtime/hub.go` |
