# AgentForge 威胁模型

> 状态：随代码维护
> 适用版本：v0.1.x 基线
> 读者：改动这个仓库的人，以及要判断它能不能承载真实流量的人

本文回答三个问题：**保护什么、防谁、凭什么认为防住了**。

规则：新增任何对外入口，必须同时更新第 5 节，并补一条对应测试。
没有测试的控制视为未实现。

---

## 1. 系统组成

两个独立进程，各自有公网监听和管理监听：

| 进程 | 公网监听 | 管理监听 | 职责 | 是否连数据库 |
|---|---|---|---|---|
| `agentforge-api` | `:55555` | `127.0.0.1:55556` | 注册登录、业务读写、签发 WebSocket 票据 | 是 |
| `agentforge-ws` | `:8090` | `127.0.0.1:8091` | 维护长连接、订阅与广播 | 否 |

拆分的理由见 [ADR-0002](../adr/0002-http-ws-split-and-ticket-auth.md)。

## 2. 信任边界

```text
        浏览器 / 第三方客户端（不可信）
                    │  HTTPS / WSS
                    ▼
        ┌───────────────────────┐
        │  Nginx（唯一公网入口） │  TLS 终止、静态资源
        └───────────┬───────────┘
            /api/*  │  /ws
                    ├──────────────► API :55555
                    │                    │
                    └──────────────► WS :8090
                                         │
   运维 / 采集（私网或回环，不可信程度：中）
                    │
                    ├──────────────► API :55556   /readyz /version /metrics
                    └──────────────► WS :8091     /readyz /version /metrics

   数据面：API :55555 ──明文 TCP──► MySQL（当前无 TLS）
```

边界含义：

- **Nginx 之后的所有请求都视为不可信**，包括带 `X-Forwarded-For` 的请求——只有配置了 `HTTP_TRUSTED_PROXIES` / `WS_TRUSTED_PROXIES` 的源地址才采信该头。
- **管理监听是第二个信任边界**。它默认只绑回环地址；容器部署时不发布端口。`/readyz`、`/version`、`/metrics` 一旦落在公网网卡上，等于把依赖状态、版本号和内部分布式信息直接交出去。

## 3. 资产

| 资产 | 位置 | 影响 |
|---|---|---|
| 用户凭证（bcrypt 哈希） | MySQL | 泄漏后离线爆破 |
| 访问 token（JWT，24h） | 客户端 | 泄漏后冒用身份直到过期 |
| WebSocket 票据（JWT，30s，单次） | URL query | 泄漏后 30 秒内可开一条连接 |
| `JWT_SECRET` / `WS_TICKET_SECRET` | 环境变量 | 泄漏后可伪造任意身份或票据 |
| 业务数据（Todo 等） | MySQL | 越权读写 |
| 日志 | stdout → 采集 | 含凭证则等同凭证泄漏 |

## 4. 密钥与凭据的最小集合

| 变量 | 用途 | 约束 |
|---|---|---|
| `JWT_SECRET` | 签访问 token | ≥32 字符；生产拒绝仓库占位值 |
| `WS_TICKET_SECRET` | 签 WebSocket 票据 | ≥32 字符；**必须不同于** `JWT_SECRET`；两个服务必须相同 |

两者分离的意义：任何一个泄漏都不会让另一个失效。这条约束由 `TestLoadRejectsSharedAccessAndTicketSecrets` 强制。

## 5. 攻击面与现有控制

### 5.1 API 公网 `:55555`

| 入口 | 威胁 | 控制 | 测试 |
|---|---|---|---|
| `GET /healthz` | 通过版本/提交/环境信息挑选已知漏洞 | 只返回 `{"status":"ok"}`；元数据移到管理端口 | `TestLivenessExposesStatusOnly`、`TestLivePublicSurface` |
| `POST /api/v1/auth/register` | 批量注册、暴力试探 | 按来源 IP 限流（默认 10/分钟） | `TestAuthRateLimitStopsRepeatedAttempts` |
| `POST /api/v1/auth/login` | 密码爆破；账号枚举 | 限流 + 未知账号也执行一次 bcrypt（消除时序差） | 同上、`TestLoginHidesUnknownAccounts` |
| `GET /api/v1/users/me` | 未授权访问 | JWT 中间件校验签名、算法、`iss`、`aud`、`exp` | `TestMiddlewareRejectsInvalidTokens` |
| `GET /api/v1/todos/:id` | 越权读取他人数据（IDOR） | 查询条件绑定当前 subject | `TestGetDoesNotExposeAnotherUsersTodo` |
| `POST /api/v1/todos` | 超长/畸形输入 | 标题去空白后非空且 ≤200 字符；`userID`/`id` 必须是 UUID；全局 body 上限 2 MiB | `TestCreateRejectsInvalidTitle` |
| `POST /api/v1/ws/ticket` | 为他人取票；长期凭据 | 必须已认证且只能为自己签；TTL 上限 2 分钟 | `TestIssueProducesScopedShortLivedTicket`、`TestLoadRejectsLongTicketTTL` |
| 任意响应 | 技术栈指纹 | 中间件清除 `Server`、`X-Powered-By` 等；补 `nosniff`、`no-store`、`no-referrer` | `TestHardeningMiddlewareRemovesFingerprints` |
| 任意请求 | 请求头/query 进日志 | 访问日志固定字段（method/path/status/耗时），不记 query 与 header | 代码评审 + `TestSensitiveAttributesAreRedacted` |

