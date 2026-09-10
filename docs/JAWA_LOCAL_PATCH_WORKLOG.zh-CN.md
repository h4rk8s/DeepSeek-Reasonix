# Jawa 本地 Reasonix 语义补丁台账

更新时间：2026-09-11

## 维护基线

- 唯一长期分支：`jawa/reasonix-composer-state-visibility`
- 主 checkout：`/Users/jawa/Lab/2026-07-06-reasonix-dev`
- 本轮固定上游：`e2145b031deef603d9ec1acc9d30f9a1e1ac9325`（v1.38.6）
- 补丁结构：13 个产品语义 owner + 2 个维护闸门 + 1 个临时 renderer pin；线性 Git 栈共 18 个提交（计费日历和队列图片预算是所属 owner 的 follow-up fix）
- 同步入口：`scripts/jawa-upstream-sync.sh`
- 临时 worktree：只允许位于仓库内 `.worktree/<task>`

本台账描述行为契约，不把某次提交 SHA 当成真源。每次重放后，准确提交以：

```sh
git log --reverse --format='%H %s' origin/main-v2..HEAD
```

为准。最后一个提交不能在自己的正文中记录自己的 SHA，避免自指。

## 不可破坏的维护原则

1. 本地只维护一条长期集成线。实现、审计和同步分支完成后必须收回长期线并删除，不形成平行产品分支。
2. 上游更新固定一次基线后再重放；阶段内不边做边追。使用 `range-diff` 检查语义映射，不能用“rebase 成功”代替审查。
3. 配置、transcript、composer、worktree、usage、memory 和 context 各自只能有一个权威 owner。
4. 上游已经提供的能力优先复用；本地只保留更细的用户契约，不复制第二套 executor、ledger、freshness clock 或 merge 生命周期。
5. 动态 workspace、时间、usage、memory result、图片结果只进入 runtime/session tail，不进入 cache-stable system/tool prefix。
6. 用户配置解析失败必须带路径返回非零错误。禁止加载默认值继续运行，禁止静默覆盖，普通读取不得写盘。
7. permission、sandbox、folder trust 和 worktree isolation 是四个独立边界，不能互相替代。
8. live、resume、copy、resize 和 mouse hit-test 必须消费同一 typed transcript；不能各自从渲染字符串猜语义。
9. 手工文本 `[Image #1]` 永远只是文本。只有带稳定内部 ID 的附件 part 才能触发图片处理。
10. 上游实现等价能力时，先用 contract test 对比细节；完全覆盖才退休本地补丁，不能仅凭功能名称相同就删除。

## 最终单向 owner 图

```text
upstream config schema
  -> authoritative config/provenance
  -> worktree, usage, vision, CLI presentation

upstream event log
  -> canonical semantic transcript
  -> planner, disclosure, live/replay/resume/copy

typed composer parts
  -> queue admission
  -> vision input normalization

upstream exact worktree merge
  -> explicit subagent isolation

provider rate_schedules + upstream ModelRef
  -> occurrence-time CostQuote
  -> fail-closed usage projection

upstream memory Scope + FreshnessFor
  -> diversity rerank

upstream ContextManager
  -> overflow recovery + complete-tool-boundary maintenance

upstream operation ledger
  -> exact receipt reference
```

箭头只允许从基础 owner 指向 consumer。最终代码中不存在反向 import，也不存在一个 consumer 回写基础真源的自指环。

## 13 个产品语义补丁

下面顺序是长期线的真实 replay 顺序。每一个中间提交都应可构建；后续补丁可以把早期临时表示迁移到最终 owner，但不得要求“未来提交先存在”。

### 1. `fix(agent): project planner output without transcript duplication`

**用户问题**

Planner 和 executor 曾把相同答案各显示一次；planner 的过程文本也会混入用户可见 transcript。

**最终行为**

- planner 流仍保留 usage、状态和调试证据，但普通 planner 文本不作为 assistant 回复落入可见 transcript。
- executor 只接收结构化 handoff，并负责最终用户答案。
- 只有明确的 `[no_changes]` 结构化结果可以跳过 executor。
- 不再用“你好”“无需修改”等中英文短语猜测 planner 是否完成。

