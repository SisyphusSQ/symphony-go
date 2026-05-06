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
15. [TOO-131] 新增 opt-in 真实 Linear/Codex dogfood profile，提供默认显式 skipped 的验证入口、受控 workflow 默认值、runbook、conformance 结果和 unblock steps。
16. [TOO-132] 新增 SQLite-backed durable state store，覆盖 runs、sessions、retry queue、agent events、startup crash recovery、claim/lease 与 optional `state_store` config。
17. [TOO-133] 新增单实例 operator controls 与本地 HTTP status surface，覆盖 pause/resume/drain、cancel/retry、health/ready/runs/metrics endpoints、`--port` precedence 和 loopback bind 默认值。
18. [TOO-134] 新增 production baseline safety controls，覆盖 safe Codex sandbox defaults、secret redaction、redacted audit events、tool-call audit 和 per-issue runtime/token/estimated-cost guardrails。
19. [TOO-135] 新增 typed Linear write APIs，覆盖 issue comment create/update、workpad upsert、state transition、URL attachment 和 fake GraphQL 测试。
20. 新增 Go CLI production runtime wiring，`symphony run` 会从 workflow 装配 Linear tracker、workspace manager、lifecycle hooks 和 Codex runner 后进入真实 dispatch loop。
21. 修正默认 dogfood workspace clone hook，避免 Symphony metadata 使 `git clone "$SOURCE_REPO_URL" .` 在新 workspace 中失败，并支持 retry 时补齐缺失 checkout。
22. [TOO-139] 新增稳定 `/api/v1` operator state/runs 只读契约，覆盖 state summary、run list/query、run detail、统一 error envelope 与无 durable state store 兼容路径。
23. [TOO-140] 新增脱敏 run event timeline API 与 issue latest-run lookup，覆盖 SQLite event projection、分页、category filter、redaction 和 no-event 语义。

#### note:
1. 新增 symphony-go 版本发布 repo-local skill，明确流程完成前必须回写 changeLog。
2. 切换 Go module 到 github.com/SisyphusSQ/symphony-go，并初始化 GitHub 仓库远端。
3. [TOO-115] 新增 Go port conformance charter 与 SPEC 对齐矩阵，记录必选能力、推荐扩展、验证入口和延期决策。
4. [TOO-136] 新增 Go cutover runbook 与 replacement gate，明确 NO-GO 准入、rollback、post-cutover monitoring 和 residual-risk 处理。
5. 新增 Go self-dogfood 详细测试方案，明确外部 runner 与 Go binary 证明边界、本地敏感输入文件、真实预检、完整调度、恢复回滚和脱敏证据要求。
6. [TOO-138] 完成一次 Go binary real self-dogfood smoke，记录 4002 operator endpoint、workspace/state DB、Codex invocation、Linear workpad/state writeback 与 residual risk。
