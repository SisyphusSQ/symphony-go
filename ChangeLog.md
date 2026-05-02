## Unreleased

#### feature:
1. [TOO-116] 完善 CLI workflow 路径校验，支持 validate 与 run preflight 识别 positional、--workflow 和默认 ./WORKFLOW.md 路径。
2. [TOO-119] 新增 typed runtime config layer，覆盖 SPEC 默认值、环境变量解析、workspace 路径归一化和启动前校验。
3. [TOO-118] 新增 WORKFLOW.md 动态 reload 能力，支持内容变化检测、last-known-good fallback 与后续 dispatch config/prompt 重应用。
4. [TOO-120] 新增安全的 per-issue workspace manager，支持 identifier sanitize、root containment、目录创建/复用 metadata 与 fail-safe 冲突处理。
5. [TOO-121] 新增 lifecycle hook runner，支持 workspace cwd 执行、timeout、输出捕获、失败结果返回和 create-only `after_create`。

#### note:
1. 新增 symphony-go 版本发布 repo-local skill，明确流程完成前必须回写 changeLog。
2. 切换 Go module 到 github.com/SisyphusSQ/symphony-go，并初始化 GitHub 仓库远端。
3. [TOO-115] 新增 Go port conformance charter 与 SPEC 对齐矩阵，记录必选能力、推荐扩展、验证入口和延期决策。
