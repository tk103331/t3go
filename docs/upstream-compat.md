# 上游 T3 Code 协议兼容

T3Go 自己的客户端协议（`/api/v1` + 自有 SSE/WS）仍是主线。这一层让 T3 Code
官方客户端能完成发现和连接握手，读到环境配置、生命周期和线程列表，为后续把
T3Go 的线程/run/工具事件投影成上游 v2 协议打基础。

## 已实现

| 接口 | 说明 |
| --- | --- |
| `GET /.well-known/t3/environment` | 上游的 `ExecutionEnvironmentDescriptor`，`orchestrationProtocolVersion: 2` |
| `POST /oauth/token` | 用环境 token 换取 bearer access token（token-exchange 表单） |
| `GET /api/auth/session` | 当前 bearer 会话的权限 |
| `POST /api/auth/websocket-ticket[/{use}]` | 一次性 socket ticket |
| `/ws?...&wsTicket=<ticket>` | 用 ticket 升级 WebSocket，或用 Bearer header |
| `GET /ws?orchestrationProtocol=2` | Effect RPC 的 JSON 帧（`_tag: Request/Exit/Chunk`，含 `Ping/Pong`） |
| `server.probe` | 环境探活 |
| `server.getConfig` | `ServerConfig`：环境、auth、cwd、T3Go 的 provider/模型目录、默认设置 |
| `subscribeServerConfig` | 第一帧 `{version:1,type:"snapshot",config}` |
| `subscribeServerLifecycle` | `welcome` 事件 |
| `orchestration.subscribeShell` | `snapshot` + `synchronized`，包含 T3Go 的项目和线程 shell 行；之后的项目/线程变化以 `project.updated` / `project.removed` / `thread.updated` / `thread.removed` 实时推送 |
| `subscribeAuthAccess` | 空权限流 |
| `orchestration.subscribeThread` | 线程快照：AppThread、run 列表、文本消息与 `user_message`/`assistant_message`/`reasoning`/`command_execution` turn item；运行中推送 `message.updated`、`turn-item.updated`、`run.created` / `run.updated` 事件 |
| `orchestration.getThreadProjection` | 同一投影的普通请求 |
| `orchestration.launchThread` | 用客户端的 threadId、项目、模型和 runtime mode 创建线程，可带首条消息；`workspaceStrategy` 的 `root`、`worktree`、`existing_worktree` 都映射到 T3Go |
| `orchestration.dispatchCommand` | `message.dispatch`、`thread.stop`、`thread.archive`/`unarchive`、`thread.delete` |
| `orchestration.getTurnDiff` / `getFullThreadDiff` | 从 run checkpoint 到当前工作区的 diff，带 turn 范围 |
| `attachments.createUploadUrl` / `attachments.delete` | 预建附件并返回带 token 的上传 URL；文件用 POST 写入 |
| `assets.persistChatAttachments` | 把粘贴图片的 data URL 存成附件 |
| `assets.createUrl` | 给附件签发可预览/下载的 URL |
| `vcs.refreshStatus` / `subscribeVcsStatus` | 项目或 worktree 的分支、ahead/behind、工作区文件增删行；订阅先发 snapshot（不持续 watch 文件） |
| `vcs.refreshBranch` / `subscribeVcsBranch` | 当前分支名 |
| `orchestration.searchThreads` | 跨线程搜索文本消息，返回 thread/project、消息 id、片段和匹配度 |
| `orchestration.searchThread` | 线程内查找，返回匹配总数、当前匹配和 entry id |
| `server.getUsageSummary` | 按天/provider/model 聚合 T3Go 的 run 用量与费用 |

以上形状用上游本仓库的 `@t3tools/contracts`（0.1.8）与 `effect@4.1.2` 的
`RpcClient` 实际解码验证过：项目/线程 shell、订阅后的实时项目更新，以及带两条
文本消息和 turn item 的线程快照。`message.updated` / `turn-item.updated` / `run.created` 事件、diff、上传 URL、
ChatAttachment、asset URL、VCS 状态、搜索和 usage 形状也用上游 schema 校验过。

## 尚未实现（完整兼容所需）

- DPoP 和 T3 Connect 云身份：直接配对的 bearer token exchange 已实现，DPoP
  证明和云 relay 流程尚未实现
- `t3code://pair` 链接本身：`t3go serve`/`t3go pair --json` 会给出
  `http://host/#token=...` 形式的直连地址，官方客户端可直接作为 pairing URL
- run attempt/node、审批、用量等更深层的 v2 投影
- git 提交/分支切换等写操作、PR 等其余 RPC
- VCS 状态的文件监听（目前订阅只发 snapshot，刷新由客户端触发）
- 图片尺寸与内联预览之外的 asset 类型（workspace-file 等）
- 上游 `t3code://pair` 链接格式

官方客户端现在可以用 bearer 直连配对（把 `t3go serve` 打印的 `t3` 地址作为
pairing URL）、看到项目和线程列表、打开线程读到文本历史、通过
`launchThread` 新建线程、通过 `message.dispatch` 发送消息，并归档/删除线程。reasoning/工具调用、附件和完整 run 元数据仍会让
细节缺失；T3Go 自己的客户端不受影响。

## 验证方式

`internal/server/server_test.go` 的 `TestUpstreamDiscoveryAndProbe` 覆盖发现、
probe、config、shell 快照和实时项目更新。开发时另用上游 contracts 做了真实解码验证：

```sh
# 在临时目录安装 effect@4.1.2，复制上游 packages/contracts/src
# 用 RpcClient.make(WsRpcGroup) + Socket.layerWebSocket 连接本服务
```

该验证脚本不进入仓库，避免把 Node 工具链带入 Go 模块。
