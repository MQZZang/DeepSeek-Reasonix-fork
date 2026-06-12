# 模式系统工作流回顾：PR0–PR7 汇总报告

> 适用读者：未跟进过本工作流的工程师、评审者与产品同事。
> 范围：截至 PR7 完成（2026-06-12），含设计背景、各 PR 交付明细、测试护栏现状与剩余路线图。
> 配套规范：[`SPEC.md`](./SPEC.md)（§3.4 计划模式、§3.7 权限与审批）、[`GUIDE.md`](./GUIDE.md)（用户向说明）。

---

## 1. 这个工作流在解决什么问题

Reasonix 同时服务多个前端（CLI TUI、HTTP/SSE、Desktop、ACP、Bot），所有前端共享同一个
`control.Controller` 中枢与 `agent.Run` 执行循环。工作流启动前的体检（Gate 1）发现：
模式相关的语义分散在各处布尔量与文本启发式里 —— plan 模式靠"非空回复就弹审批门"判断提案、
todo 种子靠解析 markdown 列表、问答与规划共用一条链路导致纯提问也被弹门。这些都是
**非根因实现**：表象修补会在每个前端重复一遍。

工作流的目标是把 Ask / Plan / Spec / Agent / Debug / Goal 六个用户模式收敛到少数几个
**内核原语**上，使每个模式只是原语的一种组合，而不是一套平行的 if-else。

### 总原则（全程强制）

- **Gate 流程**：Gate 1 事实体检（只读）→ Gate 2 体验基线设计 → Gate 3 模式系统设计 →
  Gate 4 分 PR 执行。不跳步，每个 PR 先提"七要素"计划（变更目标 / 修改与不修改文件 /
  风险 / 测试方式 / 回滚方式 / 验收标准）经确认后再动手。
- **DeepSeek 前缀缓存是硬约束**：system prompt 与工具 schema 在会话内字节稳定；
  一切模式指令以"turn-tail"（用户回合尾部标记）注入，模式切换零缓存成本。
  该约束由基线 G1 测试永久门禁（见 §6）。
- **权限相关改动单独说明、等显式确认**；行为收窄可直接做，放宽必须有分类器与测试背书。
- **小 PR**：每个 PR 一个根因，测试先行或同行，不留半成品。

---

## 2. 核心架构决策：三个正交内核原语

Gate 3 的产出。六个用户模式由三个互相独立的轴组合而成：

| 原语 | 含义 | 落点 |
|---|---|---|
| **CapabilityCeiling** | 执行能力天花板：`CeilingFull`（无额外限制）/ `CeilingReadOnly`（harness 在权限门之前拒绝一切非只读工具调用，任何审批姿态都打不穿） | `internal/agent/ceiling.go`，`executeOne` 中先于权限门检查 |
| **TurnLifecycle** | 回合生命周期：单回合应答（Ask）/ 门控提案（Plan）/ 自动续转（Goal） | `control.Controller` 的 plan 门循环、goal 续转循环 |
| **Artifact & ExitGate** | 工件与出口门：结构化提交物（计划、未来的 spec）落盘 + 显式审批出口 | `submit_plan` 工具、`.reasonix/plans/` 工件、审批三选 |

用户可见模式与原语的映射：

| 模式 | Ceiling | Lifecycle | Artifact/ExitGate |
|---|---|---|---|
| Agent（normal） | Full | 单回合 | 无 |
| Ask | ReadOnly | 单回合，答完即止 | 无 |
| Plan | ReadOnly | 门控 | `submit_plan` → 工件 → 三选审批 |
| Goal | Full | 自动续转（标记驱动） | 无（PR7 将硬化） |
| Debug（规划中） | 两段式 ReadOnly→Full | 门控 | repro 契约（PR8） |
| Spec（规划中） | ReadOnly | 分 stage 门控 | spec 工件模板（PR9） |

与之平行、**不属于模式**的轴：工具审批姿态（`ask` / `auto` / `yolo`）。任何姿态都不会
代替用户批准计划，也不回答 `ask` 工具的问题。

---

## 3. PR 总览

