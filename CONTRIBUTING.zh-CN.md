# 为 Tinyclue 贡献代码

[English](CONTRIBUTING.md) · [简体中文](CONTRIBUTING.zh-CN.md)

感谢你对 **Tinyclue** 的兴趣！本指南说明了如何报告问题、请求新特性，以及如何提交符合
项目约定的代码。

- [行为准则](#行为准则)
- [开始之前](#开始之前)
- [仓库结构](#仓库结构)
- [开发工作流](#开发工作流)
- [做出修改](#做出修改)
- [Pull Request 检查清单](#pull-request-检查清单)
- [报告 Bug](#报告-bug)
- [请求新特性](#请求新特性)
- [测试规范](#测试规范)
- [文档](#文档)
- [许可证](#许可证)

## 行为准则

保持尊重与建设性。Tinyclue 是一个社区项目——分歧应当围绕代码本身，而非针对个人。
欢迎任何贡献，但不欢迎骚扰或攻击性行为。

## 开始之前

### 环境要求

- **Go 1.26+** —— 从源码构建所必需。
- **macOS / Linux** —— 安装脚本基于 Unix；目前不支持 Windows 构建。

### 环境搭建

```bash
git clone <你的 fork 地址> tinyclue-code
cd tinyclue-code
make            # 构建二进制到 bin/tinyclue
go test ./...   # 运行完整测试套件
```

## 仓库结构

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

新增或改动功能时，把相关代码保持在其所属的包内；如果目录结构发生变化，请同步更新
`README.md` / `README.zh-CN.md` 中的架构图。

## 开发工作流

### 构建

```bash
make            # go build -o bin/tinyclue cmd/main.go
make clean      # 清理构建产物（bin/）
```

### 运行测试

```bash
go test ./...   # 所有包的单测 + 集成测试
```

`tests/` 目录还包含一些独立测试程序（如 `tests/mcp_test_server`、
`tests/ripgreptest`、`tests/globcoverage`）。它们是独立的 `main` 包，不参与主 module
的构建——请单独运行它们各自的测试或二进制。

### 代码风格

- 所有改动文件都要用 `gofmt`（或 `go fmt ./...`）格式化——CI 与评审都要求代码已格式化。
- 提交前运行 `go vet ./...`，保持整个树 vet 干净。
- 遵循所在包的命名、注释与错误处理约定。本代码库的注释同时使用英文与简体中文——与
  你正在改动的代码风格保持一致。

### 依赖管理

项目使用标准 Go module 模式（以 `go.mod` / `go.sum` 为准），Go 版本在 `go.mod` 中
固定。`vendor/` 目录**不提交**——它仅在本机构建时生成以复现构建，已被 `.gitignore`
排除。新增或更新依赖时：

```bash
go get <module>@<version>
go mod tidy
```

请确保 `go.mod` / `go.sum` 保持一致并提交。若本地重新生成了 `vendor/`，保持其不被
纳入仓库（`.gitignore` 已排除）。

## 做出修改

1. **先查找或新建 issue。** 对于任何非平凡改动，先说明你的意图再写代码——避免重复或
   不必要的劳动。
2. **基于默认分支创建功能分支。**
3. **实现你的改动。** 保持聚焦，避免顺手做无关的重构。
4. **补充或更新测试。** 行为变化需要有测试覆盖（见[测试规范](#测试规范)）。
5. **本地验证：**
   ```bash
   gofmt -l <改动的文件>   # 应为空
   go vet ./...
   go build ./...
   go test ./...
   ```
6. **提交**，提交信息要简洁并解释*为什么*（见[提交规范](#提交规范)）。
7. **发起 pull request**，并在其中关联要关闭的 issue。

### 提交规范

- 主题行用祈使句且简短：如 "Fix token count under-count in long sessions"，而不是
  "fixed things"。
- 正文解释*为什么*需要这个改动，而不只是*做了什么*。
- 尽量关联 issue 编号：如 "Closes #123"。

## Pull Request 检查清单

- [ ] `go build ./...` 通过
- [ ] `go test ./...` 通过
- [ ] `go vet ./...` 干净
- [ ] 改动文件已用 `gofmt` 格式化
- [ ] 行为变化包含新增 / 更新的测试
- [ ] 用户可见行为变化时，README / 文档已同步更新

## 报告 Bug

提交 issue 时请包含：

- 你做了什么（复现步骤）。
- 期望行为，以及实际发生了什么。
- 环境信息：操作系统版本、Go 版本、Tinyclue 版本（或 commit）。
- 如果失败与某次运行相关，附上 `~/.tinyclue/log/` 或 `~/.tinyclue/sessions/` 下
  会话文件中的相关信息。
- 如果问题与崩溃或渲染相关，一份终端录制（或 `script` 输出）会非常有帮助。

## 请求新特性

描述你想解决的问题以及具体的用例。能说明特性*为什么*重要、以及大致*如何*融入现有
架构的提案更容易被评估。最好附上一段简短的 UX 草图（用户输入什么、看到什么）。

## 测试规范

- **行为变化 → 测试变化。** 如果修复或特性改变了可观察行为，请在同一个 PR 中新增或
  更新测试。
- 单元测试紧邻被测代码（同包的 `*_test.go`）。
- 组件级 UI 测试（如 TUI 面板）用合成输入渲染组件，再对输出行做断言——参考
  `tui/component/mcppanel/mcppanel_test.go` 的模式。
- 集成类夹具放在 `tests/` 下作为独立程序；保持它们可独立运行（`go test` 或直接
  调用），从而不影响主 module 的 `go build ./...`。
- 如果改动涉及 MCP 客户端，可参考 `tests/mcp_test_server`——它提供了可在测试中驱动的
  stdio 服务。

## 文档

- `README.md`（英文）与 `README.zh-CN.md`（简体中文）必须保持同步——修改任一文件时
  请同步更新另一个。
- 改动命令行参数、斜杠命令、配置文件结构、环境变量或安装行为时，请更新文档。

## 许可证

贡献即表示你同意你的贡献基于 [MIT 许可证](LICENSE) 授权。
