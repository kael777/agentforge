# ADR-0001：独立仓库与平台基线

- 状态：接受
- 日期：2026-10-12

## 背景

AgentForge 需要独立维护前端、后端、部署配置、CI/CD、版本标签和发布制品。继续嵌在学习笔记仓库中会让 workflow、Release 和提交历史与其他内容耦合。

## 决策

1. AgentForge 使用独立 Git 仓库，采用 `backend`、`frontend`、`devops`、`docs` 的职责分层。
2. 后端使用 Go 1.27、Fiber v3、GORM 和 MySQL 8.4。
3. 前端使用 React 19、TypeScript 和 Vite；普通 Web 应用与未来管理端保持独立。
4. 第一阶段部署目标是单台 Linux 主机上的 systemd + Nginx，不提前引入 Kubernetes。
5. 正式制品由 GitHub Actions 在版本 Tag 上构建，本地 `publish` 只负责校验和触发。
6. 后续异步 Agent 任务使用可持久化队列和数据库状态机；具体队列实现在 Phase 2 前另写 ADR。

## 结果

- CI、Release、Issue 和版本历史只属于 AgentForge。
- 前后端构建进入同一个不可变 Release bundle。
- 本地开发同时提供 Make、PowerShell 和 Bash 入口。
- 生产发布与部署分离，生产部署必须经过环境审批。
