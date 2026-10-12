# AgentForge Web

这是一个面向普通前台或中台项目的 React 工程脚手架，不是管理后台模板。管理端如有需要，后续单独放到 `frontend/admin`。

## 技术基线

- React + TypeScript + Vite
- React Router：页面路由
- TanStack Query：服务端状态、缓存与重试
- Zod：环境变量和接口响应校验
- Vitest + Testing Library：组件测试
- Oxlint：静态检查

## 本地启动

先启动后端：

```powershell
cd backend/gofiber-gorm-next
go run ./cmd/api
```

再启动前端：

```powershell
cd frontend/web
Copy-Item .env.example .env
npm install
npm run dev
```

访问 `http://localhost:5173`。开发服务器会把 `/api`、`/healthz` 和 `/readyz` 代理到 `http://127.0.0.1:55555`。

如果 API 不在同一域名，设置：

```dotenv
VITE_API_BASE_URL=https://api.example.com
```

注意：所有 `VITE_*` 变量都会进入浏览器构建产物，不能放数据库密码、Token 等秘密。

## 常用命令

```bash
npm run dev        # 开发服务器
npm run lint       # 静态检查
npm run test       # 单元/组件测试
npm run typecheck  # TypeScript 检查
npm run build      # 生产构建，输出到 dist/
npm run check      # 依次执行 lint、test、build
npm run preview    # 本地预览构建结果
```

## 目录约定

```text
src/
├─ app/       # 全局 Provider、应用初始化
├─ features/  # 按业务能力组织的功能模块
├─ pages/     # 路由级页面
├─ shared/    # API、配置、布局和可复用基础代码
└─ test/      # 测试初始化
```

新业务优先建立 `features/<业务名>`；只有真正跨业务复用的代码才放进 `shared`。接口返回值应在 API 边界完成校验，不要让不可信数据直接流入页面。

## 当前示例

首页请求 Go 服务的 `GET /healthz`，用于验证：

1. Vite 开发代理正常；
2. TanStack Query 请求状态正常；
3. Zod 能校验后端响应；
4. 测试能替换网络边界并验证页面结果。