**Owner 与边界**

- Owner：agent coordinator 的 planner event projection。
- 不改变 provider prompt、planner session mutex 或 executor 工具权限。
- 不由 TUI 隐藏重复文本来掩盖内核重复事件。

**验证**

- planner/executor 双模型只出现一个最终回答；
- planner usage 仍归集；
- 普通问候不能误触发 no-op；
- 显式 no-op 可以终止且有 contract test。

**退休条件**

上游同时提供结构化 planner outcome、单一可见回答和准确 usage 归因后可退休。

### 2. `feat(vision): add managed image understanding pipeline`

**用户问题**

CleanShot、Raycast clipboard history、文件 URL 和延迟落盘会产生不同 clipboard 形态；过去有时只粘贴路径、有时图片丢失，也看不到图片是否已理解。

**最终行为**

- 归一化 NSPasteboard 图片、文件 URL、本地路径、CleanShot 和 Raycast clipboard history 的真实来源。
- 在文件仍落盘时做有界稳定性检查，失败给出可诊断错误，不把随机路径作为普通消息发送。
- 优先使用上游 `vision_model`；本地 OCR/UI-state command 只作为明确 fallback，不与 native vision 双跑。
- 多图一次提交，保留每张图稳定引用，并产生独立的 Image understood disclosure。
- 图片摘要和来源属于 runtime/session metadata，不污染 cache-stable prefix。
- 图片处理期间 footer 可临时显示 vision 模型，完成后恢复常规模型段。

**Owner 与边界**

- Owner：control image-understanding pipeline。
- 输入只接收 typed attachment identity；模型选择只认上游 `vision_model`。
- 不创建第二套 provider registry，不直接在 TUI 调 OCR。

**验证**

- 路径、file URL、TIFF/PNG clipboard、多图和延迟文件；
- native/fallback 互斥；
- provider error、timeout 和取消；
- resume 后 disclosure 与 live 一致；
- 不改变 prefix hash。

**退休条件**

上游覆盖 macOS 多来源归一化、稳定落盘、native/fallback 互斥、结构化结果与 resume disclosure 后可退休。

### 3. `feat(cli): establish configurable interactive shell`

**用户问题**

原始 TUI 缺少可配置的信息结构、composer 边界和终端标题；半屏窗口里信息密度失衡，消息层次不清。

**最终行为**

- 提供 `hybrid / balanced` 等 presentation 配置，并保留上游默认值兼容。
- 支持 turn 留白、用户输入 band、assistant 圆点、`›` composer prefix、可选 composer frame。
- terminal title 可按 session/project 更新，可手动覆盖和恢复。
- status 支持两层结构，展示模式、模型、CWD/branch、cache/context/cost，并按真实剩余宽度降级。
- 输入换行只显示一个 prompt icon；软换行不复制 `›`。

**Owner 与边界**

- Owner：CLI shell configuration 与基础 renderer。
- 纯 presentation 字段不进入 agent prompt。
- 不在 ANSI 字符串中嵌入业务状态；后续 typed projection 统一接管语义。

**验证**

- 80/104/半屏/全屏宽度 golden；
- 单行、多行、CJK、长路径和长 branch；
- terminal title 生命周期；
- config round-trip 与未知字段兼容。

**退休条件**

上游提供同等可配置 IA、单 prompt composer、两层 status 和 title 生命周期后可按字段逐项退休。

### 4. `feat(subagent): add recoverable worktree isolation lifecycle`

**用户问题**

两个 writer subagent 共享 workspace 时会互相覆盖；仅有目录创建不等于完整隔离，也不能安全 apply 或恢复。

**最终行为**

- 稳定枚举 `isolation: none | worktree`；兼容默认仍为 `none`。
- read-only subagent 继续共享 workspace；writer 只有显式选择才创建隔离 worktree。
- resource metadata 保存 source/root/branch/base/head/state/cleanup/created time，并支持 resume/continue_from。
- source dirty 时 fail-closed，除非调用方明确选择 committed-head policy。
- child tools、config、instructions、skills、commands、hooks、MCP 和 sandbox root 全部绑定 child root。
- 创建位置固定为仓库内 `.worktree/<task>`。
- Apply 复用上游 `InspectMerge -> MergeBack -> FinalizeMerge` exact-tree/CAS 生命周期，不再执行本地 `git apply` 双检查。
- clean apply、冲突路径、取消、orphan、list/show/gc 都有结构化状态；不会盲删含用户修改的 worktree。