### 5.2 WS 公网 `:8090`

握手顺序是设计的一部分：**任何检查失败都在协议升级之前返回，不占用连接名额。**

| 顺序 | 入口 | 威胁 | 控制 | 测试 |
|---|---|---|---|---|
| 1 | `GET /ws` | 非 GET 探测 | 405 | — |
| 2 | `GET /ws` | 跨站 WebSocket 劫持（CSWSH） | Origin **精确**白名单；缺失 Origin 默认拒绝；`*` 启动即失败 | `TestHandshakeRejectsMissingOrigin`、`TestHandshakeRejectsForeignOrigin` |
| 3 | `GET /ws?ticket=` | 无票/伪造/过期/重放 | HS256 验签 + `iss`/`aud`/`exp` + jti 单次使用 | `TestHandshakeRejectsBadTickets`、`TestTicketCannotBeReplayed` |
| 4 | `GET /ws` | 连接洪泛 | 全局 / 单用户 / 单 IP 三层上限 | `TestPerUserConnectionLimit` |
| 5 | 帧 | 超大消息 | `SetReadLimit`，超限以 1009 关闭 | `TestOversizedMessageClosesConnection` |
| 6 | 帧 | 二进制帧绕过解析 | 只接受文本帧，其余 1003 | — |
| 7 | 帧 | 消息洪泛 | 单连接令牌桶，超限 1008 | — |
| 8 | topic | 目录穿越式 topic | `^[a-z0-9][a-z0-9_-]{0,31}$` | `TestInvalidTopicIsRejected` |
| 9 | 出站队列 | 慢消费者拖垮进程 | 有界队列，满即 1013 关闭，绝不阻塞读循环 | `TestSlowConsumerIsDisconnected` |
| 10 | 关停 | 断连不给关闭码 | 先停接入，再并行广播 1001，按预算排空 | `TestGracefulShutdownSendsGoingAway` |
| 11 | 日志 | 票据经 URL 进日志 | 访问日志只记 path，不记 query；Nginx 侧独立 `log_format` | 配置评审 |

### 5.3 管理端口 `:55556` / `:8091`

| 入口 | 威胁 | 控制 |
|---|---|---|
| `/readyz`、`/version`、`/metrics` | 暴露内网拓扑与版本 | 默认绑 `127.0.0.1`；非回环绑定必须显式 `*_ALLOW_NON_LOOPBACK=true`；容器部署不发布端口 |

对应测试：`TestPublicListenerDoesNotExposeManagementRoutes`、`TestManagementListenerExposesDiagnostics`、`TestLoadRejectsPublicManagementAddress`、`TestLivePublicSurface`。

### 5.4 日志与可观测性

| 威胁 | 控制 | 测试 |
|---|---|---|
| 凭证写入日志 | 结构化 handler 统一脱敏：敏感键名、嵌套 group、Bearer、JWS、MySQL DSN、`password=` 片段 | `TestSensitiveAttributesAreRedacted`、`TestErrorsAndMessagesAreScrubbed`、`TestNestedGroupsAreRedacted` |
| 调用方忘记脱敏 | 脱敏包在 handler 里，`logging.New` 默认开启，不存在「忘记调用」的路径 | 同上 |
| 指标标签基数爆炸 | 以路由模板而非原始路径打标签；消息类型白名单 | `TestMiddlewareLabelsByRoutePattern` |

### 5.5 依赖与构建