| PR | 一句话 | 主要包 | 基线影响 | 状态 |
|---|---|---|---|---|
| PR0 | 基线测试套件：把 Gate-2 体验基线固化为可执行测试，建立 KNOWN-GAP 契约 | control（纯测试） | 新增 12 项基线 | ✅ 完成 |
| PR1 | `CapabilityCeiling` 重构：plan 布尔泛化为 Ceiling 原语，外部行为零变更 | agent、control | 12/12 原样全绿 | ✅ 完成 |
| PR2 | Ask 模式：只读问答、零审批摩擦、turn-tail 注入 | control、cli、desktop 映射 | A3(ask 侧) 翻绿，G1 扩为含 ask 的六步循环 | ✅ 完成 |
| PR3 | 只读 bash + ceiling 继承子代理：ReadOnly 天花板内放行无副作用调用 | agent、permission、tool/builtin | B3 翻绿 | ✅ 完成 |
| PR4 | `submit_plan` 工具 + 计划工件落盘 + 结构化种 todo + 问答不弹门 | tool/builtin、control、i18n | A3(plan 侧) 翻绿，B1/B4 重写为结构化 | ✅ 完成 |
| PR5 | 审批三选（批准/修改后再提/拒绝）+ `plan-approved` 标签检查点 | control、cli、serve、desktop 桥 | B4 语义保持，新增修订路径 9 项测试 | ✅ 完成 |
| PR6 | 执行窗收紧：批准后免审仅覆盖可预览 writer，bash/MCP 照常弹门（权限收窄，经用户确认） | control | A2 注释 risk-5 RESOLVED，新增执行窗 5 项测试 | ✅ 完成 |
| PR7 | Goal 硬化：paused 状态（markerless×2/取消/手动）+ `/goal pause\|resume` + sidecar 持久化跨 session 恢复 | agent、control、i18n | F2/F4 翻绿，新增 11 项硬化测试 | ✅ 完成 |

依赖链：PR1 是 PR2/PR3 的前提；PR4 是 PR5/PR6 的前提；PR7 仅依赖 PR1。全部按序落地。

---

## 4. 各 PR 详述

### PR0 — 基线测试套件（护栏先行）

**根因定位**：后续每个 PR 都会动模式语义。没有"当前行为"的可执行快照，就无法区分
"有意翻转"与"意外回归"。

**交付**：
- `internal/control/baseline_{ask,plan,agent,goal,invariants,helpers}_test.go` 六个文件，
  把 Gate-2 的工作流基线（A=问答 / B=计划 / D=代理稳定性 / F=目标 / G=不变量）钉成测试。
- **KNOWN-GAP 契约**：已知产品缺口按"今天的错误行为"断言，并打可 grep 标记
  `KNOWN-GAP(risk-N → PR-K)`。缺口测试变红 = 行为变了，要么在对应 PR 里翻转断言，
  要么是回归。**永不删除，只翻转。**
- 测试基建：`fakeTool`（可脚本化工具，支持 `IsReadOnlyCommand` 分类器注入）、
  `scriptedTurns`（脚本化模型回合）、`autoRespondApprovals`（防死锁审批应答器）。
  后续全部 PR 的测试都构建在这套基建上。

**验收**：纯测试 PR，零产品代码改动；suite 全绿即基线锁定。

### PR1 — CapabilityCeiling 内核原语（外部行为零变更的重构）

**根因定位**：`agent` 内的 `planMode bool` 是单一消费者专用开关。Ask（PR2）、Debug 两段式
（PR8）都需要"只读天花板"，再加布尔就是平行 if-else 的开始。

**交付**：
- `internal/agent/ceiling.go`：`Ceiling` 类型（`CeilingFull`=零值 / `CeilingReadOnly`），
  原子存取，`executeOne` 在**权限门之前**检查 —— 没有任何审批姿态能打穿天花板。
- `control.Controller` 引入 `collabMode`（`CollabNormal` / `CollabPlan` / `CollabAsk`），
  plan 与 ask 都映射到 `CeilingReadOnly`，差异只在生命周期。
- `SetPlanMode` 保留为兼容 shim。

**验收**：PR0 基线 12/12 原样全绿（不改一字）—— 重构零行为变更的直接证明。

### PR2 — Ask 模式（只读问答，零摩擦）

