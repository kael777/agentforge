# Step 2：后端安全基线、实时服务与模板化

> 日期：2026-10-28
> 环境：Windows 开发机，Docker Desktop，本地 MySQL
> 结果：完成，CI 绿，三个模板可一键生成并通过容器验证

## 起点

仓库当时是一个通用脚手架：`healthz/readyz`、JWT 注册登录、Todo 示例。
工作区里还有两个未提交的包（LLM provider + 工具执行层），没有接进任何路由，属于带测试的死代码。

方向上的问题是：精力花在自研 Agent 框架上，而这个仓库的定位是**可复用的安全脚手架**。

## 做了什么

| 内容 | 结果 |
|---|---|
| 目录改名与命名统一 | `backend/gofiber-gorm-next` → `backend/api`，服务名统一 |
| API 安全基线 | 端口拆分、响应头脱敏、日志脱敏、限流、token 作用域 |
| WebSocket 服务 | 独立 module，票据鉴权、连接限制、优雅关闭 |
| 三个模板 | `api` / `ws` / `web` 一条命令生成独立项目 |
| 安全文档 | 威胁模型 + ADR-0002 |
| 依赖与工具链 | Go 1.27.3，govulncheck 0 可达漏洞 |

## 关键决策

**暴露面拆成两个监听口。** 公网只留 `/healthz` 和业务路由；`/readyz`、`/version`、
`/metrics` 移到管理端口。`/healthz` 从返回 7 个字段收敛成 `{"status":"ok"}`——
版本号和环境信息对攻击者的价值，远大于对运维的价值。

**HTTP 与 WebSocket 拆成两个进程。** 超时与关停语义不同，混在一起会互相拖累。
理由与被否决的七个备选方案记在 ADR-0002。

**票据而不是直接复用访问 token。** 访问 token 24 小时有效，WebSocket 握手需要一次性凭据。
票据 30 秒、单次使用、独立受众、独立密钥。

**模板不复制代码。** 三个服务本身就是模板，生成器负责改名与裁剪。
维持两份副本必然漂移，而这个仓库的安全控制全落在真实服务上。

**先单实例。** Loki、Prometheus、Kuma 都是单点，不在第一版做集群。

## 踩的坑

| 坑 | 现象 | 处理 |
|---|---|---|
| `/healthz` 泄露版本 | 返回版本、commit、环境、运行时长 | 收敛为最小响应，元数据移到管理端口 |
| 前端按旧结构解析 | 后端改了，前端仍按 `service/version` 解析，运行时误报"后端未连接" | 同步 schema，测试 fixture 也改 |
| `contrib/jwt` 的 Claims 跨请求共享 | 并发隐患 | 自己写中间件，显式校验算法/iss/aud/exp |
| `app.Test` 不经过真实服务端 | 测不出 fasthttp 是否添加 `Server` 头 | 加真实监听端口的冒烟测试 |
| 关闭握手死锁 | 阻塞的 `Read` 持有读锁，`conn.Close` 等不到回帧，要等满读超时 | 关闭帧异步投递，排空交给 hub 限时 |
| 握手/关闭竞态 | `CloseAll` 在 `conn` 还没赋值时关闭，socket 泄漏、连接计数只增不减 | `Attach` 与 `Close` 互斥，未 attach 时立即关 socket |
| 库的关闭握手最长 5 秒 | 关停预算不足会切断在线连接 | `SHUTDOWN_TIMEOUT=20s`，systemd `TimeoutStopSec=25s` |
| govulncheck 报 12 条 stdlib 漏洞 | 工具链落后 6 个补丁版本 | `go.mod` 声明最低工具链 `go 1.27.3` |
| govulncheck 二进制版本不匹配 | 升 Go 后报 `package requires newer Go version` | 重新 `go install`，CI 每次装最新 |
| Gitleaks 报 6 条"密钥" | 全是测试常量和 `.env.example` 占位值 | `.gitleaks.toml` 用精确正则放行，不整类文件放行 |
| 生成项目残留源仓库信息 | Dockerfile 的 OCI `source` 标签仍指向本仓库 | CI 加断言，抓到后修生成器 |

最后一条值得单记：那条断言是随手加的，第一次跑 CI 就抓到了**自己写的生成器**的缺陷。

## 验收

- 两个 module 的 `test` / `vet` / `gofmt` 全绿
- `govulncheck` 两个 module 均 0 可达漏洞
- 三个模板生成后 `go test` / `npm run build` 通过，镜像可构建并运行
- 响应头无 `Server` / `X-Powered-By`；公网 `/readyz`、`/metrics`、`/version` 均 404
- 登录限流实测：连发 12 次，第 11 次起 429
- CI 绿（含 `templates` job 每次回归生成与构建）

## 遗留

- WebSocket 票据的单次使用依赖进程内缓存，多实例部署需改共享存储
- 访问 token 无吊销机制，登出后仍有效至过期
- 镜像层漏洞扫描（trivy 之类）未接入，只做了模块级依赖扫描
