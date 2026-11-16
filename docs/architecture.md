# 架构

T3Go 由三部分组成：

1. **环境服务**（`internal/server`）：一个进程内的 HTTP 服务，拥有 SQLite、
   事件总线和 orchestrator。
2. **orchestrator**（`internal/orchestrator`）：项目、线程和 run 的业务逻辑。
3. **客户端**（`cmd/t3go-app` + `internal/client`）：NSGo UI 通过 REST 发动作，
   通过 SSE 接收更新。

CLI（`cmd/t3go`）只是环境的启动器；它和客户端可以在同一台机器，也可以分离部署。
桌面安装包自带这个启动器和环境服务，见下文「桌面安装包」。

环境身份保存在 SQLite 中，独立于每次启动的 `Server.ID`。客户端以环境 ID
隔离会话，验证候选路线后优先直连；`internal/relay` 用 WebSocket + yamux
承载出站隧道，将 `/e/{environmentId}/...` 转发到电脑上固定的 T3Go 监听端口。
使用方式、当前边界与后续设计见
[environments-and-relay.md](environments-and-relay.md)。

Relay 账号、环境发现和请求授权使用两套分离的凭证：

1. 客户端用 Relay 用户 Token 登录并调用 `GET /api/v1/relay/environments`，
   只获得当前账号拥有的环境。
2. 客户端请求 `/e/{environmentId}/...`。Relay 验证用户 Token 和环境归属后，
   删除客户端 `Authorization`，用环境专属 `RelayKey` 生成包含方法、环境 ID、
   时间戳和请求 URI 的 HMAC-SHA256 签名。
3. 电脑从本机 `relay.json` 取得同一 `RelayKey`，验证签名和时间窗口后，把请求
   当作正常环境 API 处理。

`RelayKey` 只保存在 Relay 的私有绑定文件和电脑私有配置中；用户 Token 不转发
给电脑。环境 Token 仍可直连或通过旧入口访问，因此局域网配对保持兼容。
客户端由账号发现的环境保存 Relay 路线；已有直连路线只追加 Relay 回退，不把
本机或局域网的强环境 Token 替换成账号 Token。

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
  └─ Service.SendWithRequest
       ├─ 有 requestId 时查已有回执，同键同内容返回原 run
       ├─ 校验线程、provider、project 目录
       ├─ 事务提交 run、用户/assistant 消息、线程 running 状态与可选回执
       ├─ 登记 active run
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

`GET /state` 的 `idempotentMessages` 能力允许主聊天客户端在提交前保存请求 ID。
接收结果不明时暂停队列，可通过 `GET /threads/{id}/receipts/{requestId}` 确认，
或显式以原请求 ID 和原内容重试。回执随线程保留，删除 run 历史不删除接收记录。
这保证同一数据库中同线程请求不会重新创建任务，不承诺外部工具副作用恰好一次。

每次状态变化都会先写 `events` 表再广播，因此新客户端可以用 `after` 重放
最近的窗口；窗口之外的旧事件由快照（`GET /state`、`GET /threads/{id}`）补齐。
`GET /state` 返回快照构建时的 `seq`，客户端从该 `seq` 之后订阅事件，既不会
漏掉构建期间的变化，也不用重放已经包含在快照里的历史。

## SSE 与 WebSocket

- SSE：`GET /api/v1/events?after=<seq>`。连接时先订阅实时 channel，再分页重放
  `after` 之后的事件，按 `seq` 去重，每 20 秒发一个 keepalive 注释。
  客户端有响应头与空闲超时，订阅成功即恢复在线显示；旧流取消后的回调丢弃。
  权限拒绝终止自动恢复，游标缺口重新加载快照。换路线采用冷却和直连连续成功阈值。
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

## 上游 T3 Code 兼容层

`internal/upstream` 实现 T3 Code 的发现描述与 Effect RPC JSON 帧的一个子集：
`server.probe`、`server.getConfig`、`subscribeServerConfig`、
`subscribeServerLifecycle`、`orchestration.subscribeShell` 和
`subscribeAuthAccess`。shell 把 T3Go 的项目/线程投影成只读行；线程详情、发送
和 v2 事件尚未投影。入口是 `/.well-known/t3/environment` 和带
`orchestrationProtocol=2` 的 `/ws`，状态与差距见
[upstream-compat.md](upstream-compat.md)。

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

## 桌面安装包

桌面版不需要用户另外安装服务端：`scripts/build-app.sh` 先为同一个平台编译
`cmd/t3go`，放进 `cmd/t3go-app/resources/`，再由 `go tool nsgo build` 打包。
NSGo 把 `resources/` 拷进 macOS 的 `T3Go.app/Contents/Resources/`，Linux 与
Windows 则放在可执行文件旁边；macOS 上先给这个可执行文件单独做 ad-hoc 签名，
再让安装包整体签名。手机包用 `nsgo.yaml` 的 `mobile.exclude_resources` 排除它。

启动时 `cmd/t3go-app/local_server_exec.go` 找到这个二进制，用
`t3go serve --home <T3GO_HOME 或 ~/.t3go> --listen <地址>:<port> --quiet`
拉起，并从 `<home>/token` 读 Token 连上去。默认地址为 `127.0.0.1`；
Access 开启 LAN direct 后改为 `0.0.0.0`。4870 被占用时依次尝试最多 20 个端口。
如果端口上已经有一个同 home 的 T3Go（Token 能通过 `/api/auth/session`），
就复用它，不重复起进程。健康接口报告实际监听地址和是否为后台进程；
设置改变而复用进程的监听模式不一致时，客户端通过鉴权的本地关闭接口
停止该后台进程，再以正确参数重启。

环境服务的身份来自 home 目录里的 SQLite，所以重复启动得到同一个
`environmentId`，环境列表里只更新地址和 Token，不会多出一条。客户端只在没有
选中任何环境时（首次安装）自动切到本机环境，之后仍停留在用户上次用的环境。
用户在本机环境上执行「忘记」时，客户端记住该环境 ID，不再自动加回。

`t3go serve` 带 `--parent-pid` 时每秒确认启动它的进程还在，app 被强杀也会在
一秒左右退出，不会留下孤儿服务。设置 → Access 的「Keep running in the background」
控制这个开关：开启时 server 不带 `--parent-pid`，关掉窗口后继续跑正在执行的
agent run，下次启动复用同一个进程；关闭时随窗口退出。切换开关会重启由客户端
启动的 server，因为父进程监视只存在于启动命令里。桌面端只负责拉起和连接；
orchestrator 仍然只有环境服务里的那一份，agent CLI（claude/codex/opencode 等）
需要装在运行环境的机器上。

访问边界分为三层：本机桌面使用无需账号；同一局域网的手机通过携带环境 Token
的直连配对链接访问，同样无需账号；只有跨网络访问才需要 Relay 登录。
Relay 用户身份不等于环境执行身份：账号路线由 Relay 验证归属并签名，直连路线
仍验证环境 Token。两种凭证不互相替代，登出账号不会删除独立的局域网配对。