**根因定位**：用户提问走 normal 模式会被写工具审批打断，走 plan 模式会被
"非空回复弹门"误伤（当时的 A3 缺口）。问答需要自己的组合：ReadOnly + 单回合 + 无门。

**交付**：
- `CollabAsk`：`AskModeMarker` 以 turn-tail 注入（前缀缓存不动），答完即止，不弹任何门。
- TUI：`/ask` 命令族 + Shift+Tab 循环 normal→plan→ask；状态行与模式 chip 同步。
- desktop：Go 侧映射与 `collaborationMode.ts` 规范化（未知值安全回退 normal）；
  React chip 完整 UI 记为 desktop pass（PR10 期）欠账。
- `ask` 工具（结构化提问卡）在 plan/ask 模式下可用，`ReadOnly()=true`。

**验收**：A3 的 ask 侧翻绿（`TestBaselineA3AskModeQANoApprovalGate` 零审批）；
G1 前缀稳定性循环扩为六步（含 ask 切换），字节恒等保持。

### PR3 — 只读 bash 与 ceiling 继承子代理

**根因定位**：ReadOnly 天花板把 bash 一刀切拒绝，但 `git log` / `go vet` 这类只读命令
正是规划期最需要的侦察工具；`task` 子代理同理 —— 父代理只读时子代理不应该能写。
这是 B3 基线钉住的缺口。

**交付**（权限相关，提交前单独安全说明并经确认）：
- 可选接口 `commandReadOnly`（`IsReadOnlyCommand(args) bool`）：静态 writer 工具可按
  **单次调用**声明只读。bash 借 `internal/permission/bash_readonly.go` 分类器放行
  只读命令；分类器白名单式、默认拒绝。
- `task` 子代理继承父 `Ceiling`（`executeOne` 把 Ceiling 盖进 `context.Context`）。
- 安全边界声明：本 PR **放宽**的唯一路径有分类器与测试背书；deny 规则仍最高优先。

**验收**：B3 翻绿（只读 bash 与 task 在 plan 模式可用、writer bash 仍被拒）；
`bash_readonly_classify_test.go` 钉住分类器语料。

### PR4 — `submit_plan` 工具：从文本启发式到显式提案

**根因定位**：plan 门键在"回复非空"上 —— 问答也弹门（A3 的 plan 侧缺口）；todo 种子
解析 markdown 列表 —— 格式漂移即碎（B1 脆弱点）。两者同根：**计划没有结构化的提交动作**。
只有模型自己知道一段文字是不是提案，所以让它显式说出来。

**交付**：
- `internal/tool/builtin/submitplan.go`：`submit_plan` 工具（`ReadOnly=true`），
  schema 为 `title` + `phases[]`（每相位 `name` + 可选 `steps[]`）。工具本身无状态，
  controller 消费提交。
- 门重写：**只有 `submit_plan` 调用弹门**；纯文本是对话，答完即止（A3 翻绿）。
  形似计划（markdown 列表）却没提交的回复获得**一次** nudge（合成回合，UI 不渲染）；
  模型再不提交则发 Notice 结束回合，不弹门。
- 工件落盘：`.reasonix/plans/<时间戳-slug>.md`，frontmatter
  `status: draft → approved|rejected → executed`。追加式历史 —— 修订产生**新**工件，
  不改写旧的。落盘是 best-effort：无 workspace root 或写失败不阻断主流程。
- todo 种子改为从结构化提交生成（相位=0 级、步骤=1 级、首项 in_progress、上限 20），
  markdown 启发式仅作为渲染回路校验保留。

**验收**：A3 plan 侧翻绿 + `plan_submit_test.go` 全链路（nudge 接受/拒绝、工件生命周期、
模式外提交忽略）；既有 auto-plan e2e、YOLO 门测试全部重写后通过。

### PR5 — 审批三选与 `plan-approved` 标签检查点

**根因定位**：评审计划最高频的动作是"方向对，改两处再来"，旧门只有批准/拒绝两个出口，
用户被迫拒绝后手工重新组织上下文。同时批准后的执行改动与计划讨论混在同一个回合检查点里，
"回到动手前"在 rewind 列表不可见。

