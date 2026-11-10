# 架构

T3Go 由三部分组成：

1. **环境服务**（`internal/server`）：一个进程内的 HTTP 服务，拥有 SQLite、
   事件总线和 orchestrator。
2. **orchestrator**（`internal/orchestrator`）：项目、线程和 run 的业务逻辑。
3. **客户端**（`cmd/t3go-app` + `internal/client`）：NSGo UI 通过 REST 发动作，
   通过 SSE 接收更新。

CLI（`cmd/t3go`）只是环境的启动器；它和客户端可以在同一台机器，也可以分离部署。

## 数据模型

```
project 1─n thread 1─n run 1─n message
                 └─ provider_session（续接 CLI 会话）
```

- `project`：一个可被 agent 操作的目录。
- `thread`：一段持续对话，绑定 provider、model、权限模式和可选 worktree。
- `run`：一次用户输入触发的执行，记录状态、错误、用量和 Git checkpoint。
- `message`：transcript 中的一行，按 `seq` 排序；流式回复是一条不断增长的
  `text` 消息，思考过程和工具调用是独立的 `reasoning` / `tool` 消息。

## 一次发送的流程

```
client POST /threads/{id}/messages
  └─ Service.Send
       ├─ 校验线程、provider、project 目录
       ├─ 创建 run + 用户消息 + 运行中的 assistant 消息
       ├─ 标记线程 running，登记 active run
       ├─ 发布 run.started / thread.updated / message.updated
       └─ go execute(...)
            ├─ git.Checkpoint（stash create + refs/t3go/checkpoints/...）
            ├─ provider.Run（CLI 子进程，逐行解析 JSON）
            │    ├─ Sink.Text       → message.delta + 定期 message.updated
            │    ├─ Sink.Reasoning  → message.updated
            │    ├─ Sink.ToolStart/Finish → message.updated
            │    └─ Sink.Usage/Session
            └─ finish
                 ├─ 落库最终消息与 run
                 ├─ 发布 message.updated / run.finished
                 └─ 线程回到 idle 或 failed
```

服务启动时把上次异常退出遗留的 `running` run 和线程标成失败（原因是环境重启），
关闭时先结束 SSE/WebSocket 推送、取消活动 run 并等待它们落库，再关闭数据库。

每次状态变化都会先写 `events` 表再广播，因此新客户端可以用 `after` 重放
最近的窗口；窗口之外的旧事件由快照（`GET /state`、`GET /threads/{id}`）补齐。
`GET /state` 返回快照构建时的 `seq`，客户端从该 `seq` 之后订阅事件，既不会
漏掉构建期间的变化，也不用重放已经包含在快照里的历史。

## SSE 与 WebSocket

- SSE：`GET /api/v1/events?after=<seq>`。连接时先订阅实时 channel，再分页重放
  `after` 之后的事件，按 `seq` 去重，每 20 秒发一个 keepalive 注释。
- WebSocket：`GET /ws`。服务端先发 `hello`（协议版本、server id、lastSeq），
  然后推送 `event` 帧。客户端可发 `ping`、`subscribe`、`unsubscribe` 做线程过滤。
- 两个通道都不接受写操作，避免同一业务出现第二套入口。

## 权限审批

supervised 的 Claude 运行时，适配器写一份临时 MCP 配置并传
`--permission-prompt-tool mcp__t3go__approve`，其中服务器命令是
`t3go mcp --url ... --token ... --thread ... --run ...`。Claude 需要审批时调用
该工具；helper 用 stdio JSON-RPC 应答，同时把工具名和参数 POST 到
`/api/v1/threads/{id}/approvals`，请求一直挂起，直到客户端
`POST /api/v1/approvals/{id}` 给出允许/拒绝（或 run 结束，自动拒绝）。审批和
结果以 `approval.requested` / `approval.resolved` 事件广播，客户端重连时会用
`GET /api/v1/approvals` 补齐还没回答的请求。

## Web 客户端

`scripts/build-web.sh` 用 `GOOS=js GOARCH=wasm` 构建 `cmd/t3go-app`，生成
`app.wasm`、`wasm_exec.js` 和 `index.html`。`t3go serve --web DIR` 在 `/` 下
只提供该目录里的文件（不列目录、不需要鉴权）；页面与 API 同源，Go 的 js
`net/http` 走浏览器 fetch，SSE 因此也能流式读取。地址和 token 保存在
`localStorage`（键前缀 `nsgo:default:`），所以刷新后会自动重连。

## 附件

客户端把文件上传到 `POST /api/v1/uploads`，环境把内容存到 `$T3GO_HOME/uploads/<id>`，
元数据写入 `uploads` 表。发送消息时只传 `attachmentIds`；orchestrator 校验后把
用户消息的内容与附件元数据一起落库，并在给 provider 的 prompt 末尾追加一行
`- /abs/path (name, mime)`，让 CLI 用自己的文件读取工具打开。删除线程或项目时，
其消息引用的上传文件与元数据一并清理。

## Provider 适配

`internal/provider` 定义了 `Provider`（探测版本/模型）和 `Runner`（执行 turn）。
每个 CLI 适配器负责：

1. 组装非交互参数（print/JSON 输出、模型、权限、会话续接）。
2. 启动子进程，放入独立进程组（POSIX 上 `Setpgid`），停止时整组终止。
3. 逐行解析 CLI 的 JSON 事件，映射到 `Sink` 的文本、思考、工具、用量和 session。

CLI 的原始 JSON 差异被限制在各自的适配器内；orchestrator 只看到统一的 Sink。

## Git checkpoint

每个 run 开始前执行 `git stash create`，得到一个 commit 对象，然后写入隐藏 ref
`refs/t3go/checkpoints/<thread>/<run>`。它不改变工作区，也不影响用户的分支。
`GET /threads/{id}/diff` 用 `git diff <checkpoint>` 展示该 run 的改动。

线程选择 worktree 时，目录为 `$T3GO_HOME/worktrees/<slug>-<id>`，分支默认
`t3go/<slug>`；项目本身必须是 Git 仓库。

## 客户端

`internal/client` 是纯 Go 的 REST + SSE 客户端：

- `do` 统一处理 JSON、Bearer token 和结构化错误。
- `Stream` 在断线后按 1s→15s 退避重连，并用收到的 `seq` 继续。
- `ParsePairing` 支持 `t3go://pair?url=...&token=...`。

NSGo UI（`cmd/t3go-app`）把信号和事件分开：

- HTTP 结果和事件都通过 `cx.Post` 回到 UI 循环再改信号。
- `core.Dynamic` 负责在连接状态/选中线程变化时重建视图；列表用 `core.Each`
  按消息/线程 ID 保持行身份。
- transcript 按 `runId` 分组，组内消息间距小，组间使用聊天气泡的节奏。
- 宽度 ≥ 760pt 时使用 `ui.SidebarShell`；窄屏在“列表 ↔ 线程”两个页面间切换。

## 为什么不用 Web 目标

NSGo 的 `js/wasm` 目标不能直接使用 `net/http`。当前客户端面向原生多端
（macOS/iOS/Android/Windows/Linux）。Web 端需要一层浏览器网络适配（fetch +
EventSource 或 WebSocket），这不在当前 MVP 内。
