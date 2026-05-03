# Symphony Go 重构方案：从工作流到生产化设计

## 0. 目标结论

目标可以定义为：

```text
用 Go 实现一个符合 SPEC.md 的 Symphony 生产版实现。
当前阶段不在目标 Go 项目中保留 elixir/。
外部使用当前 Symphony 作为 bootstrap orchestrator。
Go 项目自己维护 WORKFLOW.md、SPEC.md、AGENTS.md、.codex/skills 和实现代码。
生产设计先采用：
  单实例 → 单 Linear project
  单实例 → 单执行 repo
  多 repo 进度共享同一个 Linear project 时，用 repo label routing 拆成多个实例。
```

核心部署模式：

```text
外部 Symphony / bootstrap runner
        ↓
读取目标 Go 项目的 WORKFLOW.md
        ↓
监听一个 Linear project
        ↓
根据 issue 状态和 label 调度 agent
        ↓
每个 issue 一个隔离 workspace
        ↓
Codex 修改 Go 项目、开 PR、进入 Human Review
```

---

## 1. 目标架构

### 1.1 推荐目录结构

```text
~/tools/openai-symphony/              # 外部 bootstrap orchestrator
  elixir/
    bin/symphony

~/code/symphony-go/                   # Go 目标项目
  WORKFLOW.md
  SPEC.md
  AGENTS.md
  docs/
    go-port/
      CONFORMANCE.md
      ARCHITECTURE.md
      TESTING.md
      PRODUCTION.md
  .codex/
    skills/
      linear/
      commit/
      push/
      pull/
      land/
  cmd/
    symphony/
      main.go
  internal/
    workflow/
    config/
    tracker/
      linear/
    workspace/
    hooks/
    orchestrator/
    agent/
      codex/
    tools/
      lineargraphql/
    observability/
    state/
    policy/
  go.mod
  go.sum

~/code/symphony-go-workspaces/         # 每个 Linear issue 的隔离 workspace
  TOO-116/
  TOO-117/
  TOO-118/
```

### 1.2 外部 Symphony 只作为 bootstrap orchestrator

目标 Go 项目里不保留 `elixir/`。

当前 Elixir Symphony 只在项目外部运行：

```bash
cd ~/tools/openai-symphony/elixir

export LINEAR_API_KEY=...
export SOURCE_REPO_URL=git@github.com:your-org/symphony-go.git
export SYMPHONY_WORKSPACE_ROOT=~/code/symphony-go-workspaces

mise exec -- ./bin/symphony ~/code/symphony-go/WORKFLOW.md
```

---

## 2. 核心工作流

### 2.1 Linear 状态机

推荐状态：

```text
Backlog
  ↓
Todo
  ↓
In Progress
  ↓
Human Review
  ↓        ↘
Merging     Rework
  ↓          ↓
Done      In Progress
```

`WORKFLOW.md` 中建议配置：

```yaml
tracker:
  active_states:
    - Todo
    - In Progress
    - Rework
    - Merging

  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate
```

### 2.2 状态语义

| 状态 | agent 行为 |
|---|---|
| `Backlog` | 不处理 |
| `Todo` | 立即转 `In Progress`，创建或更新 `## Codex Workpad`，开始执行 |
| `In Progress` | 继续执行 |
| `Human Review` | 不写代码，等待人类 review |
| `Rework` | 返工，重新读 issue、review comments、重建计划 |
| `Merging` | 执行 land flow，不直接 `gh pr merge` |
| `Done` | 终态，不处理 |

---

## 3. `WORKFLOW.md` 设计

### 3.1 文件职责

`WORKFLOW.md` 分两部分：

```text
YAML front matter  →  orchestrator 运行配置
Markdown body      →  agent 执行 prompt
```

推荐 core keys：

```yaml
tracker:
polling:
workspace:
hooks:
agent:
codex:
```

---

### 3.2 推荐 `WORKFLOW.md`