**交付**：
- 控制器：`RevisePlan(id, feedback)` 公开 API；门重写为**修订循环** ——
  拒绝带反馈 → 工件标 `rejected` → 反馈以合成用户消息（`planReviseMessage` 前缀）回灌 →
  模型修订重提 → 新 draft 工件、新一轮门。每迭代消耗一次显式用户决策，循环由用户驱动；
  修订段只认反馈之后的提交（旧计划无法重新弹门）；修订不重提则回合正常结束。
  误用于工具审批时降级普通拒绝。
- **标签检查点**：批准瞬间 `beginCheckpoint("plan-approved: <标题>")` ——
  执行段 pre-edit 快照落入该检查点，`/rewind` 一跳回到动手前。
- 前端三面：TUI `e` 键反馈输入态（Esc 返回、空 Enter 不发、Ctrl-C 取消整轮）；
  serve `POST /approve` 可选 `feedback` 字段（向后兼容）；desktop Go 桥
  `RevisePlan` / `RevisePlanTab`。
- 文档：SPEC §3.7、GUIDE 双语同步。

**验收**：`plan_revise_test.go` 4 项（黄金修订路径 / 不重提不死锁 / 标签检查点 /
管道契约）+ TUI 3 项 + serve 1 项，全绿；B4（普通拒绝留在 plan）语义原样保持。

---

### PR6 — 执行窗收紧（权限收窄，经用户显式确认）

**根因定位**：批准计划后的执行 turn 内，旧实现对**除 plan 审批外的一切工具**免审
（`approvalBypassAllowsLocked` 等同整轮 YOLO）——批准"改三个文件"的计划，隐含授权了
窗内任意 `rm -rf`、MCP 调用与子代理。这是 Gate-1 risk-5 的执行窗侧。

**方案选型**（否决名单硬编码与路径白名单后）：免审判定改为**工具非只读且实现
`tool.Previewer`** —— 该集合（write_file / edit_file / multi_edit / delete_range /
delete_symbol / notebook_edit）恰好等于 plan-approved 检查点能预览并快照的集合，
"每个免审写都可回滚"由构造保证，未来新 writer 实现 Previewer 即自动同时获得免审与
快照两件套，不会漂移。

**交付**：
- `approvalBypassAllowsLocked` 拆分：YOLO 姿态语义不变（用户显式 opt-in，A2 钉住）；
  执行窗分支改走新谓词 `planWindowWaivesLocked`（经 `c.reg` 查 Previewer；
  无 registry 时保守不豁免）。
- bash / MCP / task / kill_shell 在窗内恢复正常审批；只读 bash 命令不受影响
  （`permission.Gate.Check` 在 policy 决策前已重分类放行，从不到达审批层）；
  被显式 ask 规则点名的只读工具窗内照常弹门（窗不豁免用户自己的规则）。
- deny 最高优先、session grants、auto 姿态（policy 层 `Mode=Allow`，不经 bypass）
  全部零改动；permission 包零改动。
- 文档：SPEC §3.7 执行窗语义重写；A2 注释 risk-5 标 RESOLVED。

**验收**：`plan_exec_window_test.go` 5 项 —— 窗内可预览 writer 免审且窗随 turn 关闭
（无幽灵免审）/ 窗内 bash 弹门（B1 附加断言）/ 窗内拒绝 bash 即拦截 / 执行段失败
defer 复位窗 / 谓词单测（writer 真、bash 假、只读假、未注册假、无 registry 假）。
control、agent、permission、tool 全包 `-count=1` 绿。

---

### PR7 — Goal 硬化（paused 状态 + sidecar 持久化）

**根因定位**：goal 模式的两个韧性缺口（Gate-1 risk-7）。① 模型回复缺
`[goal:*]` 标记时落入 `advanceGoalAfterTurn` 的 default 分支静默续跑（最多烧满
50 轮上限）——根因是 `parseGoalStatusMarker` 的 `ok` 返回值被丢弃，"显式
[goal:continue]" 与"忘了打标"不可区分（F2）。② goal 全状态只活在 Controller
内存，session 的 JSONL 与 branch meta 都不携带，重启/恢复即丢（F4）。

