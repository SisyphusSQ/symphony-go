---
name: symphony-go-version-release
description: 当在 symphony-go 仓库处理版本、发布收口、ChangeLog/changeLog、release notes、CLI build 验证或 Symphony workflow 发布准备时使用。流程完成前必须回写 changeLog。
---

# symphony-go 版本发布

用于 `symphony-go` 的版本、发布准备和 ChangeLog 收口。

核心规则：issue 是执行粒度，release 是发布粒度。不要因为一个 Linear issue 完成就自动发布版本；但任何 release / closeout 流程完成前，必须回写 `ChangeLog.md` 或 `changeLog.md`。

## 第一步

1. 确认当前目录是 `symphony-go` 仓库根目录。
2. 任何仓库变更前先读取根级 `AGENTS.md`。
3. 决定更新什么之前，先用 dry-run 跑检查：

```bash
python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py check \
  --repo . --json
```

## 先分类再变更

写文件前先判断本次变更属于哪一类：

| 分类 | 含义 |
| --- | --- |
| `issue-only` | 只更新 Linear / Workpad / run summary，不发布版本。完成前仍要确认 changeLog 是否需要记录。 |
| `changelog-only` | 只追加到 `ChangeLog.md` 或 `changeLog.md` 的 `Unreleased`。 |
| `workflow-policy` | harness、prompt、workflow、docs/symphony 运行口径变化，需要记录发布/运维影响。 |
| `runtime-build` | CLI/runtime/build 行为变化，进入 release 前必须跑构建与 CLI help 验证。 |
| `release-docs` | README、设计文档或 release notes 变化，不一定发布 artifact，但必须写 changeLog。 |

使用：

```bash
python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py classify \
  --repo . \
  --changed-files <文件路径...> --json
```

## 写入规则

- 脚本默认 dry-run。
- 只有目标动作明确后才添加 `--write`。
- 脚本不得执行 `git push`、操作 Linear、修改 tag、上传 release artifact。
- `ChangeLog.md` / `changeLog.md` 使用 `Unreleased + release archive` 模式；issue 结果先写入 `Unreleased`，只有真实 release 时才归档。
- 流程完成前必须回写 changeLog；如果仓库缺少 ChangeLog 文件，先创建或明确补齐目标文件，不能只在 Linear / PR 里说明。
- 收口摘要必须写明 `changelog_action` 和 `changelog_version`：例如 `updated Unreleased`、`archived v0.1.0`、`not-applicable with reason`。

## 常用命令

追加 Unreleased 条目：

```bash
python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py changelog-add \
  --repo . \
  --issue <ISSUE-ID> --type note --text "<简洁变更说明>" --write --json
```

检查当前变更是否包含 changeLog 回写：

```bash
git diff --name-only origin/main...HEAD \
  | python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py changelog-gate \
      --repo . --changed-files-from - --json
```

真实 release 时归档：

```bash
python3 .agent/skills/symphony-go-version-release/scripts/symphony_go_version_release.py release-archive \
  --repo . \
  --version v0.1.0 --date 20260502 --write --json
```

## 详细策略

当任务涉及版本边界、release archive、workflow 发布口径或 changeLog 分类时，读取 [references/symphony-go-version-policy.md](references/symphony-go-version-policy.md)。
