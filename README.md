# T3Go

用 Go 重写的 [T3 Code](https://github.com/pingdotgg/t3code) 服务端，以及基于
[NSGo](https://github.com/oneo-me/nsgo) 的多端原生客户端。

一个 T3Go 环境（environment）管理项目目录、线程、运行记录，并把 Claude Code、
Codex CLI、OpenCode 作为本地 agent 进程跑起来。客户端通过 HTTP 取快照、发动作，
通过 SSE 或 WebSocket 接收流式事件。

## 当前状态

已实现：

- SQLite 持久化：项目、线程、消息、运行、设置、事件重放窗口
- 线程运行生命周期：流式文本、思考过程、工具调用、停止、失败、用量
- Provider CLI 适配：`claude`、`codex`、`opencode`，含模型列表与版本探测
- Git：工作区状态、每个 run 的 checkpoint（`git stash create` + 隐藏 ref）、run diff、worktree
- HTTP REST API、SSE 事件流、WebSocket 事件流、Bearer token 鉴权、CORS
- `t3go` CLI：`serve`、`pair`、`doctor`、`version`
- NSGo 客户端：连接页、项目/线程侧栏、聊天流、新建/重命名/删除项目与线程、设置、diff 查看、窄屏布局、错误提示条
- 测试：store、orchestrator（fake provider）、provider 流解析、HTTP/SSE/WS、Go 客户端、NSGo UI 集成

尚未实现（有意留到后续）：

- 交互式权限审批（supervised 模式下 CLI 进程内的确认框）
- 附件、图片、语音输入
- Web 目标（`js/wasm` 不能直接用 `net/http`，需要浏览器 fetch/EventSource 适配层）
- 与上游 t3code 客户端协议的兼容

## 环境要求

- Go 1.26+
- 至少安装一个 agent CLI 并能在 `PATH` 中找到：
  - `claude`（Claude Code）
  - `codex`（Codex CLI）
  - `opencode`（OpenCode）
- `git` 可选，用于 checkpoint、diff 和 worktree

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

启动时会打印 token 文件和一条配对链接：

```
t3go://pair?url=http://127.0.0.1:4870&token=...
```

运行客户端：

```sh
./bin/t3go-app
```

在连接页填入服务地址和 token（也可以直接粘贴 `t3go://pair?...` 链接），
点击 Connect。之后可以添加项目目录、创建线程、选择 provider/model/权限并开始对话。

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
| `t3go serve [--home DIR] [--listen HOST:PORT] [--no-auth] [--quiet]` | 启动环境 |
| `t3go pair [--home DIR] [--listen HOST:PORT] [--json]` | 打印配对链接 |
| `t3go doctor [--home DIR]` | 检查数据库、token、provider CLI、git |
| `t3go version` | 版本与平台信息 |

数据目录默认为 `~/.t3go`，可用 `T3GO_HOME` 或 `--home` 覆盖：

```
~/.t3go/
  token          # 32 字节随机 token
  t3go.sqlite    # 状态与事件
  worktrees/     # 线程独立 worktree
  logs/
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
| `POST /api/v1/threads/{id}/messages` | 发送消息，启动一次 run |
| `POST /api/v1/threads/{id}/stop` | 停止当前 run |
| `GET /api/v1/threads/{id}/diff?runId=` | 某次 run 的 checkpoint diff |
| `GET /api/v1/providers` | provider 与模型 |
| `GET/PATCH /api/v1/settings` | 环境默认设置 |
| `GET /api/v1/fs?path=` | 列出子目录，供目录选择器使用 |
| `GET /api/v1/events?after=<seq>` | SSE 事件流 |
| `GET /ws` | WebSocket 事件流 |

SSE 事件带 `id: <seq>` 和 `event: <type>`；断线后用 `after` 或
`Last-Event-ID` 续传。WebSocket 连接先收到 `hello`，之后是 `event` 帧；
客户端可以发 `ping`、`subscribe`、`unsubscribe` 做线程级过滤。所有写操作都走
REST，socket 只负责推送。

事件类型：

```
project.updated   thread.created   thread.updated   thread.deleted
run.started       run.finished     message.updated  message.delta
notice
```

## 权限模式

| T3Go | Claude Code | Codex |
| --- | --- | --- |
| `supervised` | `manual`，且关闭交互提示 | `workspace-write` 沙箱 |
| `accept-edits` | `acceptEdits` | `workspace-write` + `--approve-for-me` |
| `auto` | `auto` | `workspace-write` + `--approve-for-me` |
| `full-access` | `bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` |

因为 provider 以非交互的 print 模式运行，**supervised 无法弹出审批**：
需要审批的动作会直接失败或被拒绝，而不是等待用户确认。需要 agent 自主完成
工作时请选择 `accept-edits`、`auto` 或 `full-access`。

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

更细的请求流和 run 生命周期见 [docs/architecture.md](docs/architecture.md)。

## 开发与测试

```sh
go test ./...
go vet ./...
gofmt -l .
```

UI 集成测试用 `nsgotest` 在无头环境里启动真实 server，验证连接、侧栏、线程
transcript 的渲染，以及重命名、删除线程和项目后服务端状态的变化。

## 已知限制

- 一次线程同时只允许一个 run（`409 thread is already running`）。
- 事件保留最近约 5000 条；断线太久时需要重新取快照。
- 流式文本每 200ms 落一次库；token 级 delta 也会写事件表，长回复会产生较多写入。
- checkpoint 使用 `git stash create`，只覆盖 Git 项目；非 Git 项目没有 diff。
- 删除项目会删除其线程记录和 worktree 引用，不会删除用户目录本身。
- NSGo 仍处于早期，API 可能随版本变化；本仓库锁定了一个伪版本。