**Owner 与边界**

- Owner：`internal/worktree` service；agent/boot 只消费接口。
- Desktop Delivery 的 durable policy 保持独立，不因 subagent cleanup 改变。
- worktree 不替代 permission、sandbox 或 folder trust。

**验证**

- 两个 background writer 修改同一文件但父 workspace 不变；
- 两份 diff 独立；
- clean merge 成功，冲突不半应用；
- dirty source、subdirectory、linked worktree、cancel/orphan、resume；
- exact merge recovery token。

**退休条件**

上游把显式 subagent isolation、child-root discovery、durable metadata 和 exact merge 生命周期完整接通后可退休。

### 5. `feat(usage): aggregate occurrence-time quotes fail closed`

**用户问题**

未知 pricing 曾显示 `$0`，nested/background usage 未归集也像完整结果，无法用于自动化对账。

**最终行为**

- Headless result 使用 schema v2，并保留旧字段兼容。
- 输出 totals、per-model 和 main/planner/executor/subagent attribution。
- `usage_is_incomplete` 和 `cost_is_partial` 明确表达尚未归集或缺失 quote。
- 完整成本才输出 USD；精确整数 ticks 避免 float 累积误差。
- 只消费事件发生时的 `ModelRef` 和 `CostQuote`，绝不根据当前 pricing 重新定价。
- Provider 可在 TOML 中声明按 model、生效区间、时区、工作日和峰值窗口版本化的 `rate_schedules`；供应商调价只改配置，不改源码。
- `CostQuote` 优先使用显式配置日历，未命中才回退静态 `price` 或旧内置 catalog；历史费率段与当前费率段互不覆盖。
- 峰谷档位使用第一笔真实 HTTP 请求的发起时刻，而不是响应完成时刻；跨 12:00/18:00 的长请求不会被完成时刻误分类。
- stream-json 最后一行仍为 result，stdout 只含 JSON/NDJSON，诊断走 stderr。
- ACP 复用同一 projection，不维护第二套计算逻辑。

**Owner 与边界**

- Owner：usage ledger 只负责聚合、完整性和归因。
- 定价 owner 是 occurrence-time CostQuote；供应商数字和 cutover 属于用户配置，Go 代码只解析通用日历规则。

**验证**

- 单模型、planner/executor、foreground/background/nested；
- 完整、部分和完全未知 quote；
- resume/copy 不重复计费；
- JSON purity、整数 ticks 和 ACP projection。

**退休条件**

上游提供同等 fail-closed 完整性、per-source 归因、精确 ticks 且 CLI/ACP 共用 owner 后可退休。

### 6. `feat(memory): add provenance-aware diverse recall`

**用户问题**

BM25 容易让多个同义 memory 占满上下文，旧事实又缺少来源和时效解释。

**最终行为**

- 保留 Markdown/typed memory 真源、审批、archive-on-forget 和 BM25。
- 复用上游 `Scope`、`LastVerifiedAt`、`ExpiresAt`、`Volatility` 与 `FreshnessFor`，不再维护第二套 scope/clock。
- 仅新增 `SourceKind` 等互补 provenance。
- BM25 + relative floor 后执行可配置 Jaccard/MMR 去重，保持 top-1 relevance。
- debug/doctor 可解释 relevance、diversity、freshness 和 source。
- 旧格式无需迁移；读取绝不自动写盘；archived memory 不参与 active recall。

**Owner 与边界**

- Owner：上游 memory metadata + 本地 recall reranker。
- staleness 只影响排序，不宣判事实错误，不覆盖 truth lock。

**验证**

- 中英混合函数名、文件名、错误短语和参数 corpus；
- Recall@K、重复率、注入 token、top-1 稳定；
- old/new format、no-write-on-read、archive。

**退休条件**

上游提供相同多样性、可解释 ranking 和来源字段且不破坏旧文件语义后可退休。

