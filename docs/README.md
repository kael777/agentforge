# Docs

本目录保存 AgentForge 的 ADR、架构图、安全边界、接口约定和演示文档。

架构决策从 `adr/0001-repository-and-platform-baseline.md` 开始；学习路线和阶段计划继续维护在相邻 `roadmap` 仓库的 `todo/AGENT/agent-fullstack-实施方案.md`。

## 文档索引

**安全**

- [威胁模型](./security/THREAT-MODEL.md)：保护什么、防谁、每条控制对应哪个测试、哪些风险是知情接受的。
  改动任何对外入口前先读这份。
- [AI 自动化部署与运维指南](./AI自动化部署与运维指南.md)：把服务器交给 AI/自动化执行时的授权分级、
  变更协议、撤销与并发控制、SSH 密钥配置与防泄露规则。与项目无关，任何团队可直接用。

**架构决策**

- [ADR-0001：独立仓库与平台基线](./adr/0001-repository-and-platform-baseline.md)
- [ADR-0002：HTTP 与 WebSocket 拆分，以及一次性票据鉴权](./adr/0002-http-ws-split-and-ticket-auth.md)

**笔记**

- [工程化学习笔记](./工程化学习笔记.md)：记录 `.gitattributes`、密钥扫描、Go module 等实践中遇到的知识点和排查经验。

**开发日志**

- [logs/](./logs/README.md)：按 Step 记录做过什么、为什么这么选、踩了什么坑。已脱敏，随仓库发布。
  - [Step 2：后端安全基线、实时服务与模板化](./logs/step-02-security-baseline.md)
  - [Step 1：跨服务商 WireGuard 组网与服务器基线](./logs/step-01-network.md)

## 三份文档的分工

| 文档 | 回答的问题 | 谁看 |
|---|---|---|
| 威胁模型 | 防什么、控制在哪、哪个测试证明 | 改代码的人 |
| AI 运维指南 | 谁授权、按什么流程做、出事怎么停 | 把服务器交出去的人 |
| logs/ | 我们实际怎么做的、踩了什么 | 接手的人 |

冲突时的优先级：**业务规则看项目文档，安全与流程看运维指南，具体控制看威胁模型。**

## 本地文件

`docs/plans/` 被 `.gitignore` 排除，只在本地维护（真实拓扑、IP、安全组、密钥位置、备份与排障方案），不随仓库上传。
`plans/` 是怎么做，`logs/` 是做过什么。
