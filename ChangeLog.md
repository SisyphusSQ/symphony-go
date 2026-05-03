## Unreleased

#### feature:
1. [TOO-116] 完善 CLI workflow 路径校验，支持 validate 与 run preflight 识别 positional、--workflow 和默认 ./WORKFLOW.md 路径。
2. [TOO-119] 新增 typed runtime config layer，覆盖 SPEC 默认值、环境变量解析、workspace 路径归一化和启动前校验。
3. [TOO-118] 新增 WORKFLOW.md 动态 reload 能力，支持内容变化检测、last-known-good fallback 与后续 dispatch config/prompt 重应用。
4. [TOO-120] 新增安全的 per-issue workspace manager，支持 identifier sanitize、root containment、目录创建/复用 metadata 与 fail-safe 冲突处理。
5. [TOO-121] 新增 lifecycle hook runner，支持 workspace cwd 执行、timeout、输出捕获、失败结果返回和 create-only `after_create`。
6. [TOO-122] 新增 Linear tracker 只读适配器，覆盖候选 issue、状态刷新、终态拉取、分页错误和 labels/blockers 归一化的 fake GraphQL 测试。
7. [TOO-123] 新增 issue dispatch policy 与 `tracker.issue_filter` typed extension，覆盖 eligibility、blocker、排序和 repo label routing reason。
8. [TOO-124] 新增 orchestrator polling 与 dispatch loop，支持 immediate/interval tick、runtime-owned state、workspace + hook + runner pipeline、global/per-state concurrency 和 CLI run startup wiring。
9. [TOO-125] 新增 orchestrator retry queue、active-run reconciliation 与 terminal workspace cleanup，支持 normal exit continuation retry、exponential backoff cap、non-active release 和 `before_remove` cleanup hook。
10. [TOO-126] 新增 Codex app-server JSONL client 与 runner adapter，覆盖 subprocess cwd/env、initialize/thread/turn protocol、token usage、timeouts、process error 和 unsupported server request fake 测试。
11. [TOO-127] 新增 agent runner strict prompt rendering、attempt/max turns orchestration、Codex timeout/cwd wiring 与 fake client metadata 测试。
12. [TOO-128] 新增 raw `linear_graphql` agent tool，复用 Linear tracker endpoint/auth，覆盖 structured errors、GraphQL error failure 和 Codex dynamic tool wiring fake 测试。
13. [TOO-129] 新增 observability baseline，提供共享结构化事件、JSON/recorder logger、runtime status snapshot，并接入 orchestrator run/retry/failure 事件。
14. [TOO-130] 新增 fake Linear + fake Codex E2E profile，覆盖 workflow load、dispatch、workspace/hooks、runner、retry/status、terminal cleanup 和 observability 的无外部服务验证。

#### note:
1. 新增 symphony-go 版本发布 repo-local skill，明确流程完成前必须回写 changeLog。
2. 切换 Go module 到 github.com/SisyphusSQ/symphony-go，并初始化 GitHub 仓库远端。
3. [TOO-115] 新增 Go port conformance charter 与 SPEC 对齐矩阵，记录必选能力、推荐扩展、验证入口和延期决策。