**交付**：
- **paused 状态**（`GoalStatusPaused`）：markerless 容忍一次、连续两次自动暂停
  （目标文本保留）；显式 marker 重置计数。取消回合（Esc/ctx cancel）从
  stopped 改为 paused —— 取消是暂停而非丢弃。`/goal pause` 暂停（运行中在下一
  loop 边界生效）、`/goal resume|continue` 恢复 paused/blocked 目标并清审计、
  重启续跑循环；`/goal`（status）显示非 running 状态。裸词才是动词
  （"/goal resume the migration" 仍是设目标）。
- **sidecar 持久化**：`BranchMeta` 增可选 `Goal *GoalState`（text/status/turns/
  blocks/block/markerless，`omitempty` 前向兼容）。控制器每次状态变更
  `persistGoal()` 镜像进 `<session>.jsonl.meta`（原子写；清除不创建文件——
  避免给空 session 凭空铸 meta）；complete/cleared 即从 sidecar 移除。
  `Resume()` 经 `restoreGoalFromMeta` 恢复：running/paused → **一律 paused**
  （不静默自启自治执行），blocked → blocked（原因保留）；无 goal 的 session
  清掉前一会话残留。`/new`、`/clear` 旋转 session 时清 goal（会话域语义）。
- i18n 七条双语文案（paused/resumed/restored/markerless 暂停等）。

**验收**：F2/F4 两个 KNOWN-GAP 翻绿为永久契约（`baseline_goal_test.go` 重写）+
`goal_harden_test.go` 11 项（计数重置 / 命令解析 / pause-resume 状态机 / 取消即
暂停 / meta round-trip / 无 goal 清残留 / blocked 恢复 / resume 重启循环 /
pause 命令 / new-session 清理）。F1/F3 等既有 goal 契约原样保持；修复过程中
发现并堵住 `persistGoal` 在 `/clear` 后凭空创建 meta 文件的副作用
（`TestSubmitClearDiscardsCurrentContextWithoutSavingTranscript` 钉住）。

---

## 5. 基线护栏体系现状

用例族 → 测试函数的权威映射维护在 `internal/control/baseline_helpers_test.go` 文件头。
摘要：

| 族 | 含义 | 现状 |
|---|---|---|
| A1/A2 | 只读问答零审批；writer 在 ask/auto/yolo 三姿态下的弹门契约 | 钉住；risk-5 执行窗侧 **PR6 RESOLVED**，auto/yolo 为显式姿态契约 |
| A3 | plan 模式问答不弹门 / `submit_plan` 才弹门 | **PR4 翻绿** |
| B2/B3 | plan 拒 writer；只读 bash 与 task 放行、writer bash 仍拒 | **PR3 翻绿** |
| B4 | 拒绝计划留在 plan 模式可迭代 | 钉住，PR5 增修订路径 |
| B5 | auto-plan 语料 | 两行 `KNOWN-GAP(risk-8 → PR10)` 仍挂 |
| D2 | 错误风暴触发 loop-guard；被拒审批不误触发 | 钉住 |
| F2/F4 | goal markerless×2 自动暂停；goal 经 sidecar 跨 session 恢复（paused） | **PR7 翻绿** |
| G1 | **永久门禁**：六步模式轮换中 system 消息与工具 schema 字节恒等 | 钉住 |

**现存 KNOWN-GAP 全集**（grep `KNOWN-GAP` 可核）：
仅剩 `risk-8 → PR10` ×2（auto-plan 误判行）。
risk-5 已随 PR6、risk-7 已随 PR7 翻绿为永久契约（auto/yolo 保留为显式姿态契约）。

---

## 6. 验证基线与已知环境性失败

每个 PR 的验收都包含：`go build ./...`、`go vet ./...`、受影响包 `-count=1` 测试、
全仓测试。当前全仓状态（Windows 开发机）：

