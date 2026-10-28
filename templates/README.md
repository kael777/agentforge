# 三个模板

本目录不存放代码副本。三个模板就是本仓库里三个可运行的服务：

| 模板 | 源目录 | 用途 | 生成命令 |
|---|---|---|---|
| Go Fiber | `backend/api` | HTTP API：鉴权、业务读写、签发 WebSocket 票据 | `--template api` |
| Go WebSocket | `backend/ws` | 实时服务：票据鉴权、连接限制、广播、优雅关闭 | `--template ws` |
| React + Vite | `frontend/web` | 前端：路由、请求、Zod 校验、测试与构建 | `--template web` |

**为什么不做第二份副本**：模板一旦独立维护，就会和真实服务漂移；而这个仓库的
安全控制全部落在真实服务上。让模板就是服务本身，生成器负责改名与裁剪，
两者永远不会分叉。

## 生成一个独立项目

```bash
python devops/scripts/new-project.py \
  --template api \
  --name my-service \
  --target ../my-service \
  --module github.com/acme/my-service \
  --init-git
```

生成结果是**自包含**的：

- Go 模板：改好 `go.mod` 的 module path、所有 internal import、服务名，
  并带上自己的 `Dockerfile`
- Web 模板：改好 `package.json` 的 name，并带上 `Dockerfile`、`nginx.conf`、
  `.dockerignore`
- `--init-git` 会初始化 git 并提交首个版本；不传则只生成目录

创建远程仓库不在脚本范围内（需要你的 GitHub 账号与组织），生成后执行：

```bash
gh repo create my-service --private --source . --push
```

## 每个模板包含什么

### `--template api`

- Fiber + GORM + MySQL，Handler / Service / Repository 分层
- `/healthz` 只返回 `{"status":"ok"}`；`/readyz`、`/version`、`/metrics` 在管理端口
- 结构化日志 + 脱敏、响应头清理、登录限流、JWT 作用域校验
- 生产配置校验：拒绝占位密钥、通配 Origin、非回环管理端口
- 示例业务：注册登录 + 按用户隔离的 Todo
- 显式 SQL migration、`cmd/migrate`、优雅退出

### `--template ws`

- `net/http` + `coder/websocket`，不依赖 Fiber
- 握手顺序：方法 → Origin 精确白名单 → 一次性票据 → 连接上限
- 心跳、读限制、令牌桶限速、有界发送队列、慢消费者剔除
- 关闭码语义明确（1001/1008/1009/1013/4001）
- 示例业务：订阅 / 发布 / 心跳

### `--template web`

- React 19 + TypeScript + Vite，`npm ci` + lockfile
- 路由、TanStack Query、Zod 运行时校验
- 非特权 nginx 容器：uid 101、8080 端口、SPA fallback、CSP、
  hashed 资源长缓存、`index.html` 不缓存
- `/healthz` 由静态容器自己返回，不依赖后端

## 验证方式

模板的验收标准和文档一致，且全部可以本地复现：

```bash
# 1. 一键生成
python devops/scripts/new-project.py --template api --name demo-api \
  --target /tmp/demo-api --module github.com/acme/demo-api

# 2. 测试与构建
cd /tmp/demo-api && go mod tidy && go test ./... && go build ./...

# 3. 容器启动（api 需要可用的 MySQL）
docker build -t demo-api .
docker run -d -p 15555:55555 --network <mysql 所在网络> \
  -e DATABASE_DSN=... -e JWT_SECRET=... -e WS_TICKET_SECRET=... demo-api
curl http://127.0.0.1:15555/healthz   # {"status":"ok"}
```

CI 的 `templates` job 会自动执行上面 1、2 两步，防止生成器或模板在重构后失效。
