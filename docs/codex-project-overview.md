# PicoClaw Project Overview For Codex

本文档记录对当前仓库的快速熟悉结果，目标是让后续进入该仓库的 Codex 或开发者能快速建立项目级认知。

## 项目定位

PicoClaw 是一个纯 Go 实现的个人 AI Agent 项目，不只是聊天 CLI。

它的目标更接近“低资源占用、可长期运行、可接多模型和多渠道的 AI 助手平台”：

- 用 Go 实现主运行时，强调低内存、快启动、多平台。
- 通过 `gateway` 进程常驻运行。
- 支持多种 LLM Provider、消息渠道、工具系统、MCP、技能、Web 管理界面。
- 除命令行外，还提供 Web Launcher 和 TUI Launcher。

## 顶层结构

- `cmd/`
  - 可执行入口。
  - `cmd/picoclaw` 是主 CLI。
  - `cmd/picoclaw-launcher-tui` 是 TUI 启动器。
- `pkg/`
  - 核心业务实现，包含 agent、gateway、channels、providers、tools、config、memory 等。
- `web/`
  - 独立的 Web Launcher，小型 monorepo。
  - `backend/` 是 Go 后端。
  - `frontend/` 是 React 19 + Vite 前端。
- `config/`
  - 配置示例文件。
- `workspace/`
  - onboarding 时嵌入或复制到用户目录的默认工作区模板来源。
- `docs/`
  - 项目文档。

## 主要入口

### 主 CLI

文件：`cmd/picoclaw/main.go`

主命令包括：

- `onboard`
- `agent`
- `auth`
- `gateway`
- `status`
- `cron`
- `migrate`
- `skills`
- `model`
- `update`
- `version`

其中最关键的是：

- `picoclaw onboard`
  - 初始化配置和默认工作区。
- `picoclaw gateway`
  - 启动常驻运行时。

### Gateway 命令

文件：`cmd/picoclaw/internal/gateway/command.go`

关键参数：

- `--debug` / `-d`
- `--no-truncate` / `-T`
- `--allow-empty` / `-E`

其中 `-E` 表示在没有默认模型时也允许网关启动，进入受限模式。Web Launcher 会用到这一模式。

### Web Launcher

文件：`web/backend/main.go`

这是一个独立的 launcher 进程，不只是静态前端服务器。它负责：

- 提供浏览器聊天和管理界面。
- 管理 launcher 登录令牌。
- 启动或接管 `picoclaw gateway -E` 子进程。
- 通过 Pico channel WebSocket 代理聊天流量。
- 暴露配置、模型、渠道、技能、工具、日志等管理 API。

### TUI Launcher

文件：`cmd/picoclaw-launcher-tui/main.go`

它会在必要时自动执行 `picoclaw onboard`，然后加载 launcher 配置并运行 TUI。

## 运行时主链路

核心文件：`pkg/gateway/gateway.go`

`gateway.Run(...)` 大致流程如下：

1. 初始化 panic 日志和文件日志。
2. 加载配置。
3. 做基础配置校验。
4. 写 PID 文件，确保单实例运行。
5. 创建 Provider。
6. 创建 `MessageBus`。
7. 创建 `AgentLoop`。
8. 启动一组服务：
   - Cron
   - Heartbeat
   - MediaStore
   - ChannelManager
   - HealthServer
   - DeviceService
   - 可选 ASR / Voice Agent
9. 启动 `AgentLoop.Run(ctx)`。
10. 监听：
   - 进程退出信号
   - 配置热重载
   - 手动 reload 请求

这是项目最值得先读的文件之一，因为它把“配置、模型、Agent、渠道、运维服务”全部串起来了。

## Agent 核心

核心文件：

- `pkg/agent/loop.go`
- `pkg/agent/registry.go`

`AgentLoop` 是整个项目的核心调度器，负责：

