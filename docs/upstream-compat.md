# 上游 T3 Code 协议兼容

T3Go 自己的客户端协议（`/api/v1` + 自有 SSE/WS）仍是主线。这一层让 T3 Code
官方客户端能完成发现和连接握手，读到环境配置、生命周期和线程列表，为后续把
T3Go 的线程/run/工具事件投影成上游 v2 协议打基础。

## 已实现

| 接口 | 说明 |
| --- | --- |
| `GET /.well-known/t3/environment` | 上游的 `ExecutionEnvironmentDescriptor`，`orchestrationProtocolVersion: 2` |
| `GET /ws?orchestrationProtocol=2` | Effect RPC 的 JSON 帧（`_tag: Request/Exit/Chunk`，含 `Ping/Pong`） |
| `server.probe` | 环境探活 |
| `server.getConfig` | 最小 `ServerConfig`（环境、auth、cwd、空 provider/编辑器列表、默认设置） |
| `subscribeServerConfig` | 第一帧 `{version:1,type:"snapshot",config}` |
| `subscribeServerLifecycle` | `welcome` 事件 |
| `orchestration.subscribeShell` | `snapshot` + `synchronized`，包含 T3Go 的项目和线程 shell 行；之后的项目/线程变化以 `project.updated` / `project.removed` / `thread.updated` / `thread.removed` 实时推送 |
| `subscribeAuthAccess` | 空权限流 |
| `orchestration.subscribeThread` | 线程快照：AppThread、文本消息与 `user_message`/`assistant_message` turn item，并按 200ms 的文本更新推送 `message.updated` / `turn-item.updated` 事件 |
| `orchestration.getThreadProjection` | 同一投影的普通请求 |
| `orchestration.dispatchCommand` | `message.dispatch` 映射到 T3Go 的发送，`thread.stop` 映射到停止 |

以上形状用上游本仓库的 `@t3tools/contracts`（0.1.8）与 `effect@4.1.2` 的
`RpcClient` 实际解码验证过：项目/线程 shell、订阅后的实时项目更新，以及带两条
文本消息和 turn item 的线程快照。`message.updated` / `turn-item.updated` 事件
形状也用上游 schema 校验过。

## 尚未实现（完整兼容所需）

- 上游的认证/配对/DPoP 流程：目前 `/ws` 仍用 T3Go 的 bearer token（header 或
  `?token=`），官方客户端的配对链接和会话凭据还不能直接复用
- `ServerConfig` 里的 provider 目录、模型和编辑器列表
- 线程创建/归档/删除等其余 dispatch 命令
- reasoning、工具调用、审批、run 状态在 turn item / v2 事件里的完整投影
- 附件、diff、git、usage 等其余 RPC
- 上游 `t3code://pair` 链接格式

官方客户端现在可以连上、看到项目和线程列表、打开线程读到文本历史，并通过
`message.dispatch` 发送消息。reasoning/工具调用、附件和完整 run 元数据仍会让
细节缺失；T3Go 自己的客户端不受影响。

## 验证方式

`internal/server/server_test.go` 的 `TestUpstreamDiscoveryAndProbe` 覆盖发现、
probe、config、shell 快照和实时项目更新。开发时另用上游 contracts 做了真实解码验证：

```sh
# 在临时目录安装 effect@4.1.2，复制上游 packages/contracts/src
# 用 RpcClient.make(WsRpcGroup) + Socket.layerWebSocket 连接本服务
```

该验证脚本不进入仓库，避免把 Node 工具链带入 Go 模块。