```md
---
tracker:
  kind: linear
  api_key: "$LINEAR_API_KEY"
  project_slug: "symphony-go"
  active_states:
    - Todo
    - In Progress
    - Rework
    - Merging
  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate

polling:
  interval_ms: 5000

workspace:
  root: "$SYMPHONY_WORKSPACE_ROOT"

hooks:
  after_create: |
    git clone "$SOURCE_REPO_URL" .
    go version
    go mod download

  before_run: |
    git fetch origin main
    git status --short

  after_run: |
    git status --short || true

  before_remove: |
    git clean -fdx || true

  timeout_ms: 60000

agent:
  max_concurrent_agents: 2
  max_turns: 20
  max_concurrent_agents_by_state:
    Merging: 1
    Rework: 1

codex:
  command: codex app-server
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
  turn_timeout_ms: 3600000
  read_timeout_ms: 5000
  stall_timeout_ms: 300000
---

You are working on Linear issue `{{ issue.identifier }}`.

Title: {{ issue.title }}

Description:
{{ issue.description }}

## Mission

You are implementing a Go reimplementation of Symphony.

Treat `SPEC.md` as the implementation contract. Treat the existing Elixir implementation only as behavioral reference, not as source code to mechanically translate.

## Repository rules

1. Work only inside the provided workspace.
2. Do not add Elixir source code to this repository.
3. Implement the Go version in this repository.
4. Keep `docs/go-port/CONFORMANCE.md` updated whenever a SPEC behavior is implemented, deferred, or intentionally made implementation-defined.
5. Every PR must run:
   - `gofmt`
   - `go test ./...`
   - targeted tests for the changed package
6. Prefer deterministic fake tests before real Linear/Codex integration tests.
7. Do not move the issue to `Human Review` unless the completion bar is satisfied.

## Status map

- `Backlog`: out of scope. Do not modify.
- `Todo`: move to `In Progress`, create or update `## Codex Workpad`, then execute.
- `In Progress`: continue implementation.
- `Human Review`: do not code. Wait for review.
- `Merging`: follow `.codex/skills/land/SKILL.md`; do not call `gh pr merge` directly.
- `Rework`: treat as a full approach reset.
- `Done`: terminal. Do nothing.

## Workpad protocol

Use exactly one persistent issue comment titled:

`## Codex Workpad`

Keep it updated in place. It must contain:

```text
<hostname>:<absolute-workspace-path>@<short-git-sha>
```

Sections:

- `### Plan`
- `### Acceptance Criteria`
- `### Validation`
- `### Notes`
- `### Confusions`

## Execution flow

1. Read the issue body and comments.
2. Reconcile the current workpad.
3. Write or update a hierarchical checklist plan.
4. Sync with latest `origin/main`.
5. Implement the smallest change that satisfies the acceptance criteria.
6. Run targeted validation.
7. Run `gofmt`.
8. Run `go test ./...`.
9. Commit logically.
10. Push the branch.
11. Create or update the PR.
12. Link the PR to the Linear issue.
13. Check PR comments, review summaries, inline comments, and CI.
14. Address all actionable feedback.
15. Move to `Human Review` only when the completion bar is satisfied.

## Completion bar before Human Review

Only move to `Human Review` when all are true:

- Acceptance criteria are complete.
- `docs/go-port/CONFORMANCE.md` is updated.
- `gofmt` has been run.
- `go test ./...` passes.
- Targeted package tests pass.
- PR is linked to the Linear issue.
- PR feedback sweep is complete.
- PR checks are green or documented as irrelevant with justification.
- Workpad is accurate and current.

## Blocked handling

If blocked:

1. Update `## Codex Workpad` with:
   - blocker
   - impact
   - what was tried
   - exact unblock action required