- 维护配置、消息总线、状态、上下文管理器。
- 注册并执行工具。
- 调用 Provider 做推理。
- 处理消息历史、总结、媒体、技能、hook、subturn。
- 与 channel manager 配合发送消息和流式输出。
- 处理 reload。

`AgentRegistry` 负责多 Agent 注册：

- 如果没有显式 `agents.list`，会隐式创建一个 `main` agent。
- 如果配置了多个 agent，则按配置注册，并支持 route resolve。
- 支持子 agent allowlist 检查。

从代码结构上看，这个项目已经不是单 Agent demo，而是面向多 Agent / 子任务分派的系统。

## 配置体系

核心文件：

- `pkg/config/config.go`
- `config/config.example.json`

配置是模型中心设计，不再只是“一个 provider + 一个模型”：

- `agents.defaults`
  - 默认工作区、默认模型、token 上限、上下文窗口、工具迭代次数、总结阈值、routing、subturn 等。
- `model_list`
  - 模型清单。每项通过 `protocol/model` 的方式声明协议和实际模型。
- `channels`
  - 各渠道配置。
- `tools`
  - 工具开关和工具细项配置。
- `gateway`
  - 网关行为。
- `heartbeat`
  - 心跳任务。
- `devices`
  - 设备事件。
- `voice`
  - 语音相关能力。

值得注意的点：

- 默认模型通过 `agents.defaults.model_name` 指向 `model_list` 中的 `model_name`。
- 支持 fallback、light model routing、敏感信息过滤、subturn。
- 配置支持热重载，但 gateway 监听地址变化仍然需要完整重启。

## Provider 层

核心文件：

- `pkg/providers/factory_provider.go`
- `pkg/providers/types.go`

Provider 抽象接口：

- `LLMProvider`
- `StatefulProvider`
- `StreamingProvider`

模型字符串会按 `protocol/model` 解析，例如：

- `openai/gpt-5.4`
- `anthropic/claude-sonnet-4.6`
- `azure/my-gpt5-deployment`
- `bedrock/...`

当前支持的 Provider 家族非常多，包括：

- OpenAI 兼容族
- Anthropic
- Anthropic Messages
- Azure OpenAI
- AWS Bedrock
- LM Studio
- Ollama
- Groq
- DeepSeek
- Gemini / Antigravity
- Minimax
- ModelScope
- Codex CLI / Claude CLI / GitHub Copilot 等集成

Provider 层还定义了：

- 流式输出能力
- thinking 能力
- native search 能力
- failover / fallback 错误分类

## Channel 层

核心文件：`pkg/channels/manager.go`

`ChannelManager` 负责：

- 初始化启用的渠道。
- 管理各渠道的发送 worker。
- 处理文本和媒体的异步发送。
- 对不同渠道做限流。
- 管理 placeholder、typing、reaction undo。
- 对接 HTTP server 和健康检查接口。
- 支持 reload。

当前支持的渠道从目录结构和配置示例看包括：

- telegram
- discord
- slack
- qq
- matrix
- irc
- line
- dingtalk
- feishu
- wecom
- weixin
- whatsapp
- whatsapp_native
- pico
- pico_client
- maixcam
- onebot

项目不是把渠道写死在网关里，而是通过 channel registry 和 manager 统一管理。

## Tool 系统

核心文件：

- `pkg/tools/registry.go`
- `pkg/agent/loop.go`

ToolRegistry 负责：

- 注册 core tool 和 hidden tool。
- 根据 TTL 控制 hidden tool 暴露时机。
- 执行参数校验。
- 注入 channel/chat 上下文。
- 捕获工具 panic，避免拖垮 Agent。

从 `pkg/tools/` 当前文件来看，内建工具覆盖面很广：

- shell
- filesystem / edit
- web search / web fetch
- message
- reaction
- send_file
- cron
- session
- mcp_tool
- spawn / spawn_status
- skills_search / skills_install
- send_tts
- i2c / spi

