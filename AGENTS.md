# symphony-go AGENTS

## 项目定位

本文件是 `symphony-go` 的根级协作入口。`README.md` 只承载产品功能与使用说明；协作规则、控制面导航、truth split、提交边界和初始化后检查都收口在这里。

1. 说明仓库当前阶段
2. 给出控制面文档导航
3. 定义 `docs/harness/` 与 `.agents/` 的边界
4. 明确哪些内容默认不提交

## 当前阶段

- 当前仓库已完成 harness 控制面初始化。
- 控制面文档统一收口到 `docs/harness/`。
- `.agents/` 是当前公开计划、prompt、guide、state/runs 模板和 repo-local skill 的目录；旧 `.agent/` 路径只作为历史迁移来源理解。
- base harness 默认提供 `check + review_gate`。
- `.agents/state/` 与 `.agents/runs/` 默认作为本地辅助运行面存在。
- 若通过 agent 驱动初始化或扩展，默认再补齐 `.agents/prompts/` 与 `.agents/guides/`，并保持这些目录的协作规则在本文件中可追踪。

## 快速导航

| 主题 | 入口 |
| --- | --- |
| 产品说明与使用方法 | `README.md` |
| 主流程、gate、计划 contract | `docs/harness/control-plane.md` |
| Linear 工作流与模板 | `docs/harness/linear.md` |
| 项目级机械约束登记 | `docs/harness/project-constraints.md` |
| 通用测试 runbook 模板 | `docs/test/RUNBOOK_TEMPLATE.md` |
| 计划协议 | `.agents/PLANS.md` |
| 计划主模板 | `.agents/plans/TEMPLATE.md` |
| 实现型示例 | `.agents/plans/EXAMPLE-implementation.md` |
| 本地恢复面 | `.agents/state/TEMPLATE.md` |
| 本地结果面 | `.agents/runs/TEMPLATE.md` |
| 可选 Prompt 层 | `.agents/prompts/README.md`（如存在） |
| 可选维护循环 Prompt | `.agents/prompts/maintenance-loop.md`（如存在，默认 `report-only`） |
| 可选 Guide 层 | `.agents/guides/`（如存在） |

## 推荐阅读顺序

1. `AGENTS.md`
2. `docs/harness/control-plane.md`
3. `docs/harness/linear.md`
4. `docs/harness/project-constraints.md`
5. `docs/test/RUNBOOK_TEMPLATE.md`
6. `.agents/PLANS.md`
7. `.agents/plans/TEMPLATE.md`
8. `.agents/plans/EXAMPLE-implementation.md`
9. 若存在，再读 `.agents/prompts/README.md`
10. 若存在，再读 `.agents/prompts/maintenance-loop.md`
11. 面向运行和使用时，再读 `README.md`

## 真相边界

| 路径 | 负责内容 |
| --- | --- |
| `README.md` | 产品功能、运行方法、配置入口和用户可见能力说明 |
| `docs/harness/` | 控制面规则、Linear 模板与项目级机械约束登记 |
| `docs/test/` | 通用测试 runbook 模板、可复用验证 runbook 与脱敏后的当前 / 本次验证结果摘要 |
| `.agents/PLANS.md` + `.agents/plans/` | 计划协议、公开计划模板和实现型示例 |
| `.agents/state/` + `.agents/runs/` | repo-local 恢复点与结果摘要面 |
| `.agents/prompts/` | 可选 Prompt 模板，仅 agent 驱动初始化时补充 |
| `.agents/guides/` | 可选 review / linter 说明，仅 agent 驱动初始化时补充 |
| `.agents/skills/` | 可选 repo-local skill，仅在项目需要稳定复用的专门流程时补充 |
| `scripts/harness/` | base harness 的最小 gate 脚本与共享 helper |
| `docs/symphony/` | 本机私有 Symphony workflow / 运行说明 / token 占位或真实值，默认不提交 |

固定解释：

