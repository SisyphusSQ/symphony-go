# symphony-go

`symphony-go` 是 Go 版 Symphony，用来把 Linear issue、隔离 workspace、生命周期 hooks、Codex app-server runner、SQLite 本地状态和 operator surface 串成一个可验证的单实例执行循环。

它的目标不是替代所有项目管理工具，而是在一个明确的 Linear project 和一个执行仓库内，稳定地完成 issue 分发、agent 执行、状态恢复、操作员观察与人工收口。

## 功能概览

- Linear tracker：读取候选 issue、状态、labels、blockers，并提供 typed 写回能力。
- Dispatch policy：按 workflow 中的 active / terminal states、repo label routing、并发限制和 blocker 规则筛选候选 issue。
- Workspace manager：为每个 issue 创建隔离目录，做路径归一化、metadata 复用和 root containment 校验。
- Lifecycle hooks：在 workspace 创建、运行前后、终态清理前执行 shell hook，并记录失败结果。
- Codex runner：通过 `codex app-server` 执行 agent turn，记录 token、tool、approval、turn 等事件。
- Durable state：可选 SQLite state store 记录 runs、sessions、retry queue、agent events 和 suppression，用于重启恢复与 release gate 证据。
- Operator surface：提供本地 HTTP API、CLI 控制命令、只读 TUI 和 Web GUI。

## v1 边界

v1 支持的运行形态是：

- 单个 Go Symphony 实例
- 单个 Linear project
- 单个执行仓库
- 单个 committed `WORKFLOW.md`
- 单个 workspace root
- 单个 SQLite state database
- loopback operator HTTP surface

v1 不承诺 fleet manager、单实例多 repo 执行、企业 RBAC、审批队列、容器化 worker 隔离、secret-manager provider 或非 Linear tracker adapter。

## 构建与验证

```bash
make build
bin/symphony --help
bin/symphony run --help
bin/symphony version
bin/symphony validate --workflow WORKFLOW.md
```

常用验证入口：

```bash
make test
make test-fake-e2e
make test-real-integration
make harness-check
```

`make test-real-integration` 默认安全跳过。只有显式设置真实环境变量并允许外部副作用时，才会进入真实 Linear / Codex dogfood profile。

## 运行

最小本地运行需要准备 `WORKFLOW.md` 中引用的环境变量：

| 环境变量 | 用途 |
| --- | --- |
| `LINEAR_API_KEY` | Linear GraphQL API 访问凭据 |
| `SYMPHONY_WORKSPACE_ROOT` | issue workspace 根目录 |
| `SYMPHONY_STATE_DB` | SQLite durable state 文件路径 |
| `SOURCE_REPO_URL` | workspace hook 克隆执行仓库时使用的 repo URL |

启动前先验证 workflow：

```bash
bin/symphony validate --workflow WORKFLOW.md
```

启动 orchestrator 和 operator HTTP server：

```bash
bin/symphony run --workflow WORKFLOW.md --port 4002 --instance local
```

默认生产安全口径会拒绝高信任 Codex 设置。仅在可信本机 dogfood 或 release gate 中，显式使用：

```bash
bin/symphony run --workflow WORKFLOW.md --port 4002 --instance local --allow-unsafe-codex
```

也可以通过 `SYMPHONY_ALLOW_UNSAFE_CODEX=true` 开启同一 opt-in。

## Operator CLI

operator CLI 默认访问 `http://127.0.0.1:4002`。如需连接其它本地端口，设置：

```bash
export SYMPHONY_OPERATOR_ENDPOINT=http://127.0.0.1:4002
```

常用命令：

```bash
bin/symphony status
bin/symphony doctor
bin/symphony pause
bin/symphony resume
bin/symphony drain
bin/symphony cancel TOO-123
bin/symphony retry TOO-123
bin/symphony cleanup --terminal
bin/symphony tui
bin/symphony tui --run TOO-123
bin/symphony tui --run-id run_xxx
```

HTTP operator API 也暴露在 loopback 上，核心入口包括 `/healthz`、`/readyz`、`/status`、`/runs`、`/metrics` 和 `/api/v1/*`。

## Web GUI

`web/` 是 Vue 3 + Ant Design Vue 的只读 operator dashboard，覆盖 run list、run detail 和 turn timeline。

开发模式：

```bash
cd web
npm install
npm run dev
```

默认 Vite dev server 监听 `127.0.0.1:5173`，并把 `/api/v1` 代理到 `http://127.0.0.1:4002`。如果 operator server 不在默认地址，设置 `VITE_OPERATOR_PROXY_TARGET`。

生产模式：

```bash
cd web
npm run build
cd ..
bin/symphony run --workflow WORKFLOW.md --port 4002 --instance local
```

Release binary 会内嵌 production dashboard。源码运行时如果没有内嵌资源，也可以在 `web/dist/index.html` 存在时从磁盘服务 dashboard。`/api/v1/*` 始终保留给 Go operator API。

## 文档入口

- 协作规则、truth split、提交边界：`AGENTS.md`
- harness 控制面：`docs/harness/control-plane.md`
- Linear 工作流：`docs/harness/linear.md`
- release gate：`docs/test/release-gate/RUNBOOK.md`
- real dogfood：`docs/test/real-dogfood/RUNBOOK.md`
- Go port conformance：`docs/go-port/CONFORMANCE.md`

真实 token、日志、SQLite state、workspace、原始命令输出和机器本地路径不要提交。提交版测试真相应使用 `docs/test/*` 中的脱敏摘要。
