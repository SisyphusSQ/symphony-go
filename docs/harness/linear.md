# Linear

## Symphony Issue 标准执行工作流

推荐顺序：

1. 需求不清楚时先写 Requirement Clarification
2. 需要总体分组时使用 Linear Project / Milestone / Label
3. 需要排队和依赖时使用 Linear blocker / related issue
4. 每张进入 Symphony active state 的 issue 都必须是可独立执行、验证和收口的工作单元
5. 进入仓库实施前补 Codex Handoff
6. 关键阶段用单个进展 comment 回写运行反馈
7. 收口前按单 issue Done Gate 检查

当前默认 issue 前缀示例：

- ``

## 真相 Contract

固定规则：

- `Linear 是主协作真相`
- `repo 是主执行真相`
- `PR / MR 是次级代码叙事面`

默认解释：

- 当前任务范围、状态、blockers、follow-up、运行反馈、`recovery_point`、`next_action` 默认以 Linear 为准
- 当前复杂任务的 `Execution Plan` 默认以 Linear issue-scoped Doc 为准
- 当前执行命令、代码路径、设计文档入口、Prompt / Guide、write scope 默认以 repo 为准
- PR / MR 只在仓库启用时承接代码叙事，不作为唯一任务真相面

## Issue Doc Execution Plan Contract

固定规则：

- 每张复杂 issue 默认维护一个 canonical `Execution Plan` Doc，绑定到当前 issue。
- Doc 标题格式：`Execution Plan: <ISSUE-ID> - <issue title>`。
- issue body 不承载完整计划，只保留必要背景、验收摘要和 Doc 链接 / 引用。
- `## Symphony Workpad` 不作为计划正文，只记录运行状态、`plan_ref`、`local_plan_cache`、验证摘要和恢复点。
- 需要运行 `make harness-review-gate` 时，agent 可把 Linear Doc 内容导出到 `.agent/plans/<issue>.md` 或其它 ignored 本地路径。
- 本地导出 cache 必须从 Doc 同步而来；Linear Doc 与 cache 冲突时，以 Linear Doc 为准。

## Symphony 单 Issue 模型

| 层级 | 作用 |
| --- | --- |
| `Linear Project` | 承载 Symphony 轮询队列和项目级可见性 |
| `Milestone / Label` | 可选分组、阶段、主题或 repo 路由 |
| `Issue` | Symphony 的唯一默认执行单元 |
| `Blocker / related issue` | 表达执行顺序和依赖，不模拟上层控制器 |

固定规则：

- 不默认使用上层控制器或子卡层级。
- 每张 issue 必须包含足够的 Goal、Included、Excluded、Acceptance Matrix 和 Verification Commands。
- 当前 issue 达到本轮验收后立即停止，不顺手扩展范围。
- 大任务先拆成多张独立 issue；Symphony 只消费当前 active 且未被 blocker 阻塞的 issue。
- Milestone / Label 只负责组织和筛选，不承接自动连续消费语义。

## 必填任务真相字段

### 每张 Symphony issue 至少要清楚这些字段

- `Goal`
- `Included`
- `Excluded`
- `Acceptance Matrix`
- `Completion Gate`
- `Write Scope Limit`
- `Verification Commands`
- `Rollback Unit`
- `Dependencies / Blockers`
- `Follow-up Candidates`

## Requirement Clarification 模板

### 背景

- 当前问题：
- 当前上下文：
- 相关系统 / 仓库：

### 目标

- 业务目标：
- 成功标准：

### 非目标

- 本次明确不做：

### 约束

- 技术约束：
- 时间约束：
- 协作约束：

### 风险

- 已知风险：
- 依赖项：

### 待确认

- [待确认]

## Project / Milestone 模板

### 项目目标

- 该分组要解决什么问题：
- 当前阶段：

### 范围

- In Scope：
- Out of Scope：

### 成功标准

- 里程碑 1：
- 里程碑 2：

### 风险与依赖

- 关键风险：
- 外部依赖：

### 协作方式

- 文档真相：
- 仓库真相：
- Linear 归口：

## Symphony Issue 模板

### 标题

`<slice-topic>`

### Goal

- 一句话说明本卡交付什么：

### Included

