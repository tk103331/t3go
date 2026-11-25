# T3Go

[中文](README.zh-CN.md) · [Architecture](docs/architecture.md) · [API](docs/api.md) · [MIT License](LICENSE)

![Go](https://img.shields.io/badge/Go-1.27.3+-00ADD8?logo=go&logoColor=white)
![License](https://img.shields.io/badge/license-MIT-green)

T3Go is a Go implementation of the [T3 Code](https://github.com/pingdotgg/t3code)
environment server, shipped with a native multi-platform client built on
[MyGo](https://github.com/egoist/mygo). One UI codebase runs on macOS, Windows,
Linux, Android, iOS and the browser as WebAssembly.

An environment owns projects, threads and runs, and launches Claude Code,
Codex CLI or OpenCode as local agent processes. The client sends actions over
HTTP and receives streamed updates over SSE or WebSocket.

## Highlights

- **Environments** — saved connections, LAN direct connections first, Relay
  fallback for remote access, outbound tunnels, per-environment drafts and
  queued messages. Relay accounts discover owned environments and authorize
  requests without forwarding the environment token.
- **Threads** — streaming text, reasoning and tool cards, stop/retry/fork,
  context usage, interactive approvals, attachments, `@` file mentions and
  `/` skills and commands. One run per thread, with checkpointed diffs.
- **Client** — MyGo native UI on desktop, phones and the web; responsive
  composer and new-thread layouts; model search; a config popover when space is
  tight; command palette; light, dark and accent themes; English and Chinese.
- **Code surfaces** — file tree with create, rename, delete and context
  actions; file tabs with selection, find and soft wrap; working-tree, run and
  commit diffs as panel tabs with unified/split layouts; terminals; a Git graph
  with search, lanes and commit diffs; pull requests, previews and simulators.
- **Server** — SQLite persistence, event replay, Bearer auth, SSE/WebSocket,
  provider CLI adapters, checkpoints and worktrees, PR creation through `gh`,
  and Relay tunnels.

## Requirements

- **Go 1.27.3 or newer** (the version in `go.mod`)
- At least one agent CLI on `PATH`:
  - `claude` (Claude Code)
  - `codex` (Codex CLI)
  - `opencode` (OpenCode)
- `git` for checkpoints, diffs, worktrees and commits (optional but recommended)
- `gh` for creating and managing pull requests (optional)
- For mobile packages: JDK 17, the Android SDK and NDK, and Xcode for iOS

## Quick start

```sh
go build -o bin/t3go ./cmd/t3go
go build -o bin/t3go-app ./cmd/t3go-app

./bin/t3go doctor      # check the data directory, token and agent CLIs
./bin/t3go serve       # 127.0.0.1:4870, data in ~/.t3go
```

`serve` prints a token file and a pairing link:

```
t3go://pair?url=http://127.0.0.1:4870&token=...
```

Start the client and paste the link on the connection screen:

```sh
./bin/t3go-app
```

Add a project directory, create a thread, choose provider, model and permission,
and start the conversation.

To serve the landing page and the web client from the same environment:

```sh
./scripts/build-web.sh
./bin/t3go serve --web web
# landing page: http://127.0.0.1:4870/
# web client:   http://127.0.0.1:4870/app/
```

Shortcuts (use `Ctrl` outside Apple platforms):

| Shortcut | Action |
| --- | --- |
| `⌘K` | Search threads and commands |
| `⌘N` | New thread |
| `⌘⇧N` | New project |
| `⌘,` | Settings |
| `⌘B` | Show or hide the sidebar |

## Remote access

The server listens on loopback by default. To connect a phone or another
machine, listen on the network:

```sh
./bin/t3go serve --listen 0.0.0.0:4870
```

Authentication stays on. `--no-auth` is only for a loopback address.

- **Same machine** — the desktop client connects to its bundled local
  environment without an account.
- **Same Wi-Fi** — scan the pairing link and connect with the environment
  token; no account required.
- **Across networks** — register the computer with Relay, then sign in to the
  same account on a phone or the web client to discover it. Relay issues short
  HMAC request signatures and never forwards the environment token.

See [docs/environments-and-relay.md](docs/environments-and-relay.md) for the
trust boundaries between accounts, environments, Relay and computers.

## CLI

| Command | Description |
| --- | --- |
| `t3go serve [--home DIR] [--listen HOST:PORT] [--no-auth] [--web DIR] [--quiet]` | Start an environment; `--web` also serves the web client |
| `t3go pair [--home DIR] [--listen HOST:PORT] [--json]` | Print the pairing link |
| `t3go doctor [--home DIR]` | Check the database, token, provider CLIs and Git |
| `t3go version` | Print the version and platform |

The data directory is `~/.t3go` unless `T3GO_HOME` or `--home` overrides it:

```
~/.t3go/
  token          # 32-byte random token
  t3go.sqlite    # state and events
  worktrees/     # per-thread worktrees
  uploads/       # attachments sent with messages
  logs/t3go.log  # server log
  relay.json     # optional Relay address, environment ID and tunnel credentials
```

## Permissions

| T3Go | Claude Code | Codex |
| --- | --- | --- |
| `supervised` | `manual`; tool calls come back through a local MCP helper for approval | `workspace-write` sandbox |
| `accept-edits` | `acceptEdits` | `workspace-write` + `--approve-for-me` |
| `auto` | `auto` | `workspace-write` + `--approve-for-me` |
| `full-access` | `bypassPermissions` | `--dangerously-bypass-approvals-and-sandbox` |

Claude Code's `supervised` mode runs a local MCP helper (`t3go mcp`) that asks
the client before a tool runs. Without an approval channel the action is
denied. Codex and OpenCode have no equivalent non-interactive approval entry
and follow their sandbox/automatic policies above.

## API

Actions use REST; updates stream over SSE or WebSocket. Every authenticated
request accepts `Authorization: Bearer <token>` or `?token=<token>`.

Key endpoints:

| Endpoint | Purpose |
| --- | --- |
| `GET /api/v1/state` | Snapshot of projects, threads, providers, models and settings |
| `GET/POST /api/v1/projects`, `/threads` | Projects and threads |
| `POST /api/v1/threads/{id}/messages` | Send a message and start a run |
| `GET /api/v1/threads/{id}/files`, `/file`, `/raw` | Browse and read the checkout |
| `GET /api/v1/threads/{id}/diff`, `/working-diff`, `/commits/{commit}` | Run, working-tree and commit diffs |
| `POST /api/v1/threads/{id}/files`, `/files/rename`, `DELETE /files` | Create, rename and delete files |
| `GET /api/v1/threads/{id}/graph` | Commit graph with parents and refs |
| `POST /api/v1/threads/{id}/terminal` | Start or reuse a shell |
| `GET /api/v1/threads/{id}/pull-requests` | GitHub pull requests |
| `GET /api/v1/events?after=<seq>`, `GET /ws` | Event streams |

The complete endpoint list, event types and streaming details are in
[docs/api.md](docs/api.md).

## Project layout

```
cmd/t3go/              CLI: serve, pair, doctor, version
cmd/t3go-app/          MyGo client entry; views live in internal/app
internal/app/          Client state and MyGo views for every platform
internal/proto/        Types shared by client and server
internal/store/        SQLite schema and queries
internal/events/       Event bus, replay and pruning
internal/provider/     Claude, Codex and OpenCode CLI adapters
internal/git/          Status, checkpoints, diffs and worktrees
internal/orchestrator/ Project, thread and run lifecycle
internal/server/       HTTP, SSE, WebSocket and authentication
internal/client/       Go client (REST + SSE reconnection)
internal/relay/        Accounts, environment registration, tunnels and HMAC auth
internal/config/       Data directory, token and listen address
```

The request flow and run lifecycle are described in
[docs/architecture.md](docs/architecture.md); upstream T3 Code protocol coverage
is tracked in [docs/upstream-compat.md](docs/upstream-compat.md).

## Development

```sh
go test ./...
go vet ./...
gofmt -l .
```

UI tests use MyGo's `ui.NewTester`. Run the whole local stack with:

```sh
./scripts/dev.sh
```

It starts the environment on `127.0.0.1:4870`, builds the web client and opens
the desktop app against it. The data directory is `.dev/t3go`; `T3GO_DEV_HOME`,
`T3GO_DEV_LISTEN`, `T3GO_DEV_WEB` and `T3GO_DEV_APP` adjust it.

Package the desktop client or a mobile build:

```sh
./scripts/build-app.sh macos_arm64
./scripts/build-app.sh android_arm64
./scripts/build-app.sh ios_simulator
```

The desktop client also accepts `T3GO_SERVER` and `T3GO_TOKEN` to connect from a
script or IDE, and `T3GO_CLIENT_DATA` for an isolated client data directory.

## Release

1. Set the version where it is written, so that the app, package names, the
   download page and the update notes agree:
   - `mygo.json` (`version`)
   - `internal/app/settingsview.go` (`appVersion`)
   - `web/index.html` (the MyGo artifact links and tag)
   - `CHANGELOG.md` (its `## X.Y.Z` section, which the app also shows)
2. Commit, tag `vX.Y.Z`, and push the tag.
3. The [release workflow](.github/workflows/release-mygo.yml) runs on the tag:
   it tests the merge, checks the tag matches `mygo.json`, builds every platform
   with `scripts/release-mygo.sh`, and creates a GitHub Release with the
   artifacts and `SHA256SUMS` for `T3Go-mygo-X.Y.Z-*`.

The desktop apps update themselves through MyGo's signed updates: the workflow
signs each platform's update archive with `MYGO_UPDATER_PRIVATE_KEY` (an Ed25519
key kept in the repository secrets) and uploads `update-<target>.json` next to
it. Installed apps check and install updates under Settings → About;
development builds carry no update feed and update by rebuilding. The private
key lives only in `~/.config/t3go/update-keys/` on the machine that made it and
in the GitHub secret; never commit it.

Build and package locally with:

```sh
./scripts/release-mygo.sh          # release/ artifacts
./scripts/release-mygo.sh upload   # build and upload to a mirror
```

`scripts/upload-mygo.sh` copies the artifacts to the download site configured
in your SSH config (`t3go-server` by default, `/opt/t3go/web`). The workflow
publishes GitHub Releases only; deploying to a self-hosted server remains an
operator step.

## Known limitations

- One run per thread (`409 thread is already running`).
- Uploads are capped at 25 MB and ten attachments per message; agents read them
  from local paths rather than inline images.
- Events keep roughly the last 5000 entries; reconnect after a long gap reloads
  a snapshot.
- Checkpoints use `git stash create` and cover Git projects only; non-Git
  projects have no diffs.
- Deleting a project removes its thread records and worktree references, not the
  user's directory.
- MyGo is still evolving; the client pins one MyGo commit in `go.mod`.
- Speech dictation appears only where the system provides a recognizer
  (macOS/iOS); other platforms have no microphone button.
- Approvals currently cover Claude Code only; unanswered approvals are denied
  when a run ends.
- The web client is served from the environment's origin and stores addresses
  and tokens in browser `localStorage`.
- Upstream T3 Code protocol coverage is partial; see
  [docs/upstream-compat.md](docs/upstream-compat.md).
- Device authorization/revocation, platform secure credential storage, LAN
  auto-discovery and multi-replica Relay are not implemented yet.
- Queued messages pause when a client reopens and must be resumed; a send with an
  unknown result keeps its request ID for a safe retry.

## License

[MIT](LICENSE) © 2026 T3Go contributors.