2. Do not mark the issue as complete.
3. Do not move to `Human Review` unless the completion bar is satisfied.
```

---

## 4. Go 实现架构

### 4.1 package 划分

| 组件 | Go package | 责任 |
|---|---|---|
| Workflow Loader | `internal/workflow` | 读取 `WORKFLOW.md`，拆 YAML front matter 和 prompt body |
| Config Layer | `internal/config` | 默认值、`$VAR` 展开、路径归一化、typed validation |
| Issue Tracker Client | `internal/tracker`, `internal/tracker/linear` | 拉候选 issue、刷新状态、归一化 Linear payload |
| Workspace Manager | `internal/workspace` | per-issue workspace、路径安全、生命周期 |
| Hooks | `internal/hooks` | `after_create`、`before_run`、`after_run`、`before_remove` |
| Orchestrator | `internal/orchestrator` | poll loop、dispatch、claimed/running、retry、reconcile |
| Agent Runner | `internal/agent` | workspace + prompt + Codex client |
| Codex Client | `internal/agent/codex` | `codex app-server` stdio/JSONL client |
| Linear Tool | `internal/tools/lineargraphql` | agent 侧 Linear GraphQL tool |
| State Store | `internal/state` | runs、sessions、retry queue、events |
| Observability | `internal/observability` | logs、metrics、status API |

### 4.2 Orchestrator 设计原则

采用单一状态所有者：

```go
type Orchestrator struct {
    cfg       *config.Config
    tracker   tracker.Client
    workspace workspace.Manager
    runner    agent.Runner
    state     RuntimeState
    events    chan Event
}
```

运行逻辑：

```text
startup
  ↓
load WORKFLOW.md
  ↓
validate config
  ↓
startup cleanup
  ↓
immediate tick
  ↓