- **与工作流相关的包全绿**：control、agent、tool/builtin、permission、i18n、serve、event。
- **三个失败/不稳定均为既有环境问题**，在干净 HEAD 上同样失败，PR0 起记录在案：
  - `internal/cli` `TestModelSwitchRefreshesCustomStatusline` — 依赖 Unix `cat`；
  - `internal/installsource` `TestApplyLocalSkillLinkMode` — Windows symlink 权限；
  - `internal/serve` `TestServeIndexPagePassesLanguagePreferenceToClient` —
    全局 i18n 状态泄漏导致的顺序敏感 flaky；
  - desktop 模块 `TestUpdateMCPServerSplitsPastedCommandLine`、
    `TestAddSkillPathRestoresConventionRootWithoutCustomPath` — 平台限制（bash sandbox
    不可用 / 路径约定），已用 stash 法验证与本工作流无关。

---

## 7. 剩余路线图（Gate-3 规划，未执行）

| PR | 内容 | 风险档 | 依赖 |
|---|---|---|---|
| PR8 | Debug 模式两段式（ReadOnly 诊断 → Full 修复）+ repro 契约入 evidence | 中 | PR1, PR3 |
| PR9 | Spec 模式 v1：工件模板 + 分 stage 执行，复用 plan 的门与工件机制 | 中 | PR4, PR5 |
| PR10 | auto-plan 语料调阈 + 误触发一键撤回（翻绿 risk-8）；审批姿态统一命名（risk-1）；desktop/文档/站点对齐 pass | 低 | 前述全部 |

**已知欠账（非半成品，API 已就绪待前端补全）**：
- desktop React 审批卡三选 UI 与模式 chip（Go 桥已导出，Wails 绑定待 generate；本机无 npm/Wails 验证环境）；
- bot gateway 的 `revise <id> <feedback>` 文本命令（现 deny 语义不变）；
- serve 内嵌 `index.html` 的反馈输入框（HTTP API 已支持 `feedback` 字段）。

---

## 8. 代码索引（按包）

| 位置 | 用途 | 引入 |
|---|---|---|
| `internal/agent/ceiling.go` | Ceiling 原语定义与原子存取 | PR1 |
| `internal/permission/bash_readonly.go` | bash 只读命令分类器 | PR3 |
| `internal/tool/builtin/submitplan.go` | `submit_plan` 工具 | PR4 |
| `internal/control/plan_artifact.go` | 提交解析、工件落盘、状态机、todo 种子 | PR4 |
| `internal/control/controller.go`（plan 门段 + `resolvePlanSubmission` + `RevisePlan` + `planWindowWaivesLocked`） | 门控生命周期：nudge、修订循环、标签检查点、执行窗收窄 | PR4/PR5/PR6 |
| `internal/control/input.go` | turn-tail 标记（`PlanModeMarker`/`AskModeMarker`）、合成消息前缀表 | PR2/PR4/PR5 |
| `internal/control/baseline_*_test.go` | 基线护栏套件与 KNOWN-GAP 契约 | PR0 |
| `internal/control/plan_submit_test.go` / `plan_revise_test.go` / `plan_exec_window_test.go` | PR4/PR5/PR6 行为测试 | PR4/PR5/PR6 |
| `internal/agent/branch.go`（`BranchMeta.Goal` + `GoalState`） | goal 的 sidecar 持久化载体 | PR7 |
| `internal/control/goal_harden_test.go` | PR7 行为测试（暂停/恢复/持久化） | PR7 |
| `internal/cli/chat_tui.go`（审批键位 + `handlePlanFeedbackKey`） | TUI 三选与反馈输入态 | PR2/PR5 |
| `internal/serve/serve.go`（`/approve`） | HTTP 审批面（含 `feedback`） | PR5 |
| `desktop/app.go`（`RevisePlan*`） | desktop Go 桥 | PR5 |

---

## 9. 给评审者的三条主线

1. **一切收敛到原语**：读 `ceiling.go`（30 行）即可理解 Ask/Plan/Debug 共享的安全模型；
   门控生命周期全部在 `controller.go` 的 plan 门循环一处，六个前端零重复。
2. **护栏即规范**：行为问题先看 `baseline_helpers_test.go` 的 case map 与 KNOWN-GAP 标记，
   再看 SPEC；测试名就是行为契约的索引。
3. **缓存不变量不可破**：任何"往 system prompt / 工具表加模式说明"的改法都会被 G1
   测试拦下 —— 模式指令只能走 turn-tail。
