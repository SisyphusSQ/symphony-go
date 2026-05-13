# symphony-go 版本策略

## 真相边界

`symphony-go` 的 release 相关真相分为四层：

| 面 | 负责人 | 变化时机 |
| --- | --- | --- |
| Issue log | Linear issue Doc、Workpad、changeLog `Unreleased` | 每个完成的 issue 都可以追加记录。 |
| Runtime workflow | `docs/symphony/WORKFLOW.md`、`docs/symphony/symphony.md` | 本机运行口径或自动化 prompt 变化时。 |
| Harness contract | `docs/harness/*`、`.agents/PLANS.md`、`.agents/prompts/*` | 控制面协议、gate、prompt 或 review 口径变化时。 |
| Release artifact | git tag、构建产物、release notes | 只有真实发布 artifact 时变化。 |

不要把 issue 完成、workflow 口径变化和 release artifact 发布当成同一件事。

## ChangeLog 策略

流程完成前必须回写 `ChangeLog.md` 或 `changeLog.md`。

优先级：

1. 仓库已有 `ChangeLog.md` 或 `changeLog.md` 时沿用现有文件。
2. 两者都不存在时，默认创建 `ChangeLog.md`。
3. 不得只把变更写在 Linear、PR 或本地 run summary 里。

顶部应有 `Unreleased` 段：

```markdown
## Unreleased
#### note:
1. [SYM-1] 调整 issue 级 Execution Plan 落点为 Linear issue Doc。
```

只有真实 release 收口时，才把 `Unreleased` 归档成 release 段：

```markdown
### v0.1.0(20260502)
#### note:
1. [SYM-1] 调整 issue 级 Execution Plan 落点为 Linear issue Doc。
```

## 分类顺序

changeLog 条目分类沿用：

1. `feature`
2. `optimization`
3. `bugFix`
4. `note`
5. `script`

docs / harness / workflow 维护通常写入 `note`。脚本或 Makefile 维护可写入 `script`。用户可明确指定其它分类。

## 收口要求

任何 release / closeout 任务在完成前必须报告：

- `changelog_action`
- `changelog_version`
- `verification_summary`
- `residual_risks`

若确实不更新 changeLog，必须写出明确原因；但 release / workflow / harness / runtime 口径变化不得标为 `not-applicable`。

## 自动化护栏

- `check` 和 `classify` 始终是安全 dry-run 命令。
- `changelog-gate` 用于 PR / closeout 前检查当前变更是否包含 `ChangeLog.md` / `changeLog.md`，缺失时应阻塞收口。
- `changelog-add`、`release-archive` 只有带 `--write` 时才改文件。
- 脚本不执行 git、Linear、tag、artifact upload。
- 写入后至少运行 `make harness-check`；触及 Go runtime 时再运行 `make test`、`make build` 或 `make verify`。