repeat every polling.interval_ms
```

每个 tick：

```text
1. reconcile running issues
2. validate dispatch config
3. fetch candidate issues by project_slug + active_states
4. apply issue filters
5. apply blocker rules
6. sort by priority / created_at / identifier
7. dispatch while concurrency slots remain
8. update observability
```

### 4.3 Workspace 设计原则

每个 issue 一个 workspace：

```text
/workspaces/symphony-go/TOO-115/
/workspaces/symphony-go/TOO-116/
/workspaces/symphony-go/TOO-117/
```

必须保证：

```text
workspace_path 必须在 workspace_root 下
agent subprocess cwd 必须等于 workspace_path
workspace directory name 必须 sanitize
不同 issue 不共享工作目录
```

---

## 5. Go 重构任务拆分

不要建一个大 ticket 叫“Rewrite Symphony in Go”。拆成可以被 agent 执行、测试和 review 的小任务。

下表中的 `TicketId` 来自 Linear project `symphony-go`，初始状态均为 `Backlog`。

| TicketId | 主题 | 目标 | 验收 |
|---|---|---|---|
| `TOO-115` | Charter / Conformance | 创建 Go port charter 和 conformance matrix | `docs/go-port/CONFORMANCE.md` 完成，并对齐 `SPEC.md` Section 18 |
| `TOO-116` | CLI 骨架 | CLI 骨架、工作流路径选择与 validate 能力 | CLI 路径选择、help、validate 行为可测试 |
| `TOO-117` | 工作流加载 | `WORKFLOW.md` 加载器与 prompt body 解析 | front matter、非法 YAML、空 body、happy path 测试 |
| `TOO-118` | 工作流重载 | 工作流动态监听、重载与配置重应用 | 文件变更、非法 reload、future dispatch 行为测试 |
| `TOO-119` | 类型化配置 | 类型化配置层、默认值、环境变量解析与校验 | 默认值、env var、路径归一化、非法配置测试 |
| `TOO-120` | 工作区管理 | workspace 管理器、路径安全与 issue 隔离目录 | path containment、sanitize、existing/new workspace 测试 |
| `TOO-121` | 生命周期 hooks | 生命周期 hooks 与超时处理 | cwd、success/failure、timeout、`after_create` 测试 |
| `TOO-122` | Linear 只读适配 | Linear tracker 只读适配器 | fake GraphQL server 覆盖 candidate、refresh、terminal fetch |
| `TOO-123` | 调度策略 | issue 可执行性、阻塞关系、排序与 repo label routing | eligibility、blocker、排序、`issue_filter` 测试 |
| `TOO-124` | 编排循环 | orchestrator 轮询、调度与并发循环 | fake tracker + fake workspace/hooks/runner 调度测试 |
| `TOO-125` | 重试与状态校准 | retry queue、状态校准与终态 workspace 清理 | fake clock/tracker/workspace 覆盖 retry、stop、cleanup |
| `TOO-126` | Codex 客户端 | Codex app-server JSONL 客户端 | fake app-server binary 覆盖 success、malformed JSON、timeout |
| `TOO-127` | Agent runner | Agent runner 的 prompt 渲染、attempt、max turns 与超时 | strict prompt render、attempt、cwd、timeout 测试 |
| `TOO-128` | Linear GraphQL 工具 | 原始 `linear_graphql` agent 工具 | structured success/error payload 和 fake Linear 测试 |
| `TOO-129` | 可观测性 | 可观测性基线：结构化日志与状态快照 | `issue_id`、`issue_identifier`、`session_id` 等字段测试 |
| `TOO-130` | fake E2E | fake Linear + fake Codex 的 E2E 验证 profile | fake E2E happy path 和代表性失败路径完整跑通 |
| `TOO-131` | 真实 dogfood | 真实 Linear/Codex dogfood profile | 真实低风险 issue 在受控配置下走到 Human Review，或明确阻塞原因 |
| `TOO-132` | 持久化状态 | SQLite 持久化状态存储与崩溃恢复 | runs、retry、session/event 持久化与重启恢复测试 |
| `TOO-133` | 运维控制 | 运维控制、HTTP 端点、健康检查与 metrics | status、pause/resume/drain、cancel/retry、port precedence 测试 |
| `TOO-134` | 生产安全 | 生产安全：密钥脱敏、审计日志与成本护栏 | redaction、audit、limit enforcement 测试 |
| `TOO-135` | Linear 写操作 | Linear 评论与状态流转的类型化写 API | Workpad comment create/update、state transition、error case 测试 |
| `TOO-136` | Cutover | Go 替代 Elixir 的 cutover runbook 与准入 gate | cutover go/no-go、rollback、post-cutover monitoring 标准明确 |

---

## 6. 生产版必须补的能力

`SPEC.md` 是基础，不等于完整生产平台。生产版建议分三层。

### 6.1 L1：SPEC Conformance

必须完成：

```text
WORKFLOW.md loader
typed config
Linear tracker adapter
workspace manager
hooks
orchestrator poll/reconcile/retry
Codex app-server client
strict prompt rendering
structured logs
linear_graphql extension
fake E2E
real smoke test
```

### 6.2 L2：Production Baseline

生产可用的最低增强：

```text
durable state store
crash recovery
orphan workspace reconciliation
safe sandbox defaults
secret redaction
scoped credentials
workflow validation command
health/ready/metrics endpoints
pause/resume/drain/cancel/retry
per-project concurrency
per-issue timeout / max turns / max cost
audit log
typed Linear write tools
```

### 6.3 L3：Enterprise Hardening

多团队或公司内部平台再做：

```text
RBAC
approval queue
policy engine
containerized workers
workflow version governance
protected WORKFLOW.md enforcement
per-project budgets
secret scanning
backup / restore
incident runbooks
fleet manager
```

---

## 7. 状态持久化设计

生产版不要只靠内存。建议先用 SQLite，后续可切 Postgres。

核心表：

```sql
runs (
  id text primary key,
  instance_id text not null,
  issue_id text not null,
  issue_identifier text not null,
  status text not null,
  attempt integer not null,
  workspace_path text not null,
  codex_thread_id text,
  codex_turn_id text,
  workflow_sha256 text,
  claimed_by text,
  lease_expires_at timestamp,
  created_at timestamp,
  updated_at timestamp
);

retry_queue (
  id text primary key,
  run_id text not null,
  issue_id text not null,
  next_attempt_at timestamp not null,
  backoff_ms integer not null,
  reason text,
  created_at timestamp
);

