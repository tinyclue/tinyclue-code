<div align="center">

# Tinyclue

**一个用 Go 编写的终端原生 AI 编程智能体。**

[![Go version](https://img.shields.io/badge/Go-1.26-blue)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/tinyclue/tinyclue-code)](https://github.com/tinyclue/tinyclue-code/releases)

[English](README.md) · [简体中文](README.zh-CN.md)

</div>

---

## 这是什么？

**Tinyclue** 是一个开源、终端原生的 AI 编程智能体。它完全运行在终端里——你用自然语言描述
任务，AI 智能体会规划步骤、阅读你的代码、执行 shell 命令、编辑文件，并不断迭代直到完成。

它使用 **Go** 编写（编译为单个静态二进制），并且**与模型无关**：内置一份自维护的模型目录，
覆盖 **35 个厂商 / 1081 个模型**（DeepSeek、OpenAI、Anthropic、Google、OpenRouter、Groq、
Mistral 等），你可以按预算选择任意模型——包括开箱即用的**免费**默认模型。

## 核心特性

- **🖥️ 终端原生 TUI** — Markdown 渲染、支持 Emacs 风格快捷键的多行编辑器、`/` 命令
  自动补全、权限确认弹窗，以及实时显示 token / 花费 / 上下文占用率的状态栏。
- **🌐 与模型无关** — 将 35 个厂商 / 1081 个模型聚合成本地模型目录
  （`~/.tinyclue/caches/model-cache.json`），缓存缺失时自动联网重建。
- **📡 双协议适配器** — 支持 Anthropic **Messages** 与 OpenAI **Completions / Responses /
  Codex** 接口。
- **🔌 MCP 客户端** — 基于 Model Context Protocol，支持 **stdio** 与 **HTTP/SSE** 传输，
  对 HTTP 服务提供交互式 OAuth 授权、token 持久化、自动重连，以及从 UI 对每个 server
  进行连接 / 授权 / 启用 / 禁用。
- **🛠️ 丰富的工具集** — `Bash`、`Read`、`Write`、`Edit`、`Glob`、`Grep`、`WebSearch`、
  `WebFetch`、`AskUserQuestion`、`RunAgent`、`SendMessage`、`TaskCreate/Get/List/Update`、
  `EnterPlanMode` / `ExitPlanMode`、`ToolSearch`、`Skill`，以及 MCP 桥接工具。
- **🧠 子智能体** — 内置 `General`、`Explore`、`Plan`、`GeneralPurpose`、`Verification`
  等智能体类型，可通过 `RunAgent` 生成。
- **📝 计划模式** — 独立的规划阶段，带有持久化的计划文件与进入 / 退出计划模式的工具。
- **✅ 任务管理** — 基于文件、跨进程安全的任务列表，按会话隔离。
- **💾 会话持久化** — 每次对话都会记录为 JSONL；通过 `tinyclue -c [sessionId]` 恢复。
- **🧠 上下文压缩** — 自动压缩，让长会话始终保持在模型上下文窗口之内。
- **📄 项目指令** — `TINYCLUE.md` 从 managed / user / project / local / auto-memory / team
  等层级发现，支持 `@include`、YAML frontmatter 与 rules 目录。
- **🧠 自动记忆** — 跨会话共享的持久化 `MEMORY.md` 记忆系统。
- **🔐 细粒度权限** — 按项目维护 allow-list 规则
  （`.tinyclue/config/settings.local.json`），支持通配符；可一次性放行或记住。
- **🔑 多种登录方式** — 支持 API Key，以及基于本地回环回调 + PKCE 的 OAuth 订阅登录
  （如 Claude Pro/Max）。
- **⚡ 免费默认** — 安装脚本会种子一个零成本模型（经 opencode zen 网关的
  `deepseek-v4-flash-free`），装完即用。

## 快速开始

### 环境要求

- **Go 1.26+**（仅在使用源码构建或通过 `install.sh` 安装时需要）
- 主要面向 macOS / Linux（安装脚本基于 Unix）。

### 安装

在仓库克隆目录内运行安装脚本：

```bash
./install.sh          # 交互式——覆盖已有配置文件前会逐一询问
./install.sh -y       # 非交互——直接覆盖配置文件，不再询问
```

`install.sh` 会：

1. 编译二进制并安装到 `$PREFIX/bin/tinyclue`（默认 `/usr/local/bin`，
   可通过 `PREFIX=/自定义路径` 覆盖）。
2. 初始化配置目录（`~/.tinyclue/config/`），种子写入 `auth.json`
   （共享匿名 `opencode` key `"public"`）、`settings.json`
   （默认厂商 `opencode`、默认模型 `deepseek-v4-flash-free`）与
   `slog.json`（文件日志配置）。已有文件在覆盖前会备份为 `.bak`。
3. 将模型目录种子到 `~/.tinyclue/caches/model-cache.json`（仅当不存在时）。

也可以手动构建与安装：

```bash
make            # 或：go build -o bin/tinyclue ./cmd
make install    # 执行 ./install.sh
```

卸载：

```bash
./uninstall.sh            # 交互式——未经确认保留数据
./uninstall.sh -y         # 全部删除（配置先备份）
./uninstall.sh --keep-data # 只删二进制与缓存，保留配置 / 会话 / 任务
```

### 使用

```bash
tinyclue              # 在当前目录启动
tinyclue -c           # 恢复当前项目最近一次会话
tinyclue -c <id>      # 按 session id 恢复指定会话
```

首次启动即可使用——默认免费模型无需任何 API Key。如需切换厂商 / 模型或添加凭据，
使用会话内命令：

| 命令      | 说明                                                    |
| --------- | ------------------------------------------------------- |
| `/login`  | 登录：输入 API Key，或启动 OAuth 订阅登录                |
| `/model`  | 选择厂商、模型与推理级别                                  |
| `/mcp`    | 查看 MCP server：连接、授权、启用 / 禁用                 |
| `/exit`   | 退出                                                     |

编辑器快捷键沿用 Emacs 习惯：`Ctrl+P` / `Ctrl+N` 上下移动历史，`Ctrl+B` / `Ctrl+F`
左右移动光标，`Ctrl+W` 删除光标前的单词，`Alt+B` / `Alt+F` 按单词跳跃等。
行首输入 `/` 可触发命令自动补全。在提示符前加 `!` 前缀可直接执行 shell 命令。

## 配置

所有用户配置都位于 `~/.tinyclue/` 下（可用环境变量 `TINYCLUE_CONFIG_DIR` 覆盖根目录）。

### `config/settings.json`

```json
{
  "defaultProvider": "opencode",
  "defaultModel": "deepseek-v4-flash-free",
  "reasoning_effort": "high",
  "language": "",
  "autoMemory": false
}
```

- `defaultProvider` / `defaultModel` — 当前生效的厂商与模型。
- `reasoning_effort` — 推理级别：`low` / `medium` / `high` / `max`（默认 `high`）。
- `language` — 偏好回复语言。
- `autoMemory` — 是否启用跨会话自动记忆系统。

### `config/auth.json`

```json
{
  "opencode": { "type": "api-key", "key": "public" },
  "anthropic": { "type": "oauth" }
}
```

将每个厂商映射为 `api-key` 或 `oauth` 凭据。

### MCP server

MCP server 从用户级 `~/.tinyclue/mcp.json` 加载，并与从工作目录向上遍历找到的项目级
`.mcp.json` 合并（更深层目录的配置优先生效）。每个条目支持 `stdio` 或 `http`：

```json
{
  "mcpServers": {
    "my-tools": { "command": "my-mcp-server", "args": ["--flag"] },
    "remote":   { "type": "http", "url": "https://example.com/mcp" }
  }
}
```

### 权限

按项目的 allow 规则存放在 `<项目根>/.tinyclue/config/settings.local.json`：

```json
{
  "permissions": {
    "allow": ["Bash(git status *)", "Read", "Edit"]
  }
}
```

规则使用 `Tool(pattern)` 格式并支持 `*` 通配符；只有工具名（不带括号）表示任意参数都放行。
也可以在 UI 的权限弹窗里直接添加规则（"Allow, and don't ask again…"）。

### 环境变量

| 变量                                   | 说明                                                   |
| -------------------------------------- | ------------------------------------------------------ |
| `TINYCLUE_CONFIG_DIR`                  | 覆盖配置根目录（默认 `~/.tinyclue`）                    |
| `TINYCLUE_CACHE_DIR`                   | 覆盖缓存目录（默认 `<config>/caches`）                  |
| `TINYCLUE_OAUTH_CLIENT_ID_<PROVIDER>`  | 为订阅厂商提供 OAuth client id                          |

## 模型支持

Tinyclue 会从 [models.dev](https://models.dev)、OpenRouter、NVIDIA 以及 AI 网关数据源
聚合模型元数据，形成统一的本地目录。本仓库内置的种子目录覆盖 **35 个厂商 / 1081 个模型**，
包括：

DeepSeek · OpenAI · OpenAI Codex · Anthropic · Google · Google Vertex · Amazon Bedrock ·
Azure OpenAI · OpenRouter · OpenCode · OpenCode Go · Groq · Cerebras · Mistral · xAI ·
Hugging Face · Fireworks · Together · NVIDIA · Cloudflare Workers AI / AI Gateway ·
GitHub Copilot · MiniMax · Kimi · Moonshot AI · ZAI · Xiaomi 等。

每条模型记录都带有其 API 协议、base URL、上下文窗口、最大 token、推理能力与按 token
计费的价格——正是这些数据支撑了状态栏中的实时费用估算。

## 架构

```
tinyclue
├── cmd/                 # 入口（main.go）
├── coding_agent/        # 智能体核心
│   ├── core/            #   agent 主循环、工具、提示词、会话、计划模式、
│   │                    #   任务、TINYCLUE.md 用户上下文、自动记忆
│   └── interactive.go   #   TUI 会话引导
├── api_provider/        # 协议适配器（Anthropic Messages / OpenAI）
├── models_cache/        # 模型目录聚合管道（35+ 厂商）
├── mcp/                 # MCP 客户端管理器（stdio + http）、连接生命周期与自动重连
├── tui/                 # 终端 UI：组件、渲染器、按键处理
├── config/              # settings.json / auth.json / mcp.json / 权限
├── oauth/               # OAuth 回调服务器 + token 存储（PKCE）
├── subscription/        # 订阅（OAuth）登录注册表
└── log/                 # 带轮转的分级文件日志
```

数据流：你的输入 → TUI 编辑器 → agent 主循环 → 模型 API（经由适配器）→ 工具调用
（`Bash`、`Read`、`Edit`、MCP 工具等）→ 结果流回对话并在终端渲染。

## 开发

```bash
# 构建
make            # 将 ./cmd 编译为 bin/tinyclue
make clean      # 清理构建产物

# 测试
go test ./...   # 单元 + 集成测试（coding_agent/core、mcp、tui、config 等）
```

仓库内 `tests/` 还包含一些独立测试程序（`tests/mcp_test_server`、
`tests/ripgreptest`、`tests/globcoverage` 等）。

## 参与贡献

欢迎贡献代码！请阅读 [CONTRIBUTING.zh-CN.md](CONTRIBUTING.zh-CN.md) 了解如何贡献——
包括报告 bug、请求新特性以及提交 pull request。

## 许可证

[MIT](LICENSE) © Tinyclue
