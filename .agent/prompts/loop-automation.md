Mode: full

# Automation Loop Prompt

| 项目 | 内容 |
| --- | --- |
| 文档定位 | 无人值守 / 自动化 loop 的规范页与 Prompt 模板 |
| 适用范围 | Symphony 围绕一张 Linear issue 自动推进一轮受控 harness loop |
| 关联文档 | `.agent/prompts/issue-standard-workflow.md`、`.agent/prompts/loop-codex.md`、`AGENTS.md`、`docs/harness/control-plane.md`、`docs/harness/linear.md`、`.agent/guides/code-review.md`、`.agent/PLANS.md` |

固定规则：

- 本文用于 automation，不替代仓库级控制面真相。
- 若本文与 `AGENTS.md`、`docs/harness/*`、`.agent/PLANS.md` 冲突，以后者为准。
- 当前 automation 语义优先围绕单张 Linear issue 设计。
- automation run 的结果面优先是 Linear，而不是本地 `state / runs`。
- 复杂任务的 `Execution Plan` 优先创建或更新当前 issue 关联的 Linear Doc。
- `.agent/plans/<issue>.md` 仅作为 ignored 本地 gate cache，不作为协作真相。
- 不默认模拟上层控制器；大任务通过 Milestone / Label / blocker 管理，Symphony 每次只消费当前可执行 issue。

## 0.1 Optional Superpowers Skill Hooks

- automation 仍按本文的单 issue、mode 与结果面执行。
- `superpowers:subagent-driven-development` 只在任务相互独立、subagent 可用且 write scope 不冲突时考虑；否则使用当前 inline loop。
- `superpowers:executing-plans` 可用于按已冻结 plan 串行执行。
- `superpowers:verification-before-completion` 可用于任何完成、通过或可收口声明前。
- verify 失败或异常排查时，可考虑 `superpowers:systematic-debugging`，先定位根因再修。
- skill 输出必须回填到 `verification_summary`、`review_summary`、`writeback_summary`、`residual_risks`、`recovery_point`。

## 1. 单 Issue 入口

| 输入 | 作用 | 默认 loop |
| --- | --- | --- |
| `issue` | 当前 Linear issue，是唯一默认执行单元 | `single-issue-loop` |
| `mode` | 控制副作用范围 | `implement-no-merge` 或用户指定模式 |

固定规则：

1. 当前 issue 若仍过大，必须先回 `freeze` 收窄或拆成独立 follow-up issue；拆清前不继续编码。
2. 当前 issue 有未完成 blocker 时不应执行实现。
3. 当前 issue 完成后只收口当前 issue，不自动继续下一张 issue。

## 2. 执行模式

| 模式 | 行为 |
| --- | --- |
| `propose-only` | 只做分析、冻结与下一步建议，不建卡、不实现 |
| `issue-create` | 创建或更新拆分出的独立 follow-up issue，但不开始实现 |
| `implement-no-merge` | 推进到 verify / review / writeback / PR ready，但不进入 merge |
| `full-auto` | 围绕当前 issue 自动执行到允许的最远 checkpoint；不会自动消费其它 issue |

## 3. Checkpoint 与停止规则

固定主流程：

`collect -> gate -> freeze -> slice -> implement -> verify -> review -> writeback -> pr_prep -> merge -> notify`

固定规则：

1. `slice` 表示当前 issue 内的执行边界，不表示 child issue。
2. `verify / review / pr_prep / merge / escalation` 都是独立 checkpoint。
3. 当前 issue 若仍过大，必须先回 `freeze` 收窄或拆卡。
4. review 出现 blocking finding 时，先修正，再重新执行 `verify -> review`。
5. 若自动 merge 条件不满足，应明确降级到 manual gate，而不是假装已自动收口。

## 4. 结果面

automation 至少要同步以下结果面：

- 当前执行结果 `result`
- 当前 stop_scope
- 当前 verification / review 摘要
- 当前 writeback 摘要
- 当前 residual_risks
- followups / next_action
- 当前 `recovery_point`

默认回写面：

- 优先写回 Linear
- 若仓库启用了 PR / MR，再同步代码叙事面
- 必要时再同步 repo 文档

## 5. 标准 Automation Prompt 模板

```text
你是当前仓库的无人值守 loop agent。你的职责是围绕当前 Linear issue，
用仓库内 harness 真相推进一轮受控 automation loop，并在不能安全自动化时明确降级。

运行参数：
- Issue: <ISSUE>
- Run ID: <RUN_ID>
- Mode: <MODE>

你必须优先读取以下真相源：
- 根规则：AGENTS.md
- 工程控制面：docs/harness/control-plane.md、docs/harness/linear.md
- 计划协议：.agent/PLANS.md、.agent/plans/TEMPLATE.md、.agent/plans/EXAMPLE-implementation.md
- 计划主载体：当前 Linear issue 关联的 Execution Plan Doc
- Prompt 合同：.agent/prompts/*
- Review / Lint 说明：.agent/guides/*

固定主流程：
collect -> gate -> freeze -> slice -> implement -> verify -> review -> writeback -> pr_prep -> merge -> notify

执行硬约束：
1. 当前 Linear issue 是唯一默认执行单元。
2. slice 表示当前 issue 内的执行边界，不创建隐含 child issue。
3. 若当前 issue 仍过大，必须先回 freeze 收窄或拆成 follow-up issue。
4. verify / review / pr_prep / merge / escalation 都是独立 checkpoint。
5. 结果面至少同步当前 result、stop_scope、plan_ref、local_plan_cache、verification_summary、review_summary、writeback_summary、residual_risks、followups、recovery_point。
6. `merge` / `escalation` 结论默认由 agent 给出；无法安全自动化时，明确降级为 manual gate 或 plan-only，不要伪装成已闭环。
```

## 6. 使用建议

- 初次验证 automation prompt 时，先用 `propose-only`
- 需要验证实现主流程但不想自动收尾时，用 `implement-no-merge`
- 只有在当前 issue 范围、验证和 merge 条件都清楚时，才对当前 issue 使用 `full-auto`