agent_events (
  id text primary key,
  run_id text not null,
  event_type text not null,
  payload_json text,
  created_at timestamp
);
```

必须支持：

```text
服务重启后恢复 retry queue
识别 orphaned workspace
running lease 过期后可重新 claim
issue 进入 terminal state 后停止或清理
每次 dispatch 有 idempotency key
```

Go port implementation-defined choices for `TOO-132`:

| 项目 | 当前实现 |
|---|---|
| 配置入口 | `state_store.path` 为空时保持 in-memory 行为；配置后由 `internal/state` 打开并 migrate SQLite。 |
| SQLite driver | 使用 pure Go `modernc.org/sqlite`，避免把本地状态能力绑定到 cgo 环境。 |
| migration | `schema_migrations` 记录当前 schema version；missing DB 自动创建，corrupt DB 直接 startup fail。 |
| restart recovery | 本地单实例重启时，持久化 `running` rows 视为 interrupted crash artifacts，清 lease 后按 issue id upsert 一个 due retry。 |
| idempotency | active claim 由 `runs.status = running` + `issue_id` + lease 检查保护；retry queue 以 `issue_id` 为 primary key 防止重复恢复。 |
| event/session | orchestrator 继续以 structured logger 为 operator surface，同时 best-effort append `agent_events` 和 upsert latest `sessions` 供 crash inspection。 |

---

## 8. 安全默认值

生产默认不要使用高信任配置。

推荐：

```yaml
codex:
  command: codex app-server
  thread_sandbox: workspace-write
  turn_sandbox_policy:
    type: workspaceWrite
```

生产侧再补：

```text
dedicated OS user
独立 workspace root
scoped GitHub App token
scoped Linear token
secret manager 注入凭证
hook stdout/stderr secret redaction
禁止 literal secret 写入 WORKFLOW.md
不自动 merge 高风险 PR
```

避免默认：

```text
danger-full-access
approval_policy: never
shell_environment_policy.inherit=all
长期个人 PAT
agent 可读所有 repo token
```

---

## 9. 单实例 / 单 project 设计

### 9.1 v1 范围

v1 推荐定义为：

```text
一个 Symphony instance
  = 一个 WORKFLOW.md
  = 一个 Linear project_slug
  = 一个执行 repo
  = 一个 workspace root
  = 一个 state database
```

示例：

```bash
symphony run \
  --instance symphony-go \
  --workflow /repos/symphony-go/WORKFLOW.md
```

对应配置：

```yaml
instance:
  id: symphony-go

tracker:
  kind: linear
  project_slug: "symphony-go"

workspace:
  root: "/workspaces/symphony-go"

state_store:
  kind: sqlite
  path: "/var/lib/symphony/symphony-go/state.sqlite"
```

### 9.2 为什么先不做 single-instance multi-repo

不建议 v1 做：

```text
一个 instance
  → 多个 Linear projects
  → 多个 repos
  → 多套 WORKFLOW.md
```

原因：

```text
权限边界变大
workspace 语义复杂
issue routing 复杂
调度优先级复杂
跨 repo PR/CI/merge 复杂
故障隔离变差
```

---

## 10. Repo label routing 模式

### 10.1 适用场景

如果原项目是：

```text
一个 Linear Project 管两个 repo 的进度
```

例如：

```text
Linear Project: product-platform

Repo A: product-api
Repo B: product-web
```

推荐兼容方式：

```text
同一个 Linear Project
  ├── issue label repo:api → symphony-api instance
  └── issue label repo:web → symphony-web instance
```

这仍然符合“单实例负责单 project”。每个实例只看一个 `project_slug`，只是通过 label 再过滤属于自己的 issue。

### 10.2 Linear label 规则

建议固定这些 labels：

```text
repo:api
repo:web
cross-repo
needs-routing
security
migration
low-risk
```

执行规则：

| Issue labels | 行为 |
|---|---|
| 有且只有 `repo:api` | API instance 接手 |
| 有且只有 `repo:web` | Web instance 接手 |
| 没有 `repo:*` | 不 dispatch，进入 triage |
| 同时有多个 `repo:*` | 不 dispatch，要求拆 issue |
| 有 `cross-repo` | 不 dispatch，作为 parent / coordination issue |
| 有 `security` / `migration` | 更严格审批或人工 review |

核心规则：

```text
每个可执行 issue 必须刚好有一个 repo:* label。
```

### 10.3 API repo instance

```yaml
instance:
  id: product-api

tracker:
  kind: linear
  api_key: "$LINEAR_API_KEY"
  project_slug: "product-platform"

  active_states:
    - Todo
    - In Progress
    - Rework
    - Merging

  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate

  issue_filter:
    require_labels:
      - repo:api
    reject_labels:
      - repo:web
      - cross-repo
    require_exactly_one_label_prefix: "repo:"