### 7. `feat(config): enforce authoritative config with provenance`

**用户问题**

配置损坏时回退默认并继续保存会直接毁掉权威本地配置；同时用户无法确认字段来自 builtin、user 还是 project。

**最终行为**

- 用户配置 parse/validation 失败时返回包含路径和字段的 typed error，并以非零状态退出。
- 普通 runtime load 和 edit load 都不使用默认或 last-known-good 掩盖错误，也不写盘。
- 所有生产 edit API 返回 error；不再通过 panic 处理坏配置。
- `reasonix doctor config --json` 只读展示 builtin/user/project 来源、实际生效值、ignored/unknown/retired key 和 unresolved model。
- 诊断输出脱敏，不泄露 provider secret、header 或图片 command 正文。
- remote/desktop/serve edit 路径共享同一 authority contract。

**Owner 与边界**

- Owner：`internal/config` load/edit/inspect。
- UI 不自行合并配置；migration warning 不能替代 parse failure。

**验证**

- malformed TOML、unknown key、坏 project scope、LKG 存在/不存在；
- CLI/Desktop/serve edit；
- read-only SHA256 不变；
- provenance JSON 和 secret redaction。

**退休条件**

上游同时具备 fail-closed load/edit、只读 provenance、脱敏和完整调用方 error handling 后可退休。

### 8. `feat(transcript): add canonical semantic projection`

**用户问题**

live、resume、Desktop、serve 和 copy 曾各自解析字符串，导致 resume 刷屏、角色边界丢失、thinking/image 状态与现场不同。

**最终行为**

- `internal/transcript` 把 event 投影为 typed role/activity/tool/result/turn-boundary/disclosure entry。
- CLI store 以单个 `transcriptBlock` 原子保存 rendered 内容和 semantic source，不再维护会漂移的平行 slice。
- live、resume、copy/export、resize、mouse hit-test、Desktop history 和 serve adapter 消费同一语义。
- hydration 不制造新 turn，不展开已完成 disclosure，也不丢 assistant output。
- planner 文本、image result 和 tool card 通过 source kind 区分，不靠文案猜测。

**Owner 与边界**

- Owner：canonical transcript projection。
- 前端只决定样式、折叠和 viewport，不改变会话事实。

**验证**

- live 与 resume golden；
- copy/export gutter；
- 连续工具、planner/executor、image disclosure；
- resize 后 hit range 重建；
- typed store 长度和身份始终一致。

**退休条件**

上游提供跨 CLI/Desktop/serve 共用的 typed projection，并覆盖 hydration/copy/mouse contract 后可退休。

### 9. `feat(cli): render stable transcript disclosures and status`

**用户问题**

Thought/Image 点击区域过大、hover 抖动、展开向下顶、选择文字时自动折叠、resume 默认展开，以及 vision 让半屏 footer 变三层。

**最终行为**

- `disclosureModel` 是 Thought 与 Image understood 的统一交互 owner，内容类型和 detail 独立。
- 点击只在文字/图标实际 hit range 生效；hover 不把整行变成可点击区域。
- 单击展开向上挤 transcript，composer/footer 锚点不下移；再次单击明确收起。
- 拖动选择、复制和滚轮不触发收起；hover 不改变布局尺寸。
- completed Thought 默认折叠，展开有首尾留白和 icon；resume 与 live 状态一致。
- 多图默认一条 `Image understood · N images · ...`，detail 按图片分段。
- `viewportProjection` 统一 scroll anchoring/new-message pill/Ctrl+End/click clear。
- `statusProjection` 同时决定渲染和预留行数，vision 活跃时最多保持两层内容；结束后恢复普通模型段。
- Bubble Tea v2 的 hard-scroll 会在重复行场景留下物理旧行；在当前 Ultraviolet 基线之上临时固定上游 `#143` 的单点修复，避免 todo、working token 和 footer 残影跨过 composer。
- assistant marker、turn spacing、user band 和正文左边界保持配置对齐。

**Owner 与边界**

