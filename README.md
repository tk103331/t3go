# T3Go

用 Go 重写的 [T3 Code](https://github.com/pingdotgg/t3code) 服务端，以及基于
[NSGo](https://github.com/oneo-me/nsgo) 的多端原生客户端。

一个 T3Go 环境（environment）管理项目目录、线程、运行记录，并把 Claude Code、
Codex CLI、OpenCode 作为本地 agent 进程跑起来。客户端通过 HTTP 取快照、发动作，
通过 SSE 或 WebSocket 接收流式事件。

## 当前状态

已实现：

- SQLite 持久化：项目、线程、消息、运行、设置、事件重放窗口
- 线程运行生命周期：流式文本、思考过程、工具调用、停止、失败、用量，重启时清理中断的 run
- Provider CLI 适配：`claude`、`codex`、`opencode`，含模型列表与版本探测
- Git：工作区状态、每个 run 的 checkpoint（`git stash create` + 隐藏 ref）、run diff、worktree、提交/推送、经本机 `gh` 创建或复用 PR、分支列表与切换
- HTTP REST API、SSE 事件流、WebSocket 事件流、Bearer token 鉴权、CORS
- Web 客户端：同一份 NSGo 代码用 `GOOS=js GOARCH=wasm` 构建，由 `t3go serve --web` 直接提供
- `t3go` CLI：`serve`、`pair`、`doctor`、`version`
- 交互式权限审批：supervised 模式下 Claude Code 通过本地 MCP 询问 T3Go 客户端，允许/拒绝工具调用
- NSGo 客户端：连接页、项目/线程侧栏（线程预览与相对时间、运行/失败/未读状态、每线程草稿与 Draft 标记、固定线程、搜索、可折叠项目并记住状态）、右侧 Surfaces 面板（可拖动宽度、多 Tab；Browser 内嵌网页；Linked pull requests；文件浏览与预览：Markdown 渲染、图片、JSON/文本、二进制外开；带行号和颜色标记的代码 Diff（文件列表、stage/discard、split view）、Git graph、Pull request；内置终端（PTY/ConPTY + xterm.js，多标签、重开回放）；Device/Side chat 预留）、聊天流（日期分隔、消息时间与复制、代码块复制、编辑/重试/分叉、run 用量页脚、思考/工具卡片与工具变更文件摘要、聊天内相对图片）、Composer 上下文（`@file` 提及与文件 chip、工作区 Skills/自定义命令、MCP prompts（含参数输入）与斜杠命令、粘贴图片/文件、图片附件缩略图、每线程附件草稿、内联 model/permission/plan 选择、plan 完成提示、上下文余量（provider 报告的窗口优先，其次模型目录））、新建/重命名/删除项目与线程、新建线程可选项目与 worktree 分支、线程归档与筛选、命令面板与快捷键、记住侧栏宽度与显隐、设置（General/Providers/Worktrees/Shortcuts/About 分区；环境名、默认 provider/model/权限、主题、provider 状态、登录状态与模型、一键安装/登录命令（在终端中执行）、worktree 列表）、运行历史与逐次 diff、顶部分支工具栏（切换/新建/pull）、Changes 对话框（提交/推送/PR/切分支）、运行中排队发送、附件（选择或拖入文件并随消息发送）、语音听写、审批对话框、断线重连提示、窄屏布局、错误提示条
- 测试：store、orchestrator（fake provider）、provider 流解析、HTTP/SSE/WS、Go 客户端、NSGo UI 集成

尚未实现（有意留到后续）：

- 非 Apple 平台的语音听写（NSGo 目前只在 macOS/iOS 识别）
- 上游 T3 Code 协议的完整兼容：已实现发现、配置、生命周期和线程列表，线程
  详情/发送/事件投影尚未完成，见 [docs/upstream-compat.md](docs/upstream-compat.md)
- Codex / OpenCode 的交互式审批（两者目前按各自的沙箱/自动策略运行）

排队消息保存在客户端内存里，退出客户端会丢失；服务端仍是一次只跑一个 run。

## 环境要求

- Go 1.26+
- 至少安装一个 agent CLI 并能在 `PATH` 中找到：
  - `claude`（Claude Code）
  - `codex`（Codex CLI）
  - `opencode`（OpenCode）
- `git` 可选，用于 checkpoint、diff、worktree 和提交/推送
- `gh` 可选，用于从 Changes 对话框创建 GitHub pull request

## 快速开始

```sh
# 构建
go build -o bin/t3go ./cmd/t3go
go build -o bin/t3go-app ./cmd/t3go-app

# 检查本机环境
./bin/t3go doctor

# 启动服务（默认 127.0.0.1:4870，数据在 ~/.t3go）
./bin/t3go serve
```

构建网页版并让服务同时提供它：

```sh
./scripts/build-web.sh        # 生成 web/app.wasm、web/wasm_exec.js
./bin/t3go serve --web web    # 浏览器打开 http://127.0.0.1:4870/
```

启动时会打印 token 文件和一条配对链接：

```
t3go://pair?url=http://127.0.0.1:4870&token=...
```

运行客户端：

```sh
./bin/t3go-app
```

在连接页填入服务地址和 token（也可以直接粘贴 `t3go://pair?...` 链接），
点击 Connect。`t3` 那一行是给 T3 Code 官方客户端用的直连配对地址。之后可以添加项目目录、创建线程、选择 provider/model/权限并开始对话。

快捷键（非 Apple 平台把 `⌘` 换成 `Ctrl`）：

- `⌘K`：搜索线程和命令
- `⌘N`：新建线程
- `⌘⇧N`：新建项目
- `⌘,`：设置
- `⌘B`：显示或隐藏侧栏

再次获取配对信息：

```sh
./bin/t3go pair
./bin/t3go pair --json
```

### 远程访问

默认只监听 loopback。要让手机或另一台机器连接：

```sh
./bin/t3go serve --listen 0.0.0.0:4870
```

此时鉴权保持开启。`--no-auth` 只允许配合 loopback 地址使用。

## CLI

| 命令 | 说明 |
| --- | --- |
| `t3go serve [--home DIR] [--listen HOST:PORT] [--no-auth] [--web DIR] [--quiet]` | 启动环境，`--web` 同时提供网页版 |
| `t3go pair [--home DIR] [--listen HOST:PORT] [--json]` | 打印配对链接 |
| `t3go doctor [--home DIR]` | 检查数据库、token、provider CLI、git |
| `t3go version` | 版本与平台信息 |

数据目录默认为 `~/.t3go`，可用 `T3GO_HOME` 或 `--home` 覆盖：

```
~/.t3go/
  token          # 32 字节随机 token
  t3go.sqlite    # 状态与事件
  worktrees/     # 线程独立 worktree
  uploads/       # 随消息发送的附件
  logs/t3go.log   # 服务日志
```

## API

所有需要鉴权的接口接受 `Authorization: Bearer <token>`，也接受
`?token=<token>`（给无法设置 header 的客户端用）。

| 方法与路径 | 说明 |
| --- | --- |
| `GET /api/v1/health` | 健康检查，无需鉴权 |
| `GET /api/v1/state` | 项目、线程、provider、模型、设置的快照 |
| `GET/POST /api/v1/projects` | 列出/注册项目 |
| `PATCH/DELETE /api/v1/projects/{id}` | 更新/删除项目 |
| `GET/POST /api/v1/threads` | 列出/创建线程 |
| `GET/PATCH/DELETE /api/v1/threads/{id}` | 线程详情/更新/删除 |
| `POST /api/v1/threads/{id}/messages` | 发送消息（`content` + `attachmentIds`），启动一次 run |
| `POST /api/v1/threads/{id}/stop` | 停止当前 run |
| `GET /api/v1/threads/{id}/diff?runId=` | 某次 run 的 checkpoint diff |
| `POST /api/v1/threads/{id}/git` | 线程 checkout 的 `commit`/`push`/`create_pr`/`commit_push`/`commit_push_pr` |
| `GET /api/v1/threads/{id}/working-diff` | 工作区相对 HEAD 的 patch 与文件统计 |
| `POST /api/v1/threads/{id}/git/stage` / `git/discard` | 暂存 / 丢弃指定文件 |
| `POST /api/v1/threads/{id}/retry` | 在同一线程重跑一轮 prompt |
| `POST /api/v1/threads/{id}/fork` | 从某条消息分叉出新线程 |
| `GET/POST /api/v1/threads/{id}/branches` | 列出本地分支 / 切换到指定分支 |
| `GET /api/v1/threads/{id}/files?path=` | 列出线程 checkout 中的目录；`recursive=1` 返回用于 @ 提及的源文件索引 |
| `GET /api/v1/threads/{id}/file?path=` | 读取文件内容（文本预览，含 MIME，二进制标记） |
| `GET /api/v1/threads/{id}/raw?path=` | 直接读取文件字节，用于图片和外部打开 |
| `GET /api/v1/threads/{id}/graph` | 所有分支/远程/标签的提交图（含父子关系与 refs） |
| `GET /api/v1/threads/{id}/commands` | 工作区与用户目录中的 Claude/Codex/OpenCode Skills、自定义命令，以及本地（含 Codex TOML 配置）与远程 HTTP（JSON/SSE、旧式 SSE）MCP servers 的 prompts 及参数 |
| `POST /api/v1/threads/{id}/terminal` | 启动或复用线程 checkout 中的登录 shell |
| `GET/POST /api/v1/threads/{id}/terminals` | 列出 / 新建线程的终端会话 |
| `DELETE /api/v1/terminals/{id}` | 结束一个终端会话 |
| `GET /api/v1/terminals/{id}/ws` | 终端输入/输出 WebSocket（`resize`、`input`） |
| `POST /api/v1/terminals/{id}/input` | 向终端写入命令（设置页的安装/登录） |
| `GET /api/v1/threads/{id}/pull-request` | 读取当前分支打开的 PR（经 `gh`） |
| `GET /api/v1/threads/{id}/pull-requests` | 列出当前分支的全部 PR |
| `GET /api/v1/threads/{id}/pull-requests/{number}` | 读取 PR 详情、checks、reviews、comments |
| `POST /api/v1/threads/{id}/pull-requests/{number}/merge` | 合并 PR（merge/squash/rebase，可删分支） |
| `POST /api/v1/threads/{id}/pull-requests/{number}/action` | ready/draft/close/reopen |
| `POST /api/v1/threads/{id}/pull-requests/{number}/review` | 提交 review（approve/request-changes/comment） |
| `POST /api/v1/threads/{id}/pull-requests/{number}/comments` | 添加 PR 评论 |
| `POST /api/v1/threads/{id}/git/pull` | fast-forward 当前分支 |
| `GET /api/v1/providers` | provider 与模型 |
| `GET/PATCH /api/v1/settings` | 环境默认设置 |
| `GET /api/v1/fs?path=` | 列出子目录，供目录选择器使用 |
| `POST /api/v1/threads/{id}/approvals` | provider 的 MCP helper 登记一次工具调用并等待决定 |
| `GET /api/v1/approvals?threadId=` | 等待中的审批 |
| `POST /api/v1/approvals/{id}` | 允许或拒绝一次工具调用 |
| `POST /api/v1/uploads?name=` | 上传一个附件（请求体为文件内容，上限 25 MB） |
| `GET/DELETE /api/v1/uploads/{id}` | 下载/删除附件 |
| `GET /api/v1/events?after=<seq>` | SSE 事件流 |
| `GET /ws` | WebSocket 事件流 |
| `GET /.well-known/t3/environment` | T3 Code 客户端发现用的环境描述（部分兼容层） |
| `GET /ws?orchestrationProtocol=2` | T3 Code 的 Effect RPC JSON 帧（部分兼容层） |

SSE 事件带 `id: <seq>` 和 `event: <type>`；断线后用 `after` 或
`Last-Event-ID` 续传。WebSocket 连接先收到 `hello`，之后是 `event` 帧；
客户端可以发 `ping`、`subscribe`、`unsubscribe` 做线程级过滤。所有写操作都走
REST，socket 只负责推送。

事件类型：

```
project.updated   settings.updated   thread.created   thread.updated
thread.deleted    run.started        run.finished     message.updated
message.delta     approval.requested approval.resolved notice
```

## 权限模式

| T3Go | Claude Code | Codex |
| --- | --- | --- |
| `supervised` | `manual`，通过 MCP 审批工具询问 T3Go 客户端 | `workspace-write` 沙箱 |
| `accept-edits` | `acceptEdits` | `workspace-write` + `--approve-for-me` |
| `auto` | `auto` | `workspace-write` + `--approve-for-me` |
| `full-access` | `bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` |

Claude Code 的 `supervised` 会启动一个本地 MCP helper（`t3go mcp`），
把要执行的工具和参数发回环境；客户端弹出审批框，允许后工具才执行。没有审批
通道时（例如直接调用 provider 适配器）需要审批的动作会被拒绝。Codex 和
OpenCode 没有等价的非交互审批入口，按上表的沙箱/自动策略运行。

## 代码结构

```
cmd/t3go/            CLI（serve/pair/doctor/version）
cmd/t3go-app/        NSGo 客户端
internal/proto/      客户端与服务端共享类型
internal/store/      SQLite schema、查询
internal/events/     事件总线、重放、剪枝
internal/provider/   Claude/Codex/OpenCode CLI 适配与进程管理
internal/git/        status、checkpoint、diff、worktree
internal/orchestrator/ 项目/线程/run 生命周期
internal/server/     HTTP、SSE、WebSocket、鉴权
internal/client/     Go 客户端（REST + SSE 重连）
internal/config/     数据目录、token、监听地址
```

更细的请求流和 run 生命周期见 [docs/architecture.md](docs/architecture.md)，上游
T3 Code 协议目前覆盖到哪里见 [docs/upstream-compat.md](docs/upstream-compat.md)。

## 本地调试

一条命令启动本地环境、网页版和桌面端；桌面端会自动连上刚启动的服务：

```sh
./scripts/dev.sh
```

默认数据目录 `.dev/t3go`、监听 `127.0.0.1:4870`，服务日志在
`/tmp/t3go-dev-server.log`。可用环境变量调整：

| 变量 | 默认 | 说明 |
| --- | --- | --- |
| `T3GO_DEV_HOME` | `.dev/t3go` | 调试环境的数据目录 |
| `T3GO_DEV_LISTEN` | `127.0.0.1:4870` | 监听地址 |
| `T3GO_DEV_WEB` | `1` | 构建并提供网页版 |
| `T3GO_DEV_APP` | `1` | 启动桌面端；设为 0 时前台只跑服务 |

也可以分终端手动跑：

```sh
# 终端 1：服务 + 网页版
./scripts/build-web.sh
go run ./cmd/t3go serve --home .dev/t3go --listen 127.0.0.1:4870 --web web

# 终端 2：桌面端（粘贴 serve 打印的 t3go://pair 链接）
go run ./cmd/t3go-app
```

桌面端也接受 `T3GO_SERVER` / `T3GO_TOKEN` 环境变量直接连接，适合脚本或
IDE 调试配置。

## 开发与测试

```sh
go test ./...
go vet ./...
gofmt -l .
```

UI 集成测试用 `nsgotest` 在无头环境里启动真实 server，验证连接、侧栏、线程
transcript 的渲染，命令面板与快捷键，运行中排队并自动发送，运行历史里某次 run
的 diff，附件随消息发送，断线后的重连提示，新建线程时选项目与 worktree 分支，
设置默认 provider/model/权限后新线程继承，以及重命名、归档、删除线程和项目
后服务端状态的变化。

## 已知限制

- 一次线程同时只允许一个 run（`409 thread is already running`）。
- 附件上限 25 MB、每条消息最多 10 个；provider 通过本地文件路径读取，不做内联图片消息。
- 上传文件保存在环境的数据目录，删除线程或项目时一起清理；从输入框移除的附件会立即删除。
- 事件保留最近约 5000 条；断线太久时需要重新取快照。
- 流式文本每 200ms 落一次库；token 级 delta 也会写事件表，长回复会产生较多写入。
- checkpoint 使用 `git stash create`，只覆盖 Git 项目；非 Git 项目没有 diff。
- 删除项目会删除其线程记录和 worktree 引用，不会删除用户目录本身。
- NSGo 仍处于早期，API 可能随版本变化；本仓库锁定了一个伪版本。
- 网页版由服务端同源提供，使用浏览器 `localStorage` 保存地址和 token；附件用系统文件选择器添加。
- 语音听写只在系统提供识别器时显示（macOS/iOS），其他平台不出现麦克风按钮。
- 审批目前只覆盖 Claude Code；一次 run 结束时未回答的审批自动拒绝。
- 终端在 macOS/Linux/BSD 使用 PTY，Windows 使用 ConPTY；Windows 优先 `pwsh.exe`，其次 `powershell.exe`，最后 `cmd.exe`。
- 终端前端使用 `@xterm/xterm` 与 `@xterm/addon-fit`（MIT），资源内嵌在客户端二进制中。
