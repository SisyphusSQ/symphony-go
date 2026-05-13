Mode: full

# 交互式 Loop Prompt Contract

| 项目 | 内容 |
| --- | --- |
| 文档定位 | 主对话 / 交互式 harness loop 的短 contract |
| 适用范围 | 当前会话里围绕单张 issue 做 collect / gate / freeze / implement / closeout |
| 关联文档 | `AGENTS.md`、`docs/harness/control-plane.md`、`docs/harness/linear.md`、`.agents/prompts/issue-standard-workflow.md`、`.agents/prompts/loop-automation.md`、`.agents/PLANS.md` |

固定规则：

- 本文只负责交互式主对话，不替代 automation contract。
- 需要无人值守 / 定时运行时，应改读 `loop-automation.md`。
- 若当前 loop 需要详细 issue 级模板，优先跳到 `issue-standard-workflow.md`。
- 当前状态、`recovery_point`、`next_action` 默认落 Linear，而不是本地 `state / runs`。
- 复杂任务的计划默认落当前 issue 关联的 Linear Doc，本地 plan 文件只作为 ignored gate cache。
- Symphony 默认不使用上层控制器模型；每张 active issue 都应独立闭环。

## 1. 术语

| 术语 | 定义 |
| --- | --- |
| `issue` | Symphony 的唯一默认执行单元 |
| `single-issue-loop` | 围绕单张 Linear issue 推进一轮 |
| `plan-only loop` | 只做分析、冻结和计划，不进入实现 |
| `follow-up issue` | 从当前 issue 拆出的独立后续工作，不由当前 loop 自动执行 |

## 2. 固定约束

1. 交互式 loop 先探索、再冻结、后实现。
2. 当前 issue 若仍过大，先回到 plan-only，不继续编码。
3. `verify / review / pr_prep` 仍然是独立 checkpoint。
4. 交互式 loop 的结果面最少要说明：当前范围、当前 stop_scope、下一步动作。
5. 交互式 loop 的运行反馈默认回写到 Linear。
6. 多 issue 组织通过 Milestone / Label / blocker 表达，不在 prompt 中模拟自动连续消费。

## 3. 常用 Prompt 模板

### 3.1 直启单张 issue

```text
执行 <ISSUE-ID>。
先判断当前卡是适合直接进入 single-issue-loop，还是应先降级为 plan-only loop。
若信息不足，优先先做范围分析和计划准备，不直接扩大范围。
```

### 3.2 围绕 issue 做 plan-only loop

```text
围绕 <ISSUE-ID> 执行 plan-only loop。
本轮目标不是开发，而是输出：
1. 当前范围分析
2. 已确认事实
3. 待确认项
4. 拆卡建议
5. 推荐的下一步执行顺序
```

### 3.3 当前 issue 过大时拆 follow-up

```text
围绕 <ISSUE-ID> 做范围收敛。
先判断哪些内容属于当前 issue 的独立闭环范围；
若剩余内容需要拆出 follow-up issue，只输出拆分建议或按授权创建 follow-up，
不要在当前 loop 中继续扩张实现范围。
```

### 3.4 当前 issue 完成收口

```text
围绕 <ISSUE-ID> 做最终收口检查。
先核对 Goal、Acceptance Matrix、验证摘要、review 结果与文档同步状态；
若已满足闭环条件，再给出 Done 建议；
若未满足，明确 blocker 与下一步动作。
```

## 4. 使用建议

- issue 级高频 Prompt 直接去 `issue-standard-workflow.md`
- 自动推进、无人值守、run id 等语义去 `loop-automation.md`
- 当前文件适合主对话里快速进入 loop，而不是承载完整 issue 模板全集