- Owner：CLI disclosure、viewport、status projection。
- 不把鼠标状态写回 transcript，不由 config 猜 disclosure 身份。
- renderer pin 只承载 Ultraviolet `#143`，父提交必须等于 Reasonix 原依赖；官方包含同一修复后删除 `go.mod` replacement，不长期分叉 renderer。

**验证**

- hit-test、hover、drag selection、wheel、click toggle；
- live/resume parity；
- scroll-away + new output、Ctrl+End、pill click；
- 80/104/半屏 status、vision start/finish；
- Ultraviolet repeated-row hard-scroll 随机回归；
- wrapped composer 只有一个 prefix。

**退休条件**

上游不仅支持“点击 thinking”，还必须覆盖同等 hit area、向上展开、选择行为、resume parity、Image detail 和两层 footer 才可退休。Ultraviolet `#143` 一旦进入官方依赖，只单独退休 renderer replacement，不影响其余展示语义。

### 10. `feat(inbox): add queue clearing and slash completion`

**用户问题**

恢复会话时可能出现多个 pending instruction 并暂停 inbox；用户看得到却找不到可发现的一键清空方式。

**最终行为**

- `/queue clear` 原子清空 pending instructions。
- `/queue` 进入 slash autocomplete，并有本地化说明。
- clear 后暂停状态、持久队列和 UI 计数同步更新。
- 普通消息、图片附件和 image-only queue 继续走同一 admission transaction。
- 多图冻结副本按整条 inbox item 的字节预算自动压缩；未超限图片和源附件保持原样，不裁掉画面内容。
- 极端情况下仍无法安全压入时，错误包含实际大小、限制和“拆分附件或等待 idle 直发”的恢复路径。

**Owner 与边界**

- Owner：control inbox；CLI 只负责 command/completion presentation。
- 不直接删 session 文件，不绕过 durable queue。

**验证**

- clear empty/non-empty；
- paused resume；
- slash completion；
- queued text/image 混合；
- 多张高信息量 PNG 超过单条上限后自动压入、图片顺序与可读尺寸保持、原图不改；
- 不可压缩格式的 typed capacity error。

**退休条件**

上游提供 discoverable queue clear、原子更新 durable inbox 状态，并能按整条消息预算保存多图冻结副本后可退休。

### 11. `feat(capability): add deterministic gateway repair`

**用户问题**

模型会调用旧形态 `MCP action=call` 却缺少 `capability_id`，反复得到相同错误并刷屏停滞。

**最终行为**

- capability 有 canonical/portable identity。
- 对缺失或旧 identity 做确定性解析；唯一候选才修复，歧义时拒绝猜测。
- 参数 envelope 只做窄转换，最多 exact retry 一次。
- 参数错误复用统一的未执行/可恢复诊断与 shared storm breaker；不维护私有失败计数器。
- gateway 继续复用 canonical permission、executor、event 和 receipt，不创建第二套 MCP 执行器。

**Owner 与边界**

- Owner：agent capability resolution/gateway。
- 不开放式猜参数，不根据自然语言任意选工具。

**验证**

- unique/missing/ambiguous identity；
- legacy MCP envelope；
- one retry；
- repeated failure stop；
- permission 与 receipt 不丢失。

**退休条件**

上游同时提供稳定 identity、歧义拒绝、一次修复、loop protection 和统一 receipt 后可退休。

### 12. `feat(agent): recover and maintain oversized context`

**用户问题**

会话曾达到 124% context；本地估算低于 provider 真实计数时，手动 `/compact` 也会被 400 context-limit 拒绝，整个长会话接近不可恢复。

**最终行为**

- 每次 sampling 前继续复用唯一 `ContextManager.Prepare`。
- 一个 assistant tool-call batch 的全部结果配对后、host nudge 前再做一次完整边界 maintenance。
- provider 返回可信 context-limit 时，compact 使用有界 chunk/tree-reduce 缩小输入，而不是原地反复提交同一超长 prompt。
- soft line 以下不调用 summarizer；hard ceiling 维护失败时 fail-closed。
- canonical transcript、session JSONL、checkpoint lineage 和完整 tool result 不删除。

**Owner 与边界**

- Owner：ContextManager；overflow classification 和 post-tool boundary 是同一能力。
- 不创建第二套 token admission，不在 TUI 根据百分比自行 compact。

