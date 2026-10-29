# Docs

本目录保存 AgentForge 的 ADR、架构图、安全边界、接口约定和演示文档。

架构决策从 `adr/0001-repository-and-platform-baseline.md` 开始；学习路线和阶段计划继续维护在相邻 `roadmap` 仓库的 `todo/AGENT/agent-fullstack-实施方案.md`。

## 文档索引

**安全**

- [威胁模型](./security/THREAT-MODEL.md)：保护什么、防谁、每条控制对应哪个测试、哪些风险是知情接受的。
  改动任何对外入口前先读这份。

**架构决策**

- [ADR-0001：独立仓库与平台基线](./adr/0001-repository-and-platform-baseline.md)
- [ADR-0002：HTTP 与 WebSocket 拆分，以及一次性票据鉴权](./adr/0002-http-ws-split-and-ticket-auth.md)

**笔记**

- [工程化学习笔记](./工程化学习笔记.md)：记录 `.gitattributes`、密钥扫描、Go module 等实践中遇到的知识点和排查经验。

**开发日志**

- [logs/](./logs/README.md)：按 Step 记录做过什么、为什么这么选、踩了什么坑。已脱敏，随仓库发布。
  - [Step 2：后端安全基线、实时服务与模板化](./logs/step-02-security-baseline.md)
  - [Step 1：跨服务商 WireGuard 组网与服务器基线](./logs/step-01-network.md)

## 本地文件

`docs/plans/` 被 `.gitignore` 排除，只在本地维护（真实拓扑、IP、安全组、密钥位置、备份与排障方案），不随仓库上传。
两者分工：`plans/` 是怎么做，`logs/` 是做过什么。
