# API

环境服务的 HTTP / SSE / WebSocket 接口。所有需要鉴权的接口接受
`Authorization: Bearer <token>`，也接受 `?token=<token>`（给无法设置 header
的客户端用）。写操作都走 REST，socket 只负责推送。

## 端点

| 方法与路径 | 说明 |
| --- | --- |
| `GET /api/v1/health` | 健康检查，无需鉴权 |
| `GET /api/v1/state` | 项目、线程、provider、模型、设置的快照 |
| `GET /api/v1/dirs?path=` | 列出连接主机上的目录（移动端新建项目用） |
| `GET/POST /api/v1/projects` | 列出/注册项目 |
| `GET /api/v1/projects/{id}/branches` | 列出 Git 项目的本地分支（新建 worktree 的起点） |
| `PATCH/DELETE /api/v1/projects/{id}` | 更新/删除项目 |
| `GET/POST /api/v1/threads` | 列出/创建线程 |
| `GET/PATCH/DELETE /api/v1/threads/{id}` | 线程详情/更新/删除 |
| `POST /api/v1/threads/{id}/messages` | 发送消息（`content` + `attachmentIds`），启动一次 run |
| `POST /api/v1/threads/{id}/stop` | 停止当前 run |
| `GET /api/v1/threads/{id}/diff?runId=` | 某次 run 的 checkpoint diff |
| `GET /api/v1/threads/{id}/commits/{commit}` | 某个提交的多文件 patch（供提交图双击打开） |
| `POST /api/v1/threads/{id}/git` | 线程 checkout 的 `commit`/`push`/`create_pr`/`commit_push`/`commit_push_pr` |
| `GET /api/v1/threads/{id}/working-diff` | 工作区相对 HEAD 的 patch 与文件统计 |
| `POST /api/v1/threads/{id}/git/stage` / `git/discard` | 暂存 / 丢弃指定文件 |
| `POST /api/v1/threads/{id}/retry` | 在同一线程重跑一轮 prompt |
| `POST /api/v1/threads/{id}/fork` | 从某条消息分叉出新线程 |
| `GET/POST /api/v1/threads/{id}/branches` | 列出本地分支 / 切换到指定分支 |
| `GET /api/v1/threads/{id}/files?path=` | 列出线程 checkout 中的目录；`recursive=1` 返回用于 @ 提及的源文件索引 |
| `POST /api/v1/threads/{id}/files` | 新建文件或文件夹（`path`、`name`、`directory`） |
| `POST /api/v1/threads/{id}/files/rename` | 重命名 checkout 中的文件或文件夹 |
| `DELETE /api/v1/threads/{id}/files?path=` | 删除文件或文件夹（拒绝路径逃逸与 `.git`） |
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
| `POST /api/v1/threads/{id}/open-editor` | 在环境机器上用选定编辑器打开 checkout |
| `POST /api/v1/threads/{id}/side-chat` | 创建或复用线程的隐藏 side chat |
| `GET /api/v1/search/messages?q=` | 跨线程搜索消息文本 |
| `GET /api/v1/usage?days=30` | 统计最近几天的 run token 与成本 |
| `GET /api/v1/preview/servers` | 列出本机正在监听的开发服务器 |
| `POST /api/v1/threads/{id}/runs/{runID}/restore` | 将已跟踪文件回滚到该 run 的 checkpoint |
| `GET /api/v1/devices` | 列出本机可用的 iOS 模拟器与 Android AVD |
| `POST /api/v1/devices/{id}/boot` | 启动 iOS 模拟器或 Android AVD |
| `POST /api/v1/devices/{id}/shutdown` | 关闭模拟器 |
| `GET /api/v1/devices/{id}/screenshot` | 读取模拟器当前画面（PNG，iOS/Android） |
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

## 事件流

SSE 事件带 `id: <seq>` 和 `event: <type>`；断线后用 `after` 或
`Last-Event-ID` 续传。WebSocket 连接先收到 `hello`，之后是 `event` 帧；
客户端可以发 `ping`、`subscribe`、`unsubscribe` 做线程级过滤。

事件类型：

```
project.updated   settings.updated   thread.created   thread.updated
thread.deleted    run.started        run.finished     message.updated
message.delta     approval.requested approval.resolved notice
```