**验证**

- 90% 前主动维护；
- 单个大型 tool result 跨越物理窗口；
- provider 400 后 chunk recovery；
- full tool batch 边界；
- failure preserves history；
- prefix stability。

**退休条件**

上游在 request 与 complete-tool-batch 两个边界均提供等价 maintenance，并能从 provider 实际 overflow 有界恢复后可退休。

### 13. `feat(cli): make image attachments atomic composer tokens`

**用户问题**

图片占位曾是普通字符串：换行会重复 prompt、删除只能逐字符、点击/选择不一致、Cmd+Z 不能恢复，甚至手写 `[Image #1]` 会被误认成附件。

**最终行为**

- composer 用 stable-ID typed part 保存文本与附件；`[Image #N]` 只是 label。
- 多个附件保持独立身份，编号变化不影响 underlying image。
- 单击附件选中整块；拖动进入普通文字选择，能够复制周边文本。
- Backspace/Delete/按词删除在 token 内或边界时原子删除附件和一个分隔空格。
- Cmd+Z 恢复文本、光标和 attachment mapping；Ctrl+Z 保留 Unix suspend。
- pending、queue、history、rewind 和 submit 都保留 typed parts。
- 手工输入同名 label 不具备 attachment ID，因此永远不会触发 vision。

**Owner 与边界**

- Owner：composer model；selection/rendering 是 consumer。
- vision 只消费已提交 attachment part，不按可见文本查找文件。

**验证**

- click/drag/delete/backspace/word-delete/undo；
- 多图、重复 label、renumber；
- literal `[Image #1]`；
- queue/history/rewind；
- submit 后清理过期 undo。

**退休条件**

上游支持 stable-ID atomic attachment、完整鼠标/删除/撤销和 queue/history 保真后可退休。

## 2 个维护闸门

### 14. `test(tool): benchmark the canonical tool contract`

- 目的：守住一份 canonical ToolKind/schema/executor，检测 schema allocation 和 contract 漂移。
- 它不是产品能力，也不宣称已经实现 DeepSeek/Codex/OpenCode 多方言 A/B。
- 若未来做 dialect projection，只允许变更 name/description/schema/envelope；permission、sandbox、path、execution、event 和 evidence 仍共用 canonical executor。
- 上游已有等价 benchmark 时可合并；不能为了减少提交删掉唯一的回归门。

### 15. `chore(sync): maintain semantic patch stack and structural ratchet`

- 只容纳 replay 脚本、详细 worklog、上游测试环境兼容和代码结构 ratchet。
- 从巨型文件抽出已有 owner，不新增用户行为：
  - disclosure -> `internal/cli/disclosure.go`
  - viewport -> `internal/cli/viewport_projection.go`
  - composer attachment -> `internal/cli/composer_attachment.go`
  - subagent isolation wiring -> agent/boot 专用文件
  - config edit、eventwire usage、serve effort、Desktop channel route -> 各自 owner 文件
- `repolint` 必须证明相对旧长期分支不扩大 complexity/file/function/test-size 债务。
- 维护补丁不得重新收容本应属于前 13 个 owner 的 runtime feature。

## 旧 20 补丁到当前 15 个 owner 的映射