- 仅写当前独立可验证工作单元：

### Excluded

- 显式排除顺手扩展内容：

### Acceptance Matrix

| 类别 | 口径 |
| --- | --- |
| 构建 |  |
| 测试 |  |
| review |  |
| writeback |  |

### Completion Gate

- 达到以下状态后必须立即停止：

### Write Scope Limit

- 主写入范围：
- 辅助文件范围：

### Reference Targets

- 待补充

### Writeback Targets

- 待补充

### Verification Commands

- 待补充

### Rollback Unit

- 待补充

### Dependencies / Blockers

- 待补充

### Follow-up Candidates

- 待补充

### Expected PR Narrative

- 未来 PR / MR 应如何讲述本卡边界：

## 运行反馈 Comment Contract

默认回写面：

- 当前 issue comment
- 必要时同步到关联 issue 或 milestone/project update

最小字段：

- `current_phase`
- `result`
- `plan_ref`
- `local_plan_cache`
- `verification_summary`
- `review_summary`
- `writeback_summary`
- `residual_risks`
- `recovery_point`
- `next_action`

固定规则：

- `运行反馈` 默认写回 Linear comment / issue body
- 不启用本地 `state / runs` 时，也必须能在 Linear 上恢复当前状态
- 复杂任务的 `plan_ref` 必须指向 Linear issue Doc；`local_plan_cache` 只记录本地 gate cache
- 单 issue 场景必须显式写出当前 `current_phase`、`Completion Gate` 是否满足和下一步动作

## 结果回写 Contract

### 当前 issue 完成时

- 回写本轮 `result`
- 回写 `verification_summary`
- 回写 `review_summary`
- 回写 `writeback_summary`
- 回写 `residual_risks`
- 回写下一步 `next_action`

### 当前 issue 未完成时

- 明确 `current_phase`
- 明确 `stop_scope`
- 明确当前 blocker 或待补信息
- 明确当前 `recovery_point`

### 当前 issue 可 Done 时

- 明确最终结果
- 明确完成的验收项
- 明确残余风险
- 明确 follow-ups
- 明确最终建议状态

## Codex Handoff 模板

### Repo Context

- 仓库：
- 当前分支：
- 相关 issue：

### Goal

- 本轮要交付什么：

### Frozen Scope

- 纳入：
- 不纳入：

### Key Paths

- 关键文件：
- 关键目录：
- 关键文档：

### Commands

- 验证命令：
- review gate：

### Expected Result

- 完成后应回写到哪里：
- 是否需要 PR / MR：

## 评论模板

### Issue 启动评论

```md
## Symphony Workpad

- `current_phase`: collect
- `repo`: <repo path>
- `plan_ref`: <Linear Doc title/id or none>
- `local_plan_cache`: <ignored local cache path or none>
- `scope`: <short included/excluded summary>
- `verification`: pending
- `review`: pending
- `recovery_point`: <how to resume>
- `next_action`: <next action>
```

### Issue 收口评论

```md
## Symphony Workpad

- `current_phase`: writeback
- `result`: <done / blocked / needs-review>
- `plan_ref`: <Linear Doc title/id or none>
- `local_plan_cache`: <ignored local cache path or none>
- `verification_summary`: <commands and result>
- `review_summary`: <blocking findings or none>
- `writeback_summary`: <where the facts were written>
- `residual_risks`: <remaining risk or none>
- `recovery_point`: <commit/branch/Linear Doc/workpad pointer>
- `next_action`: <human review / merge / follow-up / none>
```

## 单 Issue Done Gate

只有同时满足下面条件，才建议把当前 issue 置为 `Done`：

1. 当前 issue 的 `Goal` 与 `Acceptance Matrix` 已满足
2. `Verification Commands` 已执行并回写摘要
3. review gate 没有 blocking findings
4. 必要 PR / MR 已完成 merge 或无需 PR / MR
5. 结果、残余风险、follow-up、`recovery_point` 和 `next_action` 已写回 Linear

禁止提前置 Done 的情况：

- 当前只是完成了计划、诊断或部分实现
- 验证未执行或失败
- review 仍有 blocking findings
- 仍有关键 blocker 或待补信息属于当前 issue 范围