workspace:
  root: "$SYMPHONY_API_WORKSPACE_ROOT"

hooks:
  after_create: |
    git clone "$API_REPO_URL" .
    go mod download
```

### 10.4 Web repo instance

```yaml
instance:
  id: product-web

tracker:
  kind: linear
  api_key: "$LINEAR_API_KEY"
  project_slug: "product-platform"

  active_states:
    - Todo
    - In Progress
    - Rework
    - Merging

  terminal_states:
    - Done
    - Closed
    - Canceled
    - Cancelled
    - Duplicate

  issue_filter:
    require_labels:
      - repo:web
    reject_labels:
      - repo:api
      - cross-repo
    require_exactly_one_label_prefix: "repo:"

workspace:
  root: "$SYMPHONY_WEB_WORKSPACE_ROOT"

hooks:
  after_create: |
    git clone "$WEB_REPO_URL" .
    npm ci
```

`issue_filter` 是建议加入 Go 版的扩展字段，不是官方 core schema。Go 版在 `tracker.issue_filter`
下解析该配置，并由 dispatch policy 输出 machine-readable reason。

### 10.5 Go 版 issue filter 结构

```go
type IssueFilterConfig struct {
    RequireLabels                []string `yaml:"require_labels"`
    RejectLabels                 []string `yaml:"reject_labels"`
    RequireAnyLabels             []string `yaml:"require_any_labels"`
    RequireExactlyOneLabelPrefix string   `yaml:"require_exactly_one_label_prefix"`
}
```

Eligibility：

```go
func EligibleByLabels(issue Issue, filter IssueFilterConfig) (bool, string) {
    labels := map[string]bool{}

    for _, label := range issue.Labels {
        labels[strings.ToLower(label)] = true
    }

    for _, label := range filter.RequireLabels {
        if !labels[strings.ToLower(label)] {
            return false, "missing_required_label"
        }
    }

    for _, label := range filter.RejectLabels {
        if labels[strings.ToLower(label)] {
            return false, "rejected_label_present"
        }
    }

    if len(filter.RequireAnyLabels) > 0 {
        matched := false
        for _, label := range filter.RequireAnyLabels {
            if labels[strings.ToLower(label)] {
                matched = true
                break
            }
        }
        if !matched {
            return false, "missing_any_required_label"
        }
    }

    if filter.RequireExactlyOneLabelPrefix != "" {
        prefix := strings.ToLower(filter.RequireExactlyOneLabelPrefix)
        count := 0

        for label := range labels {
            if strings.HasPrefix(label, prefix) {
                count++
            }
        }

        if count == 0 {
            return false, "missing_repo_routing_label"
        }

        if count > 1 {
            return false, "ambiguous_repo_routing_label"
        }
    }

    return true, ""
}
```

Dispatch pipeline：

```text
fetch candidate issues by project_slug + active_states
  ↓
normalize labels
  ↓
apply issue_filter
  ↓
apply blocker rules
  ↓
sort
  ↓
dispatch
```

---

## 11. 跨 repo issue 处理

不要让一个 agent 在一个 ticket 里同时改两个 repo。

推荐：

```text
Parent issue: Add billing usage dashboard
  label: cross-repo
  不被 Symphony 直接执行

Child issue 1: Add usage API endpoint
  label: repo:api
  由 API instance 执行

Child issue 2: Add dashboard frontend
  label: repo:web
  blocked_by: Child issue 1
  由 Web instance 执行

Child issue 3: Integration validation
  label: repo:web 或 repo:api
  blocked_by: Child issue 1 + Child issue 2
```

规则：

```text
cross-repo issue = 协调 issue
repo-specific child issue = 执行 issue
```

---

## 12. 如果外部 Elixir Symphony 暂时不支持 label filter

### 选择 A：临时拆 Linear project

```text
product-api project
product-web project
```

每个 instance 配自己的 `project_slug`。

优点：最稳。
缺点：牺牲统一 project 视图。

### 选择 B：临时按状态区分 repo

```text
API Todo
API In Progress
API Rework
API Merging

