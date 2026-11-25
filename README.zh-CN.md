# T3Go

[English](README.md) · [架构](docs/architecture.md) · [API](docs/api.md) · [MIT 协议](LICENSE)

![Go](https://img.shields.io/badge/Go-1.27.3+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green)

T3Go 是用 Go 重写的 [T3 Code](https://github.com/pingdotgg/t3code) 环境服务，
并附带基于 [MyGo](https://github.com/egoist/mygo) 的原生多端客户端。桌面、手机
与 Web 共用同一份 UI 代码，Web 端以 WebAssembly 运行。

一个 T3Go 环境管理项目目录、线程与运行记录，并把 Claude Code、Codex CLI、
OpenCode 作为本地 agent 进程跑起来。客户端通过 HTTP 发送动作，通过 SSE 或
WebSocket 接收流式事件。

## 主要能力

- **环境与 Relay** — 保存的连接、局域网直连优先、跨网络中继回退、出站隧道、
  按环境隔离的草稿与排队消息。Relay 账号可发现名下环境并签发短时 HMAC 请求
  签名，不转发环境 Token。
- **线程** — 流式文本、思考过程与工具卡片，停止/重试/分叉，上下文用量、交互式
  审批、附件、`@` 文件提及与 `/` Skills 和命令。每个线程同时只跑一个 run，
  并带 checkpoint diff。
- **客户端** — 桌面、手机与 Web 的 MyGo 原生界面；响应式输入区与新建会话布局；
  模型搜索；空间不足时的配置浮窗；命令面板；明/暗主题与强调色；中英文切换。
- **代码面板** — 文件树的新建、重命名、删除与右键操作；文件页签支持选择文本、
  查找与自动换行；工作区、运行与提交 Diff 以页签展示，可切换单栏/双栏；终端；
  带搜索、泳道和提交 Diff 的 Git graph；Pull Request、预览与模拟器。
- **服务端** — SQLite 持久化、事件重放、Bearer 鉴权、SSE/WebSocket、Provider
  CLI 适配、checkpoint 与 worktree、经 `gh` 创建 PR，以及 Relay 隧道。

## 环境要求

- **Go 1.27.3 或更新版本**（以 `go.mod` 为准）
- `PATH` 中至少有一个 agent CLI：
  - `claude`（Claude Code）
  - `codex`（Codex CLI）
  - `opencode`（OpenCode）
- `git` 用于 checkpoint、diff、worktree 与提交（可选但推荐）
- `gh` 用于创建和管理 Pull Request（可选）
- 构建移动端还需要 JDK 17、Android SDK 与 NDK，以及 iOS 所需的 Xcode

## 快速开始

```sh
go build -o bin/t3go ./cmd/t3go
go build -o bin/t3go-app ./cmd/t3go-app

./bin/t3go doctor      # 检查数据目录、token 与 agent CLI
./bin/t3go serve       # 127.0.0.1:4870，数据在 ~/.t3go
```

`serve` 会打印 token 文件和配对链接：

```
t3go://pair?url=http://127.0.0.1:4870&token=...
```

启动客户端，在连接页粘贴配对链接：

```sh
./bin/t3go-app
```

随后添加项目目录、创建线程、选择 provider/model/权限并开始对话。

让同一个环境同时提供首页和网页版：

```sh
./scripts/build-web.sh
./bin/t3go serve --web web
# 首页：  http://127.0.0.1:4870/
# 网页端：http://127.0.0.1:4870/app/
```

快捷键（非 Apple 平台把 `⌘` 换成 `Ctrl`）：

| 快捷键 | 动作 |
| --- | --- |
| `⌘K` | 搜索线程和命令 |
| `⌘N` | 新建线程 |
| `⌘⇧N` | 新建项目 |
| `⌘,` | 设置 |
| `⌘B` | 显示或隐藏侧栏 |

## 远程访问

服务默认只监听 loopback。要让手机或另一台机器连接，监听网络地址：

```sh
./bin/t3go serve --listen 0.0.0.0:4870
```

鉴权保持开启；`--no-auth` 只能配合 loopback 地址使用。

- **本机** — 桌面端直接连接内置的本机环境，不需要账号。
- **同一 Wi-Fi** — 扫描配对链接，用环境 Token 直连，不需要账号。
- **跨网络** — 电脑登记到 Relay；手机或 Web 端登录同一账号后自动发现并打开
  该电脑。Relay 只签发短时 HMAC 请求签名，不接收环境 Token。

账号、环境、Relay 与电脑之间的信任边界见
[docs/environments-and-relay.md](docs/environments-and-relay.md)。

## CLI

| 命令 | 说明 |
| --- | --- |
| `t3go serve [--home DIR] [--listen HOST:PORT] [--no-auth] [--web DIR] [--quiet]` | 启动环境；`--web` 同时提供网页版 |
| `t3go pair [--home DIR] [--listen HOST:PORT] [--json]` | 打印配对链接 |
| `t3go doctor [--home DIR]` | 检查数据库、token、provider CLI 与 Git |
| `t3go version` | 版本与平台信息 |

数据目录默认为 `~/.t3go`，可用 `T3GO_HOME` 或 `--home` 覆盖：

```
~/.t3go/
  token          # 32 字节随机 token
  t3go.sqlite    # 状态与事件
  worktrees/     # 线程独立 worktree
  uploads/       # 随消息发送的附件
  logs/t3go.log  # 服务日志
  relay.json     # 可选，Relay 地址、环境 ID 与本机隧道凭证
```

## 权限模式

| T3Go | Claude Code | Codex |
| --- | --- | --- |
| `supervised` | `manual`，通过本地 MCP helper 询问客户端后执行 | `workspace-write` 沙箱 |
| `accept-edits` | `acceptEdits` | `workspace-write` + `--approve-for-me` |
| `auto` | `auto` | `workspace-write` + `--approve-for-me` |
| `full-access` | `bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` |

Claude Code 的 `supervised` 会启动本地 MCP helper（`t3go mcp`），在工具执行前
询问客户端；没有审批通道时该动作会被拒绝。Codex 和 OpenCode 没有等价的
非交互审批入口，按上表的沙箱/自动策略运行。

## API

动作走 REST，更新通过 SSE 或 WebSocket 推送。需要鉴权的接口接受
`Authorization: Bearer <token>` 或 `?token=<token>`。

常用端点：

| 端点 | 作用 |
| --- | --- |
| `GET /api/v1/state` | 项目、线程、provider、模型与设置的快照 |
| `GET/POST /api/v1/projects`、`/threads` | 项目与线程 |
| `POST /api/v1/threads/{id}/messages` | 发送消息并启动一次 run |
| `GET /api/v1/threads/{id}/files`、`/file`、`/raw` | 浏览与读取 checkout |
| `GET /api/v1/threads/{id}/diff`、`/working-diff`、`/commits/{commit}` | 运行、工作区与提交 Diff |
| `POST /api/v1/threads/{id}/files`、`/files/rename`、`DELETE /files` | 新建、重命名与删除文件 |
| `GET /api/v1/threads/{id}/graph` | 含父子关系与 refs 的提交图 |
| `POST /api/v1/threads/{id}/terminal` | 启动或复用 shell |
| `GET /api/v1/threads/{id}/pull-requests` | GitHub Pull Request |
| `GET /api/v1/events?after=<seq>`、`GET /ws` | 事件流 |

完整端点列表、事件类型与流式协议见 [docs/api.md](docs/api.md)。

## 代码结构

```
cmd/t3go/              CLI：serve、pair、doctor、version
cmd/t3go-app/          MyGo 客户端入口；视图在 internal/app
internal/app/          客户端数据层与 MyGo 视图（桌面/手机/Web 共用）
internal/proto/        客户端与服务端共享类型
internal/store/        SQLite schema 与查询
internal/events/       事件总线、重放与剪枝
internal/provider/     Claude、Codex、OpenCode CLI 适配
internal/git/          status、checkpoint、diff 与 worktree
internal/orchestrator/ 项目、线程与 run 生命周期
internal/server/       HTTP、SSE、WebSocket 与鉴权
internal/client/       Go 客户端（REST + SSE 重连）
internal/relay/        账号、环境登记、隧道与 HMAC 授权
internal/config/       数据目录、token 与监听地址
```

请求流与 run 生命周期见 [docs/architecture.md](docs/architecture.md)；上游
T3 Code 协议覆盖进度见 [docs/upstream-compat.md](docs/upstream-compat.md)。

## 本地开发

```sh
go test ./...
go vet ./...
gofmt -l .
```

UI 测试使用 MyGo 的 `ui.NewTester`。一条命令启动完整本地栈：

```sh
./scripts/dev.sh
```

它会在 `127.0.0.1:4870` 启动环境、构建网页端并打开桌面端。数据目录为
`.dev/t3go`，可用 `T3GO_DEV_HOME`、`T3GO_DEV_LISTEN`、`T3GO_DEV_WEB`、
`T3GO_DEV_APP` 调整。

打包桌面端或移动端：

```sh
./scripts/build-app.sh macos_arm64
./scripts/build-app.sh android_arm64
./scripts/build-app.sh ios_simulator
```

桌面端也接受 `T3GO_SERVER`、`T3GO_TOKEN` 直接连接，以及 `T3GO_CLIENT_DATA`
指定独立的客户端数据目录，便于脚本或 IDE 调试。

## 发布

1. 同步版本号，确保应用、包名、下载页与更新说明一致：
   - `mygo.json`（`version`）
   - `internal/app/settingsview.go`（`appVersion`）
   - `web/index.html`（MyGo 产物链接与 tag）
   - `CHANGELOG.md`（`## X.Y.Z` 段落，也是应用内显示的更新说明）
2. 提交，打标签 `vX.Y.Z`，推送标签。
3. [发布工作流](.github/workflows/release-mygo.yml) 在标签上运行：测试、
   校验标签与 `mygo.json` 一致、用 `scripts/release-mygo.sh` 构建全部平台，
   并创建 GitHub Release，附带 `T3Go-mygo-X.Y.Z-*` 产物与 `SHA256SUMS`。

桌面端通过 MyGo 的签名更新自更新：工作流用 `MYGO_UPDATER_PRIVATE_KEY`
（Ed25519 私钥，存在仓库 Secret 中）签名各平台的更新包，并随 Release 上传
`update-<target>.json`。安装版在“设置 → 关于”检查并安装更新；开发构建不带
更新源，需要重新构建。私钥只存在于本机 `~/.config/t3go/update-keys/` 与
GitHub Secret，切勿提交。

本地构建与打包：

```sh
./scripts/release-mygo.sh          # 产物在 release/
./scripts/release-mygo.sh upload   # 构建并上传到镜像站点
```

`scripts/upload-mygo.sh` 会把产物复制到 SSH 配置里的下载站点
（默认 `t3go-server` 的 `/opt/t3go/web`）。工作流只发布 GitHub Release；
部署到自建服务器仍由运维步骤完成。

## 已知限制

- 每个线程同时只允许一个 run（`409 thread is already running`）。
- 附件上限 25 MB、每条消息最多 10 个；agent 通过本地文件路径读取，不内联图片。
- 事件保留最近约 5000 条；断线太久后需要重新取快照。
- checkpoint 使用 `git stash create`，只覆盖 Git 项目；非 Git 项目没有 diff。
- 删除项目会删除其线程记录和 worktree 引用，不会删除用户目录本身。
- MyGo 仍在演进；客户端在 `go.mod` 中锁定了一个 MyGo 提交。
- 语音听写仅在系统提供识别器时出现（macOS/iOS），其他平台没有麦克风按钮。
- 审批目前只覆盖 Claude Code；run 结束时未回答的审批会自动拒绝。
- 网页端由环境同源提供，地址和 token 保存在浏览器 `localStorage`。
- 上游 T3 Code 协议只覆盖了一部分，见
  [docs/upstream-compat.md](docs/upstream-compat.md)。
- 单设备授权与撤销、平台安全凭证存储、局域网自动发现、多副本中继尚未实现。
- 排队消息在客户端重新打开后暂停，需要明确恢复；发送结果不明的消息保留原请求
  ID，可安全重试。

## 许可证

[MIT](LICENSE) © 2026 T3Go contributors.