- `Linear 是主协作真相`
- `Linear issue Doc 是 issue 级 Execution Plan 主载体`
- `repo 是主执行真相`
- `PR / MR 是次级代码叙事面`
- `.agents/state/` 与 `.agents/runs/` 只补充本地恢复和结果细节，不替代 Linear
- `.agents/plans/` 只提交模板与示例；issue 级真实 plan 不写入公开仓库
- 需要本地 review gate 时，可把 Linear issue Doc 导出为 `.agents/plans/<issue>.md` 这类 ignored cache；cache 不作为协作真相，不提交

## 协作约束

- 复杂任务默认先在 Linear issue Doc 写或更新 `Execution Plan`，再进入实现
- `docs/harness/*.md` 默认应提交
- 初始化后应在 `docs/harness/project-constraints.md` 中登记项目级机械约束；没有可执行命令或 gate 的规则不得标记为 `enforced`
- `.agents/plans/TEMPLATE.md` 默认应提交
- `.agents/plans/EXAMPLE-implementation.md` 默认应提交
- `.agents/plans/<issue>.md` 这类 issue 级导出文件只允许作为本地 ignored cache，不作为协作真相，不提交
- `.agents/state/TEMPLATE.md` 默认应提交
- `.agents/runs/TEMPLATE.md` 默认应提交
- 若后续补齐 `.agents/prompts/` 和 `.agents/guides/`，这些文档默认也应提交
- 若存在 `.agents/prompts/maintenance-loop.md`，默认只做 `report-only` 维护扫描；`issue-create / safe-fix / rule-promotion` 必须由用户显式指定
- 模板配置可提交，真实环境配置不提交
- 若需要环境配置，优先提交 `.env.example`、`settings.example.yaml` 这类示例文件
- `docs/symphony/` 是本机私有运行目录，可保存本机 workflow、token 占位符或真实 token，必须保持 ignored
- `docs/test/*` 默认提交可复用 runbook 与脱敏后的当前 / 本次验证结果摘要
- 原始命令输出、真实凭据、数据库主机、临时目录、完整下载 URL、token、行主键、本机路径等敏感或机器本地痕迹不提交
- 已写入 `docs/test/*` 的脱敏验证结果摘要是提交版测试真相，后续同步或 closeout 不得因为避免敏感信息而删成空模板
- `.agents/state/*` 与 `.agents/runs/*` 的真实运行文件默认不提交
- 本地日志、数据库文件、缓存、IDE 私有文件默认不提交
- `merge` / `escalation` 仍然是流程阶段，但默认不由 initializer 自带 shell gate 承担

## 初始化后先做什么

1. 检查 `.gitignore`。
2. 确认真实配置、日志、数据库文件不会进入暂存区。
3. 若仓库需要环境配置，优先补 `.env.example`、`settings.example.yaml`。
4. 阅读 `docs/harness/`。
5. 填写 `docs/harness/project-constraints.md` 中的项目级机械约束登记表。
6. 阅读 `docs/test/RUNBOOK_TEMPLATE.md`。
7. 阅读 `.agents/PLANS.md`、`.agents/plans/TEMPLATE.md`、`.agents/plans/EXAMPLE-implementation.md`。
8. 若是 agent 驱动初始化，再补齐 `.agents/prompts/` 与 `.agents/guides/`，并选择 `placeholder / full` 模式。
9. 若存在 `.agents/prompts/maintenance-loop.md`，确认默认 mode 是 `report-only`。
10. 执行 `make harness-verify`。

## 多仓协作约定（按需）

- 多仓协作时，默认由 provider 仓维护 contract truth、schema truth、接口示例和服务端验收口径。
- consumer 仓只维护 consumer rule、快照、缓存、mock、golden 或消费侧验证，不反定义 provider truth。
- 若 consumer 仓需要新增或调整 contract 快照，默认同步检查 provider 仓的 contract 文档是否需要更新。

## 目录级 AGENTS（按需）

- 大仓或分层约束较重的目录，可以在子目录放置更细的 `AGENTS.md`。
- 修改某个目录下的代码前，先读取该目录就近的 `AGENTS.md`；更细目录规则优先于根级通用规则。
- 目录级 `AGENTS.md` 只写稳定实现习惯、分层边界、测试约定和代码风格，不承接临时 issue 计划。

## Provider 默认值

- 当前 provider：`github`
- 若后续锁定 GitHub 或 GitLab，只调整 merge 说明，不改变目录结构