| 旧补丁 | 新 owner | 处理结果 |
|---|---|---|
| 1 terminal title | 3 interactive shell | 保留行为，并入 CLI shell 配置 |
| 2 planner visibility | 1 planner projection | 保留，删除自然语言 no-op 启发式 |
| 3 image pipeline | 2 vision + 13 composer | 保留 pipeline；附件身份迁入 typed composer |
| 4 interactive transcript | 3 shell + 8 transcript + 9 presentation | 拆成配置、语义真源和展示 owner |
| 5 worktree lifecycle | 4 subagent isolation | 保留隔离；删除本地 git-apply 生命周期 |
| 6 usage ledger | 5 usage | 保留完整性/归因；删除本地 repricing 和 UsageModel 双真源 |
| 7 memory recall | 6 memory | 保留 diversity；删除 SourceScope/LastConfirmedAt 双真源 |
| 8 tool benchmark | 14 maintenance test | 移出产品语义计数 |
| 9 strict config | 7 config | 与 provenance 合并，panic API 改为 error |
| 10 disclosures | 9 presentation | 与 transcript/status/viewport 展示合并 |
| 11 queue clear | 10 inbox | 原样保留并固定 autocomplete |
| 12 sync workflow | 15 maintenance | runtime 变更归还 owner，只保留维护资产 |
| 13 canonical transcript | 8 transcript | 提升为唯一语义 projection |
| 14 capability repair | 11 capability | 保留并限制为 deterministic one-retry |
| 15 config provenance | 7 config | 与 authoritative config 合并 |
| 16 overflow recovery | 12 context | 与 post-tool maintenance 合并 |
| 17 post-tool maintenance | 12 context | 合并到唯一 ContextManager owner |
| 18 evidence receipt | upstream operation ledger | v1.38.6 已以更强的 operation-scoped host receipt 覆盖，退休本地顺序 ID 实现 |
| 19 vision footer | 9 presentation | 并入 status projection，不再单独计数 |
| 20 image token | 13 composer | 重写为 stable-ID typed parts |
| owner refactor commits | 对应 1-15 | fixup 到各 owner；结构抽取集中进入 15 |

结果：没有用户能力被按名称粗暴删除；删除的是已被上游更强覆盖的第二套 receipt ID，以及重复真源、错误 apply/repricing、自然语言猜测和散落实现。

## 2026-09-11 v1.38.6 重放判定

- 旧上游：`fa018e4109268c912063c8cc619302fccdb57d74`（v1.38.3）。
- 固定新上游：`e2145b031deef603d9ec1acc9d30f9a1e1ac9325`（v1.38.6），上游区间共 203 个提交。
- 旧长期线：`6980ba7aa37cd556c73c3309ae1be23351b807ce`，19 个提交按序重放为 18 个；仅退休已被上游更强覆盖的旧 evidence receipt 补丁，无 squash、无意外改序。

| # | 语义 owner | 判定 | v1.38.6 集成结果 |
|---|---|---|---|
| 1 | planner projection | 保留 | 上游仍未提供相同的单一可见回答 contract；原补丁机械重放。 |
| 2 | vision | 集成并瘦身 | 采用上游 `internal/imageinput`、`vision_model` 和 remote-first 路由；删除重复 provider sidecar，只保留 macOS 输入归一化、本地 fallback、usage/disclosure 增量。 |
| 3 | interactive shell | 适配 | 跟随上游拆分后的 TUI event handler；保留 presentation、title、composer 和 status 契约。 |
| 4 | subagent isolation | 集成 | 继续复用上游 exact merge 生命周期和 `tool.HostTask`；只保留显式 worktree profile、child-root 装配与恢复 metadata。 |
| 5 | usage | 适配 | 跟随上游 eventwire/model identity 位置；保留 occurrence-time quote、完整性和 CLI/ACP 共用 projection。 |
| 6 | memory | 集成 | 复用上游 freshness/scope owner；只保留 provenance 与 BM25 后 diversity rerank。 |
| 7 | config | 保留并加固 | 接入上游 model runtime snapshot 与 OpenCode Go migration journal；普通 boot 仍严格 no-write，兼容读取后配置字节不变。 |
| 8 | transcript | 集成 | 复用上游 Desktop display buffer、schema-2 session/head 字段；共享 typed transcript 继续是 CLI live/replay owner。 |
| 9 | CLI projection | 适配 | disclosure/status 接到上游 imageinput phase 和 TUI event owner；cached vision 不闪烁，prefix 不含动态字段。 |
| 10 | inbox | 保留 | 上游仍无 `/queue clear` 与对应 slash completion；原补丁机械重放。 |
| 11 | capability | 保留 | 上游 read-evidence gate 不等于 MCP 参数 repair；deterministic one-retry 仍独立且机械重放。 |
| 12 | context | 集成并退休重复逻辑 | 采用上游 transcript -> slim -> chunk 恢复阶梯，删除本地直接分块 helper；保留完整 tool batch 后的主动 maintenance。 |
| 13 | evidence | 退休 | v1.38.6 已提供 host-issued short hash receipt、`operation_id` 绑定、bounded `CitableReceipts`、失败/跨 operation 拒绝和完整测试；旧 `r000001` 顺序 ID 会形成第二真源。 |
| 14 | composer token | 保留 | 上游 clipboard 改进尚未提供 stable-ID 原子附件、点击选择、删除与 Cmd+Z 全 contract；原补丁机械重放。 |
| 15 | tool benchmark | 保留 | 上游仍无等价 canonical contract allocation 基准；不引入 dialect executor。 |
| 16 | maintenance | 重算 | 按 v1.38.6 集成树重建 `repolint` baseline；配置 edit helper 继续使用上游文件布局。 |
| 17 | renderer pin | 暂留 | v1.38.6 的依赖仍未证明完整包含所需 hard-scroll 行修复；只保留单点 replacement，官方依赖包含同一行为后立即退休。 |