其中比较关键的是：

- `spawn`
  - 子任务分派工具，支持后台 subagent。
- `web`
  - 聚合多个搜索后端。
- `shell`
  - 与工作区执行能力相关，风险和能力都比较高。

## Memory 与会话历史

核心文件：

- `pkg/memory/store.go`
- `pkg/memory/jsonl.go`

默认持久化实现是 JSONL：

- 每个 session 一份 `.jsonl`
- 每个 session 一份 `.meta.json`

特点：

- 追加写，简单稳妥。
- 通过 `skip` 做逻辑截断，而不是频繁物理删除。
- 更偏向低复杂度和崩溃恢复，而不是复杂数据库方案。

这和项目“轻量、低依赖、可在小设备运行”的目标一致。

## 路由与多模型选择

核心文件：`pkg/routing/router.go`

这里实现的是轻量规则路由：

- 对消息复杂度打分。
- 低于阈值时走 `light_model`。
- 否则走主模型。

也就是说，这个项目不仅支持 fallback，还支持简单任务自动切到轻模型节省成本。

## MCP 集成

核心文件：`pkg/mcp/manager.go`

MCP 管理器负责：

- 从配置中加载多个 MCP server。
- 支持 stdio / http 等 transport。
- 支持 env file。
- 并发连接多个 server。
- 在部分连接失败时做降级而不是整体失败。

这部分属于项目的重要扩展机制。

## Web 子项目

关键文件：

- `web/README.md`
- `web/backend/main.go`
- `web/frontend/package.json`
- `web/frontend/src/routes/`

当前前端栈：

- React 19
- Vite
- TypeScript
- TanStack Router
- React Query
- Tailwind CSS 4
- Jotai

从路由文件看，Web 管理界面至少包含：

- 首页聊天
- models
- credentials
- logs
- config
- agent hub
- agent skills
- agent tools
- channel detail 页面
- launcher login

这意味着它已经具备比较完整的桌面/浏览器管理体验，而不是单页 demo。

## Onboarding

核心文件：`cmd/picoclaw/internal/onboard/command.go`

onboard 会把默认工作区模板嵌入到可执行文件中，模板包含：

- `AGENTS.md`
- `IDENTITY.md`
- `SOUL.md`
- `USER.md`
- `memory/MEMORY.md`
- 一批默认 skills

这说明项目从一开始就把“工作区人格/记忆/技能”的概念作为产品模型的一部分。

## 当前对项目成熟度的判断

从仓库规模粗看：

- Go 文件约 561 个。
- `_test.go` 文件约 215 个。

这说明：

- 功能面已经比较广。
- 测试覆盖不是空白。
- 仓库已经明显超过原型或单点功能项目的规模。

## 当前工作区状态

熟悉仓库时发现当前工作区不是干净状态，存在未提交内容：

- `pkg/channels/manager.go`
- `cmd/picoclaw/internal/send/`
- `upload.py`

后续如果要在这些区域继续开发，需要先确认这些改动的来源和意图，避免误覆盖。

## 推荐阅读顺序

如果后续要继续快速进入状态，建议按下面顺序读代码：

1. `cmd/picoclaw/main.go`
2. `cmd/picoclaw/internal/gateway/command.go`
3. `pkg/gateway/gateway.go`
4. `pkg/agent/loop.go`
5. `pkg/agent/registry.go`
6. `pkg/config/config.go`
7. `pkg/providers/factory_provider.go`
8. `pkg/channels/manager.go`
9. `pkg/tools/registry.go`
10. `web/README.md`
11. `web/backend/main.go`

## 一句话总结

PicoClaw 本质上是一个以 Go 为核心运行时的轻量 AI Agent 平台：`gateway` 负责常驻运行和服务编排，`agent` 负责推理与工具执行，`providers` 负责模型接入，`channels` 负责外部交互，`web` 负责管理和聊天 UI。