| 威胁 | 控制 |
|---|---|
| 依赖已知漏洞 | `govulncheck` 扫描两个 module 的**可达**调用路径（CI `security` job） |
| 标准库漏洞 | `go.mod` 声明最低工具链 `go 1.27.3`，低于该版本自动升级 |
| 前端运行时依赖漏洞 | CI 执行 `npm audit --omit=dev --audit-level=high` |
| 基础镜像被重新指向 | Dockerfile 按 digest 固定 `golang` / `alpine` |
| 本地密钥进入镜像 | `.dockerignore` 排除 `.env`、`bin/` |
| 密钥进入仓库 | Gitleaks（`secret-scan.yml`） |

### 5.6 运行时

| 威胁 | 控制 |
|---|---|
| 容器逃逸面 | 非 root（`uid=100`）、无 shell、无 home、多阶段构建仅带二进制 |
| 服务被利用后持久化/提权 | systemd：`ProtectSystem=strict`、`NoNewPrivileges`、`CapabilityBoundingSet=`、`SystemCallFilter=@system-service`、`RestrictAddressFamilies=`、`MemoryDenyWriteExecute` |

## 6. 明确不在范围内

写下来是为了避免误判这份文档的覆盖面：

- **不承诺抗 DDoS**。应用层没有 WAF、没有 IP 信誉库，依赖 Cloudflare / 云负载均衡。
- **不做多租户隔离**。当前是单租户；`sub` 只区分用户，不区分组织。
- **不做静态数据加密**。MySQL 明文存储，依赖磁盘/卷加密。
- **不做审计日志持久化**。日志只到 stdout，由采集侧负责保留。
- **不做 MFA、密码复杂度策略、账号锁定**。当前只有长度 8–72 的校验和登录限流。
- **不做数据库 TLS**。应用与 MySQL 之间是明文 TCP，同机或私网部署为前提。
- **不做镜像层漏洞扫描**。只做模块级依赖扫描。

## 7. 已知残留风险

按「多严重 × 什么时候会真的出问题」排序，都是知情接受，不是遗漏：

| # | 风险 | 影响 | 触发条件 | 缓解 |
|---|---|---|---|---|
| 1 | 票据重放缓存是进程内内存 | 多实例部署时同一票据可在每个实例各用一次 | 水平扩容 | 单实例部署；扩容前改为 Redis 或回源校验 |
| 2 | 限流、连接上限都按进程计数 | 实际额度 = 单进程额度 × 实例数 | 水平扩容 | 同上 |
| 3 | 访问 token 无吊销机制 | 登出/封禁后 token 仍有效至过期（24h） | 需要强制下线 | 缩短 `JWT_TTL`；后续加 jti 黑名单 |
| 4 | 对称密钥、无 `kid` | 无法平滑轮换密钥 | 需要换密钥时 | 换密钥即全员重新登录，接受该成本 |
| 5 | Origin 白名单是精确匹配 | 多前端域名要逐个维护 | 新增站点 | 加配置项；`*` 永远不允许 |
| 6 | 脱敏是「敏感键名黑名单」 | 名字奇怪的密钥字段可能漏过 | 新增日志字段 | 访问日志用固定字段；新增字段走代码评审 |
| 7 | 管理端口靠绑定地址保护 | 配置成 `0.0.0.0` 且端口可达即暴露 | 部署配置错误 | 启动期校验 + 生产环境拒绝非回环 |
| 8 | Nginx 未配置 CSP | 静态站点的 XSS 缓解弱于 API | 前端出现注入点 | 后续在 Nginx 侧加 CSP 头 |
| 9 | systemd 加固未在真机验证 | 极端情况下 unit 可能起不来 | 首次上 Linux | 上线前在一台测试机跑一次 `systemd-analyze security` |

## 8. 如何验证本文没写错

```bash
# 控制矩阵里的测试
cd backend/api && go test ./... && go vet ./...
cd backend/ws  && go test ./... && go vet ./...

# 依赖可达漏洞
govulncheck ./...

# 端到端：取票、连接、订阅、广播、心跳、票据重放
.\devops\scripts\ws-smoke.ps1
python devops\scripts\ws-demo.py
```

公开入口的最小披露可以直接观测：

```bash
curl -I http://localhost:55555/healthz      # 无 Server / X-Powered-By
curl http://localhost:55555/healthz         # {"status":"ok"}
curl -o /dev/null -w '%{http_code}' http://localhost:55555/metrics   # 404
```

## 9. 变更规则

1. 新增公网入口 → 更新第 5 节，并在同一提交里补测试。
2. 新增日志字段 → 确认它不携带凭证；必要时加入脱敏名单。
3. 新增密钥 → 写进第 4 节，说明它与既有密钥的关系。
4. 放宽任何白名单或上限 → 在第 7 节记录原因和触发条件。
5. 本文与代码不一致时，**以测试为准**，然后修正本文。