本轮主动舍弃的是已被更好上游实现覆盖的重复代码，不是用户行为：旧 vision provider sidecar、旧 context size helper、旧 Desktop/TUI 私有 transcript 表示，以及启动时自动改写配置的测试假设。

## 每次追上游的执行协议

### 1. 固定基线

```sh
git fetch origin
git rev-parse origin/main-v2
git merge-base origin/main-v2 jawa/reasonix-composer-state-visibility
git log --reverse --oneline origin/main-v2..jawa/reasonix-composer-state-visibility
```

只在阶段开始固定一次 upstream。记录 config SHA256、生产二进制版本、主 checkout status 和 worktree inventory。

### 2. 隔离重放

```sh
scripts/jawa-upstream-sync.sh
```

- 从长期线创建仓库内临时 worktree；
- 依序重放当前长期线全部提交；
- 冲突按本台账 owner 判定，不使用整侧 `ours/theirs` 吞并；
- 对每个非机械差异写清上游意图、本地契约和最终选择；
- 使用 `range-diff` 确认只有经过 contract 对比记录的退休项，无意外 squash、无改序。

### 3. 退休检查

逐个问题回答：

1. 上游是否真正覆盖用户 contract，而不只是出现同名 feature？
2. 上游 owner 是否更 canonical，能否删除本地重复实现？
3. live、resume、copy、mouse、queue、headless、ACP 是否都有覆盖？
4. 删除 patch 后相关 contract tests 是否仍绿？
5. diff 是否减少，而不是把同一行为挪进维护补丁？

只有五项都成立才退休。

### 4. 完整门禁

```sh
git diff --check
scripts/verify-tui-regression.sh
go vet ./...
go test -count=1 ./...
(cd desktop && go test -count=1 ./...)
make build
./bin/reasonix --version
./bin/reasonix doctor --json
./bin/reasonix doctor capabilities --json --root .
```

还必须执行真实 smoke：

- live/new/resume transcript；
- Thought 与 Image disclosure 点击、hover、选择、向上展开；
- typed image paste、多图、delete、Cmd+Z、queue；
- scroll-away/new-message/Ctrl+End/pill；
- Headless JSON/NDJSON 和 ACP；
- writer worktree clean/conflict/recovery；
- context maintenance/overflow recovery；
- memory old/new/no-write/diversity；
- config SHA256 前后不变。

### 5. 收口

- 全绿后才移动唯一长期分支、推送和安装。
- 更新长期线使用 `--force-with-lease`，保留安全 tag；禁止 `reset --hard`。
- 删除已完成的临时 worktree/branch。
- 最后只读检查一次 upstream drift；有新提交只报告，不在交付尾部自动再追一轮。

## 已知风险

1. CLI presentation 与上游 TUI 活跃区重叠最多，追更冲突概率最高；必须靠 typed contract/golden，而不是截图印象。
2. Vision 的 macOS clipboard 兼容属于平台增量；非 macOS 必须无副作用。
3. Worktree 功能使用频率低但破坏面大，不能因为 smoke 较少就降低 exact merge 测试。
4. Canonical tool benchmark 当前仍是微基准；未有稳定 A/B 收益前不增加 dialect maintenance surface。
5. Desktop 不是用户生产入口，但共享 core adapter 仍需编译和测试，不能删除上游 Desktop 源码来“简化”CLI。