Web Todo
Web In Progress
Web Rework
Web Merging
```

API instance 只监听 API 状态。
Web instance 只监听 Web 状态。

缺点：Linear workflow 会膨胀。

### 选择 C：patch 外部 Symphony，加 label filter

推荐长期 bootstrap 方案：

```text
candidate fetch
  ↓
normalize labels
  ↓
filter require_labels / reject_labels
  ↓
dispatch
```

---

## 13. 生产版运维接口

建议 Go 版加入 CLI：

```bash
symphony run --workflow /path/to/WORKFLOW.md --instance product-api
symphony validate /path/to/WORKFLOW.md
symphony status
symphony pause
symphony resume
symphony drain
symphony cancel ISSUE-123
symphony retry ISSUE-123
symphony cleanup --terminal
symphony doctor
```

HTTP endpoints：

```http
GET  /healthz
GET  /readyz
GET  /metrics
GET  /runs
GET  /runs/{id}
POST /runs/{id}/cancel
POST /runs/{id}/retry
POST /orchestrator/pause
POST /orchestrator/resume
```

Metrics：

```text
symphony_runs_active{instance="product-api"}
symphony_runs_failed_total{instance="product-api"}
symphony_run_duration_seconds{instance="product-api"}
symphony_retry_count{instance="product-api"}
symphony_token_input_total{instance="product-api"}
symphony_token_output_total{instance="product-api"}
symphony_cost_usd_total{instance="product-api"}
symphony_tracker_api_errors_total{instance="product-api"}
```

---

## 14. 建议路线图

### Phase 1：Bootstrap

```text
外部 Elixir Symphony
  ↓
读取 symphony-go/WORKFLOW.md
  ↓
执行 Go port Linear tickets
```

目标：

```text
TOO-115 到 TOO-127 的基础与核心运行层 ticket（以第 5 节为准）
fake tests 为主
不要求 Go 版真实跑 workflow
```

### Phase 2：Go Conformance

```text
Go Symphony
  ↓
fake Linear
  ↓
fake Codex app-server
  ↓
fake E2E
```

目标：

```text
workflow loader
config
workspace
hooks
tracker
orchestrator
agent runner
Codex client
linear_graphql
```

### Phase 3：Go Dogfood

```text
Go Symphony
  ↓
真实 Linear project
  ↓
真实 Codex
  ↓
低风险 issue
```

限制：

```yaml
agent:
  max_concurrent_agents: 1

tracker:
  active_states:
    - Todo
    - In Progress
    - Rework
```

先不开 `Merging`。

### Phase 4：Production Baseline

补：

```text
state store
crash recovery
metrics
audit log
safe sandbox profile
pause/resume/drain/cancel/retry
workflow validation
cost limits
typed Linear tools
```

### Phase 5：Fleet，但不是 single-instance multi-repo

当多个 repo 使用时：

```text
symphony@product-api
symphony@product-web
symphony@admin
```

可加一个 fleet manager：

```text
symphony-fleet
  ├── 管理多个 instance
  ├── 聚合 metrics
  ├── 聚合 health
  └── 提供统一 dashboard
```

但每个 instance 仍然保持：

```text
一个 project_slug
一个 repo
一个 workspace root
一个 state database
```

---

## 15. 最终设计原则

```text
1. SPEC.md 是实现契约。
2. WORKFLOW.md 是 repo-owned operating procedure。
3. 外部 Symphony 只作为 bootstrap runner。
4. Go repo 不保留 elixir/。
5. v1 不做 single-instance multi-repo。
6. v1 做 single-instance single-project single-repo。
7. 一个 Linear project 管多个 repo 时，用 repo label routing + 多实例。
8. cross-repo issue 不直接执行，拆 repo-specific child issues。
9. 生产版必须补 durable state、安全边界、观测、运维控制和审计。
10. 先 fake E2E，再 real dogfood，再 cutover。
```

最简架构句：

```text
每个 Symphony instance 只负责一个 Linear project 中、符合自己 repo label 的 issue；
每个 issue 一个 workspace；
每个 repo 一份 WORKFLOW.md；
跨 repo 工作拆成 parent coordination issue + repo-specific child issues。
```
