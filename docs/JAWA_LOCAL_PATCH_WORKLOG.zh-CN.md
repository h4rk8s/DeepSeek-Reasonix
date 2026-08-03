# Jawa 本地 Reasonix Patch Work Log

更新时间：2026-07-24
维护分支：`jawa/reasonix-composer-state-visibility`
当前同步入口：`scripts/jawa-upstream-sync.sh`（默认生成临时 worktree / 临时分支）
本轮固定上游：`a6e145962184de445de5bdb7e16339a14547c2e0`
本轮审计候选（重建前）：`d77f03a0c09a1c6379a907693b59f13149abac6f`
当前 patch stack：以固定上游为基线重建；不再把已经被上游覆盖的基础 TUI/图片能力作为独立 patch 维护。
完整列表始终以 `git log --reverse origin/main-v2..HEAD` 和同步时实际 merge-base 为准，不再维护容易过期的手抄 SHA 列表。

这份文档不是上游产品文档，而是本地分支维护台账。它记录的是：上游 `main-v2`
目前没有、但本地使用已经依赖的功能修复。每个 patch 都应该有明确的删除条件：
如果上游实现了等价能力，并且本地习惯可以迁移过去，就应该删除本地 patch，而不是长期
重复维护两套逻辑。

## 维护原则

1. 本地分支只保留一条集成线：`jawa/reasonix-composer-state-visibility`。
2. 跟进上游时优先 rebase/线性化，保持 `origin/main-v2 + 本地 patch` 的结构。
3. 每次升级上游后，先看 `git diff origin/main-v2...HEAD`，逐项判断下面的 patch 是否仍然需要。
4. 本地 patch 应尽量通过配置开关启用，不引入外部 wrapper，不依赖用户手工改 shell。
5. 不把个人运行产物、研究文档、临时导出塞进 patch。当前 `research/` 是未跟踪目录，不属于本地功能 patch。
6. 能用上游机制承载的地方优先复用上游机制，例如 transcript、config render、slash command、TUI mouse event。
7. 新增 patch 必须写清楚删除条件。上游一旦实现等价能力，优先删除本地 patch，而不是再叠一层兼容。
8. 不允许因为配置解析失败就用默认配置继续保存。配置坏了应该明确报错，不能静默覆盖用户本地文件。

## 唯一长期维护线

Reasonix 本地只维护一条长期集成线：

```text
origin/main-v2 + 全部仍有必要的本地 patch
                    |
                    `- jawa/reasonix-composer-state-visibility
```

- 主 checkout 固定为 `/Users/jawa/Lab/2026-07-06-reasonix-dev`。
- `jawa/reasonix-composer-state-visibility` 是唯一会持续追上游、构建候选和安装生产版本的分支。
- 过去用于隔离开发的 `jawa/reasonix-core-optimization` 和用于重放验证的
  `jawa/reasonix-core-integration` 已完成使命并删除，不再是第二、第三条维护线。
- 为上游 PR 保留的短期分支只是 review/CI 载体，不承担本地生产维护，也不能作为下一轮 upstream sync 的起点。
- 临时 sync worktree/branch 只用于验证；成功后结果回到唯一长期线，失败则删除或保留为故障现场，绝不形成长期分叉。
- 本地安全 tag 可以保留旧指针用于紧急回退，但 tag 不是维护分支。

未来运行 `scripts/jawa-upstream-sync.sh` 时，脚本从当前长期分支计算
`merge-base..HEAD`，因此会重放长期线上的**完整补丁栈**，包括下面的 Core 7 个补丁，
而不是只重放早期的 14 个补丁。每次同步仍需用 `range-diff` 核对补丁映射，不能只看 rebase 成功。

## 跟进上游的低成本流程

本地分支按“上游 main-v2 + 本地 patch stack”维护。每天或每次上游有大批提交时，不要重新理解整棵树，
只按下面的流程做：

### 0. 推荐入口：自动化同步脚本

优先使用脚本，不要直接在生产 worktree 上手工试错：

```sh
scripts/jawa-upstream-sync.sh
```

默认行为：

- `fetch origin/main-v2`
- 从当前本地分支创建临时 worktree
- 在临时 worktree 里 rebase 到上游最新
- 跑快速本地验收：TUI 回归、定向 Go 测试、`make build`、`reasonix --version`
- 成功后删除临时 worktree
- 不安装、不推送、不改 `~/.reasonix/config.toml`

确认临时同步通过后，再显式进入生产：

```sh
scripts/jawa-upstream-sync.sh --apply --install --push
```

含义：

- `--apply`：把当前生产 checkout 移到已验证的新 head
- `--install`：重新构建并安装到 `~/.local/bin/reasonix`
- `--push`：用 `--force-with-lease` 推送本地集成分支到 `h4rk8s`

大批上游提交或准备发版前，加完整测试：

```sh
scripts/jawa-upstream-sync.sh --full-test --apply --install --push
```

如果 rebase 冲突，脚本会保留临时 worktree 并打印路径；冲突只在那个目录里处理，不影响当前可用的
`~/.local/bin/reasonix`。

### 1. 开启冲突记忆

每个 worktree 都建议启用 `rerere`，让 Git 记住已经解决过的冲突。这样同一类冲突第二次出现时可以自动套用。

```sh
git config rerere.enabled true
git config rerere.autoupdate true
```

这是降低日常 rebase 成本的关键。它不替代 review，只是把重复冲突从“重新手解”变成“自动套用 + 人审”。

### 2. 只在临时同步 worktree 里追上游

不要直接在生产 worktree 上试错。推荐模式：

```sh
git fetch origin
git worktree add ../2026-07-10-reasonix-upstream-sync jawa/reasonix-composer-state-visibility
cd ../2026-07-10-reasonix-upstream-sync
git switch -c jawa/reasonix-upstream-sync-$(date +%Y%m%d)
git rebase origin/main-v2
```

如果需要重试，直接丢掉这个临时 worktree，不影响生产可用的 `~/.local/bin/reasonix` 和主开发目录。

### 3. 用 patch 分组判断冲突，而不是逐文件猜

冲突文件先归类：

| 冲突区域 | 归属 patch | 判断方式 |
|---|---|---|
| `internal/cli/chat_tui*.go`, `transcript.go`, `theme.go` | lazy reasoning / transcript UI / statusbar / paste | 先保留上游结构，再恢复本地交互语义 |
| `internal/control/image_understanding*`, `refs.go` | image understanding sidecar | 保留“不内联图片字节、不破坏 cache prefix”的设计 |
| `internal/agent/coordinator.go` | dual-model coordinator hygiene | planner 文本不直接刷 transcript，executor handoff 要可恢复原始任务 |
| `internal/config/*`, `reasonix.example.toml` | config safety / render | 配置解析失败必须报错，不能用默认值覆盖 |
| `cmd/reasonix/main.go`, `Makefile`, `internal/cli/buildinfo.go` | build metadata | 保留 build time / git commit / dirty / target / config path |
| `docs/*`, `internal/i18n/*` | docs/i18n | 跟 runtime patch 同步增删，不保留孤儿字段 |

冲突解决的默认策略：

1. 先看上游有没有实现等价能力。
2. 如果上游等价能力更完整，删本地 patch，并在本文件标记 retired。
3. 如果上游只是改结构但没有覆盖本地需求，保留上游结构，把本地语义移植进去。
4. 如果上游和本地都只是样式差异，优先保留更少代码、更 native 的路径。
5. 解决后必须跑对应 patch 的定向测试，再跑 `go test -count=1 ./...`。

### 4. rebase 后固定验收矩阵

```sh
go test -count=1 ./internal/cli
go test -count=1 ./internal/config ./internal/control ./internal/agent ./internal/boot ./internal/installsource
go test -count=1 ./...
make build
./bin/reasonix --version
```

生产安装前再确认：

```sh
~/.local/bin/reasonix --version
```

### 5. 可选导出 patch 包

需要给另一台机器或另一个 worktree 快速重放时，可以在本地导出补丁包。这个目录是维护辅助产物，不建议提交。

```sh
rm -rf .local-patches
mkdir -p .local-patches
git format-patch --no-stat --output-directory .local-patches origin/main-v2..HEAD
```

如果某天 rebase 复杂度太高，可以先在干净上游上用 `git am` 重放这组 patch，定位到底是哪一个 patch
开始冲突，再逐个拆解。

## 当前差异概览

### 2026-07-24 补丁去重审计

本轮不是按旧提交名机械保留，而是逐项比较固定上游 `a6e145962` 与本地候选
`d77f03a0c` 的真实协议、代码和测试。判定如下：

| 能力 | 判定 | 上游现状 | 本地处理 |
|---|---|---|---|
| 完成后把 thinking 折叠为 `Thought for Ns` | RETIRE | 上游已有完成态折叠、verbose reasoning 切换和 transcript hierarchy | 删除“基础折叠”独立 patch；只保留逐块点击、hover、锚点和 resume 状态一致性 |
| 基础剪贴板图片粘贴与 `[image #N]` | RETIRE | 上游已有跨平台 clipboard image、模型 vision gating、text-only 可读图片引用 | 删除“基础图片粘贴”独立 patch |
| CleanShot/Raycast/path/HTML/Markdown/硬换行图片来源 | KEEP-DELTA | 上游路径解析仍未覆盖本地真实剪贴板来源组合 | 作为上游 paste pipeline 的兼容输入层，不另造附件系统 |
| OCR + UI state 的 image-understanding sidecar | KEEP | 上游没有 cache-friendly 的结构化理解、缓存、日志和 disclosure | 保留独立内核 sidecar 与可折叠 transcript disclosure |
| 基础 composer 鼠标、caret、selection | RETIRE | 上游已修复 mouse selection、caret stability 和 paste/image input 分层 | 删除基础交互 patch，只保留本地 prompt 首行和 transcript disclosure 的增量测试 |
| 基础响应式 status UI | RETIRE | 上游已有 responsive status UI | 删除基础布局 patch；只保留本地两行信息架构、字段优先级和半屏密度 |
| CLI jump-to-bottom/new-message pill | KEEP | 上游只有 Desktop 对应交互，CLI 尚无等价实现 | 保留 CLI 原生 transcript/composer 接线 |
| terminal title 与 `/title` | KEEP | 上游没有 CLI terminal title 字段组合和 picker | 保留 |
| build metadata | KEEP | 上游 `--version` 仍不满足本地 build time/commit/dirty/target 规范 | 保留 |
| dual-model transcript/resume hygiene | TRIM | 上游已有部分 turn hierarchy，但没有本地 handoff 隐藏、重复回答和 resume parity 契约 | 只保留缺口，不覆盖上游通用 renderer |
| config 编辑失败后回落默认值 | KEEP-TRIM | 上游已有 private strict loader，但 public edit loader 仍可 fallback；迁移语义已更完整 | 复用上游 strict loader；读/编辑不自动写盘，显式迁移才持久化，解析失败直接终止 |
| config render/docs/i18n | CONSOLIDATE | 部分字段由仍保留能力需要，不能独立存在 | 并入对应 runtime patch，不再单列为功能 patch |
| upstream sync helper | KEEP | 上游不会提供用户私有 patch-stack workflow | 保留为仓库内维护工具 |
| Core Optimization 7 patches | KEEP | 上游仍无等价 worktree isolation lifecycle、fail-closed usage ledger、memory provenance/diversity 和 canonical contract benchmark | 保持顺序和协议边界，继续独立维护 |

上游替代证据包括：

- `40ef98de`：CLI terminal interactions 与 transcript hierarchy。
- `1bd5f04d`、`a99256d5`、`fc5d8122`、`8eb9707a`：composer、responsive status、mouse selection、caret stability。
- `e17f31b7`：paste/image input 分层。
- `7da9b0ac`、`1b2a4659`：text model 可读图片与 vision capability gating。
- `21938cd9`、`fbaaaea0`、`485c16d8`、`224bbfa6`：图片路径和附件处理修复。

这意味着旧的 22 个提交不会原样继续滚动。重建后的维护线只表达“上游固定基线 + 尚未被覆盖的本地增量”，
已退休能力如果以后再次出现在 diff 中，应视为回归，而不是重新恢复旧实现。

相对 `origin/main-v2`，本地 runtime/code patch 当前改动（不含本文档本身）：

- 69 个文件
- 约 7571 行新增，587 行删除
- 主题集中在 CLI/TUI、配置、图片理解 sidecar、终端标题、状态栏、版本构建信息

主要文件组：

- `internal/cli/chat_tui.go`
- `internal/cli/chat_tui_paste.go`
- `internal/cli/terminal_title.go`
- `internal/cli/title_picker.go`
- `internal/cli/buildinfo.go`
- `internal/control/image_understanding.go`
- `internal/control/image_understanding_command.go`
- `internal/config/config.go`
- `internal/config/render.go`
- `cmd/reasonix/main.go`
- `Makefile`
- `reasonix.example.toml`
- `docs/CONFIG_PATHS*.md`
- `docs/GUIDE*.md`
- `docs/SPEC.md`

## Patch 1：终端标题与 `/title`

### 上游缺口

上游有会话重命名、session metadata、desktop/sidebar title 等能力，但 CLI 里没有一个完整的
terminal/Ghostty tab title 体系。具体缺口是：

- `/rename` 能改 session title，但不会稳定同步到 Ghostty tab title。
- 没有 `/title` 交互配置入口。
- 没有按字段组合 terminal title 的配置，例如 activity、session title、todo progress、model、cwd、git branch。
- running/thinking/approval/picker 等 TUI 状态没有持续反映到 tab title。
- 用户无法像 Codex/Claude 一样让 terminal tab 一眼显示当前线程状态。

### 本地补的能力

本地增加了一套 Reasonix-native 的 terminal title 机制：

- CLI 启动后维护 `chatTUI.windowTitle`，并通过 OSC title 控制序列更新终端标题。
- `/rename <title>` 改当前 session title 后，会同步刷新 terminal title。
- 新增 `/title` slash command，打开 terminal title 字段选择器。
- `/title` 是 modal picker，不作为普通 prompt 发给模型。
- `/title` 支持在模型运行中打开，不会把输入变成 pending interject。
- `/title` 保存到用户配置，不重写项目配置。
- terminal title 文案不是照抄 Codex，而是按 Reasonix 状态命名：
  - activity
  - session-title
  - todo-progress
  - mode
  - model
  - effort
  - context
  - balance
  - app-name
  - project-name
  - current-dir
  - run-state
  - git-branch

### 关键实现

- `internal/cli/terminal_title.go`
  - 根据当前 TUI 状态、session metadata、usage/cache/context、cwd、branch 等字段生成 title。
  - 将长字段截断，避免 Ghostty tab 被过长内容挤爆。
  - 处理 picker 状态、thinking 状态、approval 状态。
- `internal/cli/title_picker.go`
  - `/title` 的交互选择器。
  - 支持上下移动、Space toggle、Enter 保存、Esc 关闭。
  - 保存 `[terminal_title].items`。
- `internal/cli/chat_tui.go`
  - 将 `/title` 接入 slash command。
  - 在 TUI lifecycle、状态变化和 metadata 变化时刷新 title。
- `internal/config/config.go`
  - 增加 `TerminalTitleConfig` 和字段校验。
- `internal/config/render.go`
  - `reasonix config show/edit` 能渲染 `[terminal_title]`。
- `internal/i18n/messages_*.go`
  - 增加 `/title` 文案。

### 配置入口

示例：

```toml
[terminal_title]
items = ["activity", "session-title", "todo-progress"]
```

如果配置缺省，使用 Reasonix 默认字段顺序。字段为空时会自动跳过，例如没有 git branch 时不显示
`git-branch`。

### 验收点

- `reasonix /title` 能打开 picker。
- 选择字段后保存到 `~/.reasonix/config.toml`。
- `/rename xxx` 后 Ghostty tab title 能刷新。
- 模型 thinking、approval、picker、idle 等状态能反映到 title。
- 项目级 `reasonix.toml` 存在时，`/title` 不应该误改项目配置。

相关测试：

- `internal/cli/title_picker_test.go`
- `internal/cli/rename_test.go`
- `internal/config/render_test.go`

### 删除条件

如果上游实现了：

1. terminal title 的 OSC 更新；
2. `/title` 或等价配置 UI；
3. `/rename` 后同步 terminal title；
4. 字段可配置；
5. running/thinking/approval 状态能进入 title；

并且本地配置可以迁移过去，则删除本 patch。删除时重点看：

- `internal/cli/terminal_title.go`
- `internal/cli/title_picker.go`
- `internal/config/config.go` 里的 `TerminalTitleConfig`
- `/title` slash command 接入点

## Patch 2：lazy reasoning，完成后折叠 thinking，点击再展开

### 上游缺口

上游 CLI 可以展示 thinking 中的过程，但完成后只留下 `thought for Ns` 这类摘要，用户无法按需回看
完整推理内容。对本地使用来说，这个信息很有价值：

- 可以审查模型为什么误判。
- 可以复制某段 reasoning 给另一边对话或 PSA。
- 可以看出 planner/executor 的分工是否合理。
- 可以定位“模型没用工具”“模型误读上下文”等问题。

但全文默认展开又很干扰视线，尤其是长任务会把 transcript 撑得很乱。因此本地目标不是
`verbose always on`，而是：

- 默认折叠；
- 保留完整内容；
- 点击感兴趣的 `Thought for Ns` 再展开；
- 再点击同一区域隐藏；
- 尽量学习 Claude 的视觉锚点：展开时不要把用户当前关注的下方回答挤走。

### 本地补的能力

本地增加 `[ui].lazy_reasoning`：

```toml
[ui]
lazy_reasoning = true
```

也支持环境变量临时覆盖：

```sh
REASONIX_LAZY_REASONING=1 reasonix
```

当前设计：

- thinking 流式生成时仍可见，便于实时判断模型是否跑偏。
- 回合完成后，将 thinking 正文折叠为一行摘要，例如 `Thought for 9s`。
- 完整 thinking 内容保留在 TUI transcript 数据结构中。
- 鼠标点击摘要行时展开正文。
- 展开正文使用背景色块，和普通回答区分。
- 展开/隐藏尽量保持下方回答的视觉锚点不跳。
- 已修复一次点击后 crash 的 index 越界问题。
- 已多轮调整展开块、回答块、gutter 的对齐，避免输入和输出块左右漂移。

### 关键实现

- `internal/cli/chat_tui.go`
  - 保存 completed reasoning body。
  - 维护 folded/expanded 状态。
  - 处理 mouse click、hover、scroll anchor。
  - 渲染 folded summary 和 expanded body。
- `internal/cli/transcript.go`
  - transcript entry 增加 reasoning body 相关承载。
- `internal/cli/chat_render_test.go`
  - 覆盖折叠/展开渲染。
- `internal/cli/chat_tui_test.go`
  - 覆盖点击、折叠、锚点、输入状态等 TUI 行为。
- `internal/config/config.go`
  - 增加 `UI.LazyReasoning`。
- `internal/config/render.go`
  - config show/edit 能渲染 `lazy_reasoning`。

### 已知维护风险

这个 patch 最容易和上游 TUI 改动冲突，因为它碰到了 transcript rendering、mouse event、
scroll offset、active composer 区域。升级上游时重点检查：

- upstream 是否改了 `chatTUI.View()` / transcript render pipeline；
- upstream 是否改了 mouse mode；
- upstream 是否新增自己的 reasoning 展开机制；
- 点击展开后是否仍然保持“下方回答不被往下顶”；
- 鼠标移动、选择文本、复制文本时是否会误触发折叠。

### 删除条件

如果上游实现了等价的 reasoning-on-demand：

1. 完成后默认折叠；
2. 点击后展开完整 reasoning；
3. 再点击隐藏；
4. 可配置开关；
5. 展开时不破坏阅读锚点；
6. 可以复制展开内容；

则删除本 patch。删除时重点看：

- `chat_tui.go` 中 reasoning line/body 的状态字段
- `[ui].lazy_reasoning`
- `REASONIX_LAZY_REASONING`
- 相关 render/click 测试

## Patch 3：图片粘贴、图片引用和 cache-friendly image understanding sidecar

### 上游缺口

DeepSeek 当前主模型链路偏 text-only。直接把图片作为多模态消息喂给主模型，会破坏 Reasonix
最核心的 cache-first 设计；完全不处理图片又会导致用户截图粘贴后只得到本地路径，体验和
Codex/Claude 差很多。

本地实际需求是：

- 复制截图到终端时，输入框里显示 `[Image #1]`，而不是一长串 CleanShot 路径。
- 提交时不要把二进制图片塞进主模型上下文。
- 对图片只做一次 OCR/VLM sidecar 理解，生成稳定文本。
- 主模型看到的是稳定文本摘要，因此不破坏 KV cache prefix。
- 可以用 LM Studio 或本地命令统一管理视觉模型，不让模型散落各处。

### 本地补的能力

本地增加了图片处理链路：

- 识别用户粘贴的本地图片路径，包括 shell-escaped 路径。
- 在 TUI 输入框中折叠显示为 `[Image #1]`。
- 在提交时把图片路径转为 attachment/ref。
- 如果配置了 `image_understanding_command`，调用本地 sidecar 生成结构化理解文本。
- 如果配置了 `image_understanding_model`，检查模型是否存在、是否标记为 vision capable。
- sidecar 输出目标格式是 cache-friendly 文本，例如：

```xml
<image-understanding source="@.reasonix/attachments/xxx.png" sha256="...">
visible_text: ...
ui_state: ...
errors: ...
layout: ...
confidence: ...
</image-understanding>
```

这个设计的关键点是：图片只在 sidecar 被理解一次，主 agent 仍吃文本，保持 DeepSeek/Reasonix
的缓存收益。

### 关键实现

- `internal/cli/chat_tui_paste.go`
  - 识别粘贴的图片路径。
  - 支持 CleanShot 这类带空格、反斜杠转义的路径。
- `internal/cli/chat_attachment_test.go`
  - 覆盖图片路径折叠与附件行为。
- `internal/control/image_understanding.go`
  - image understanding 的控制层逻辑。
- `internal/control/image_understanding_command.go`
  - 外部命令 sidecar 执行。
  - 接收图片路径，解析输出，注入 turn context。
- `internal/control/image_understanding_command_test.go`
  - 覆盖命令执行、错误路径、输出处理。
- `internal/control/refs.go`
  - 让 attachment/ref 能被 control 层解析。
- `internal/boot/boot.go`
  - 启动时检查 image understanding model/command 配置。
  - 配置不可用时发 warning，不中断主流程。
- `internal/config/config.go`
  - 增加：
    - `agent.image_understanding_model`
    - `agent.image_understanding_command`
- `internal/config/render.go`
  - config show/edit 能渲染上述字段。

### 配置入口

```toml
[agent]
image_understanding_command = "reasonix-vision-ocr"
# image_understanding_model = "provider/vision-model"
```

本地推荐优先使用 command sidecar，因为它可以统一走 LM Studio、MLX、OCR 小工具或未来更快的
屏幕理解服务，不把图片处理硬编码到 Reasonix 主循环里。

### 删除条件

如果上游实现了 cache-friendly image pipeline：

1. 粘贴图片时输入框显示 `[Image #1]`；
2. 主模型是 text-only 时可以自动 OCR/VLM sidecar；
3. sidecar 结果以稳定文本注入；
4. 不破坏 prompt cache prefix；
5. 支持本地命令或本地 OpenAI-compatible vision backend；

则删除本 patch。删除时重点看：

- `image_understanding_*`
- `chat_tui_paste.go` 里的 image path recognition
- `agent.image_understanding_*` 配置项
- boot-time vision gating

## Patch 4：状态栏、输入光标和使用量展示习惯

### 上游缺口

上游状态栏默认信息结构和本地 Codex/Claude 使用习惯不完全一致。本地偏好是：

- 不要在 transcript 里反复刷每轮 token/cache/cost 行；
- 底部状态栏承担主要信息密度；
- 半屏 Ghostty 下优先显示最关键字段；
- 模型名、planner、ctx、cache hit、compact headroom 需要压缩展示；
- 输入光标要能配置，避免默认形态和 Ghostty/CJK 渲染冲突。

### 本地补的能力

本地增加或校准了这些 UI 配置：

```toml
[ui]
cursor_shape = "block"      # block|underline|bar
show_usage = false          # 隐藏 transcript 里的 per-turn token/cache/cost 行
lazy_reasoning = true
```

状态栏侧的本地习惯：

- 能显示 executor + planner 的双模型结构，例如 `DS v4 flash · plan pro`。
- 能显示当前 ctx 占用，例如 `ctx 30%`。
- cache 命中信息用短字段显示，例如 `hit 99.8%` / `avg 99.8%`。
- 如果 provider 没有给 cache token，不伪造 `hit 0.00%`。
- 路径和分支做截断，避免半屏换行。
- `show_usage=false` 时，transcript 不再重复展示每轮 usage 行，减轻视觉噪音。

### 关键实现

- `internal/config/config.go`
  - `UI.CursorShape`
  - `UI.ShowUsage`
  - `UI.LazyReasoning`
- `internal/cli/theme.go`
  - 根据 cursor shape 渲染输入光标。
- `internal/cli/chat_tui.go`
  - 状态栏内容、使用量展示、cache 展示、model/planner display。
- `internal/cli/statusline_test.go`
  - 覆盖状态栏显示。
- `internal/config/render.go`
  - config show/edit 能渲染相关 UI 项。
- `reasonix.example.toml`
  - 给出可发现的配置示例。

### 删除条件

如果上游状态栏提供：

1. 可配置 cursor shape；
2. 可隐藏 transcript usage 行；
3. 双模型 compact 展示；
4. cache hit/avg 正确展示；
5. 半屏下不会把关键信息挤到第三行；

则删除或缩小本 patch。删除时重点看 `chat_tui.go` 的 statusline 渲染和 `config.go` 的 UI 字段。

## Patch 5：详细 build metadata，修复 `reasonix --version` 太稀疏

### 上游缺口

上游或普通本地构建的输出过于简单：

```text
reasonix dev
build_target: aarch64-apple-darwin
go: go version go1.26.4 darwin/arm64
config_path: /Users/jawa/.reasonix/config.toml
config_mode: user
```

这对本地长期维护不够。需要像本地 Rust CLI 一样，能直接回答：

- 这个 binary 是什么时候构建的？
- 对应哪个 git commit？
- 构建时源码是否 dirty？
- 是 release 还是 debug？
- 构建目标是什么？
- 用的 Go 版本是什么？
- 读的是哪个配置文件？

### 本地补的能力

现在 `reasonix --version` 输出类似：

```text
reasonix desktop-v1.17.7-39-g9d4d1487
build_number: 20260708060905
build_time_utc: 2026-07-08T06:09:05Z
build_time_cst: 2026-07-08 14:09:05 CST
git_commit: 9d4d1487
git_dirty: clean
build_profile: release
build_target: aarch64-apple-darwin
go: go version go1.26.4 darwin/arm64
config_path: /Users/jawa/.reasonix/config.toml
config_mode: user
```

另外做了普通 `go build` fallback：

- 没有 ldflags 时仍显示 VCS commit、dirty、profile、target。
- 没有 build time 时明确显示 `build_time_utc: unknown`，不伪造时间。
- `Makefile` build 时注入真实 `build_number` 和 `build_time_utc`。
- `git_dirty` 判断忽略未跟踪文件，避免 `research/` 这类本地资料让 release binary 误报 dirty。

### 关键实现

- `internal/cli/buildinfo.go`
  - `BuildInfo`
  - `VersionText`
  - UTC -> CST 格式化
  - Go VCS build info fallback
  - target triple fallback，例如 `darwin/arm64 -> aarch64-apple-darwin`
- `internal/cli/buildinfo_test.go`
  - 覆盖 release build 字段和 plain `go build` fallback。
- `cmd/reasonix/main.go`
  - 接收 ldflags 注入字段，传给 CLI。
- `Makefile`
  - 注入：
    - `main.version`
    - `main.buildNumber`
    - `main.buildTimeUTC`
    - `main.gitCommit`
    - `main.gitDirty`
    - `main.buildProfile`

### 删除条件

如果上游 `--version` 已经包含：

1. build number；
2. UTC/CST 或等价 build time；
3. git commit；
4. git dirty；
5. build profile；
6. build target；
7. compiler/runtime version；
8. config path/mode；

则删除本 patch，或者仅保留本地字段命名兼容层。

## Patch 6：双模型 coordinator 卫生，避免 planner/executor 重复污染 transcript

### 上游缺口

Reasonix 的双模型结构是本地重度使用路径：`deepseek-v4-flash` 做 executor，`deepseek-v4-pro`
做 planner。这个结构的收益是快、便宜、cache hit 高；风险是 planner 和 executor 都可能输出
面向用户的文本，导致 transcript 里出现两份回答，或者 resume 后把 handoff 指令当成用户真实输入展示。

本地不能接受的行为：

- planner 流式文本直接刷到 transcript，然后 executor 又回答一遍。
- planner 的 `# Reasonix executor handoff`、`Planner output`、tool schema 等交接文本在 resume 后作为普通用户输入出现。
- 用户只是问候或问“你是谁”时，planner 已经给出足够回答，executor 又强行重复一轮。
- CLI 显式 `--model xxx` 时，仍被默认 `planner_model` 包成双模型，导致“我想单模型试用”不成立。

### 本地补的能力

当前本地 patch 保留双模型能力，但收紧边界：

- planner 的 text chunk 只进入 planner buffer，不直接 emit 到用户 transcript。
- executor handoff 提取时先剥离 transient user blocks，再从 marker 处恢复 `Original task`。
- 如果 planner 结论是 no-op 或问候类无任务回答，直接把 planner 输出作为最终回答显示，不再启动 executor。
- CLI/session 层显式传入 `--model` 时，尊重这个模型作为单模型 controller，不再自动叠加配置里的 planner。

### 关键实现

- `internal/agent/coordinator.go`
  - `plan()` 中 planner text 不再直接 `sink.Emit(event.Text)`。
  - `HandoffTask()` 允许 marker 前有 transient block，并二次剥离 transient block。
  - `Run()` 对 no-op / greeting planner conclusion 做直接输出。
- `internal/agent/coordinator_test.go`
  - `TestCoordinatorDoesNotStreamPlannerTextToSink`
  - `TestHandoffTaskRecoversOriginalInput`
  - `TestCoordinatorNoOpPlannerConclusionIsVisible`
  - `TestCoordinatorSkipsExecutorForGreetingPlannerNoTask`
- `internal/boot/boot.go`
  - `opts.Model != ""` 时清空本轮 `plannerModel`，避免显式单模型被默认双模型包装。

### 删除条件

如果上游实现了等价的 dual-model transcript hygiene：

1. planner text 不直接污染用户 transcript；
2. handoff/replay/resume 能恢复原始用户任务；
3. no-op/greeting plan 不触发 executor 重复回答；
4. 显式 `--model` 能保持单模型语义；

则删除本 patch。删除时重点看 `internal/agent/coordinator.go` 和 `internal/boot/boot.go`。

## Patch 7：配置安全，不允许 load 失败后回落默认并保存

### 上游缺口

上游有些配置编辑路径会走“加载失败 -> 默认配置 -> 后续保存”的模式。对本地使用来说，这是高风险行为：

- 用户的 `~/.reasonix/config.toml` 很复杂，包含 yolo、sandbox、providers、planner/executor、image understanding 等习惯配置。
- 如果解析失败、路径异常或权限异常时继续用默认配置保存，会把用户配置重置成默认态。
- 本地原则是：配置坏了就明确报错，不能假装没事继续写。

### 本地补的能力

当前本地 patch 保留上游的正常迁移逻辑，但把“编辑/安装来源”这类会写配置的路径改成 strict load：

- `LoadForEditStrict` 失败时直接返回错误。
- `install-source` 安装 skill root / MCP / remove MCP / remove skill root 前先 strict load。
- 配置 load 失败时不连接 MCP，不触发外部副作用。
- 保存失败时 rollback 新连上的 MCP。
- legacy provider tier migration 只在内存迁移，不在普通 `Build()` 时重写用户配置文件。

### 关键实现

- `internal/installsource/apply.go`
  - `applySkillRoot`
  - `applyInstallMCP`
  - `applyRemoveSkillRoot`
  - `applyRemoveMCP`
  - 上述路径全部从 `LoadForEdit` 改为 `LoadForEditStrict`。
- `internal/installsource/install_source_test.go`
  - `TestApplyMCPRollsBackOnSaveFailure`
  - `TestApplyMCPConfigLoadFailureDoesNotConnect`
- `internal/boot/boot_test.go`
  - legacy `tier = "eager"` / `tier = "lazy"` 测试确认 Build 只做内存迁移，不重写磁盘配置。

### 删除条件

如果上游把所有会写配置的路径都改成 strict load，并明确保证：

1. load/parse/path failure 不会生成默认配置覆盖用户文件；
2. 外部副作用发生前先完成配置可写性检查；
3. 保存失败能 rollback 已连接 MCP；
4. 自动迁移不会在普通启动时改写用户文件；

则删除本 patch。

## Patch 8：配置渲染、文档和 i18n 对齐

### 上游缺口

新增本地功能如果只改 runtime，不改 config render/docs/i18n，会导致两个问题：

- 用户不知道有哪些配置项。
- `reasonix config show/edit` 不能 round-trip 本地配置。

### 本地补的能力

围绕上述 patch，同步补了：

- `docs/CONFIG_PATHS.md`
- `docs/CONFIG_PATHS.zh-CN.md`
- `docs/GUIDE.md`
- `docs/GUIDE.zh-CN.md`
- `docs/SPEC.md`
- `reasonix.example.toml`
- `internal/i18n/messages_en.go`
- `internal/i18n/messages_zh.go`
- `internal/i18n/messages_zh_tw.go`

目前文档覆盖：

- `[ui].cursor_shape`
- `[ui].show_usage`
- `[ui].lazy_reasoning`
- `[terminal_title].items`
- `[agent].image_understanding_model`
- `[agent].image_understanding_command`

### 删除条件

当某个 runtime patch 删除时，必须同步删除对应文档、示例配置和 i18n。不要留下孤儿配置项。

## Core Patch 9：冻结内核契约基线

提交：`498207bb test(core): freeze optimization baseline contracts`

### 为什么保留

这不是用户可见功能，而是后续 Core patch 的防回归底座。它把改造前容易被误解的行为固定成测试证据：

- writer-capable background subagent 默认共享 parent workspace，尚无隔离；
- Headless 未知 pricing 会看起来像零成本，且缺少完整性字段；
- 旧 memory 文件没有 provenance/staleness metadata，BM25 结果可能被近重复项占满；
- subagent Profile 对未知 frontmatter 的保留与拒绝边界。

这些测试随后被新协议更新为正向验收，确保变化是有意识的 contract migration，而不是只凭 TUI 观感。

### 删除条件

只有当上游已有覆盖相同协议边界的稳定 contract tests，而且字段、默认值和兼容语义一致时，才能合并或删除。

## Core Patch 10：可复用 Worktree Lifecycle Manager

提交：`da59d576 feat(worktree): add reusable lifecycle manager`

### 本地补的能力

把原来只服务 Desktop Delivery 的 `internal/worktree` 基础能力提升为内核 lifecycle service，统一管理：

- `Inspect` / `Create` / `Show` / `List`；
- `Status` / `Diff`；
- `Apply`；
- `Remove` / `Discard`；
- orphan resource `GC`。

每份隔离资源有结构化身份：isolation ID、source/worktree root、branch、base/head commit、source dirty、
lifecycle/cleanup state 和 created time。Desktop Delivery 仍保持 durable、never-auto-delete；subagent 使用不同的显式 policy，
没有偷改原有 Delivery 语义。

### 安全边界

- worktree 不是 sandbox，不能代替 permission、folder trust 或 hook trust。
- source dirty 默认拒绝；显式 committed-head policy 才允许忽略未提交改动，且必须暴露警告。
- 不擅自复制 dirty/untracked 文件，不把“不完整 parent 状态”伪装成完整继承。
- 含用户修改或额外 commit 的 worktree 不会被普通 cleanup 盲删。

### 删除条件

上游必须提供同等的可恢复 lifecycle、结构化冲突结果、dirty-source fail-closed 和 Delivery 兼容保证，才能替代。

## Core Patch 11：Subagent Worktree Isolation 协议与真实接线

提交：`6c194bd3 feat(subagent): thread worktree isolation through profiles`

### 对外协议

新增最小枚举：

```text
isolation: none | worktree
```

它贯穿 task tool schema、`runAs: subagent` Profile、CLI create/edit/run/try、persisted `SubagentRun` metadata
以及 Headless/ACP 投影。兼容默认始终是 `none`：只读 subagent 继续共享 workspace，writer 只有显式选择
`worktree` 才创建隔离资源，没有静默改变用户现有行为。

### 不是“只换一个 cwd”

隔离 child 的真实 runtime 会以 worktree root 重新装配：

- 文件、Bash 和其他 workspace tools 的根目录；
- permission/sandbox workspace boundary；
- project config 与 REASONIX/AGENTS/CLAUDE；
- skills、commands、hooks 和 MCP discovery；
- transcript、event、evidence、usage attribution；
- `resume` / `continue_from` 对同一 isolation resource 的定位。

动态 worktree path 只进入 runtime tail 和 metadata，不进入 cache-stable system prompt prefix。

### 删除条件

上游需同时具备显式 isolation 协议、child-root 全链路装配、persist/resume 身份和 prefix stability 测试。
只实现“临时目录执行”不算等价替代。

## Core Patch 12：Worktree Apply、冲突、恢复与 GC

提交：`31fca3f8 feat(worktree): complete safe isolation lifecycle`

### 具体行为

- subagent 完成只返回 exact path、branch、diff/stat，不自动 apply。
- `Apply` 是显式、受 permission/approval 约束的操作。
- clean apply 成功；冲突返回 `conflicted` 和 exact paths，parent workspace 不留下半应用状态。
- 两个 background writer 可同时修改同名文件，各自在独立 worktree，parent 保持不变。
- cancel、panic、进程退出后资源仍能 list/show；GC 只回收可以证明安全的 orphan。
- `continue_from` 复用原 isolation ID/root，不悄悄创建第二份资源。
- `Remove` 遇到未提交修改或 base 后的新 commit 会 fail-closed；`Discard` 才是明确的强制删除动作。

### 删除条件

上游实现同等 agent-level E2E，并证明双 writer、apply conflict、crash recovery、resume 和 GC 都不污染 parent 后方可删除。

## Core Patch 13：Headless / ACP Fail-Closed Usage Ledger

提交：`b8b55c95 feat(usage): add fail-closed shared usage ledger`

### 上游缺口

旧 Headless result 把 usage 累加进零值 struct；没有 pricing 或 nested background usage 尚未结算时，缺失值可能看起来像
`total_cost_usd: 0`。这会让自动化对账把“未知”误认为“免费”。

### 本地补的协议

CLI 与 ACP 共用一个内核 ledger/projection，不各算一遍。Headless result v2 在保留旧字段的同时增加：

```json
{
  "schema_version": 2,
  "usage_is_incomplete": true,
  "cost_is_partial": true,
  "total_cost_usd_ticks": 123,
  "modelUsage": {}
}
```

- usage 按 main/planner/executor/subagent 和 provider/model 归因；
- open background subagent、drain timeout、missing/partial pricing 都有结构化原因；
- 成本用 `1 USD = 10^10 ticks` 的整数精确求和，避免 float 累积误差；
- 只有 usage 完整且 USD pricing 齐全时才输出可信 float USD 和 ticks；
- 未知成本省略，绝不输出 0 冒充免费；
- stream-json 最后一行仍是 result，stdout 只含 JSON/NDJSON，诊断走 stderr；
- ACP 暂通过兼容 `_meta.usage` 复用相同 projection。

### 删除条件

上游必须能区分 zero、unknown、partial 和 uncollected，并共享 CLI/ACP 计算真源；只有新增几个输出字段不算等价。

## Core Patch 14：Memory Provenance、Staleness 与 Diversity

提交：`010c6c80 feat(memory): add provenance-aware diverse recall`

### 保留的 Reasonix 原则

继续使用纯 Go、Markdown/typed memory 真源、BM25、人工批准和 archive-on-forget；不引入 SQLite、CGO、embedding
或 vector DB。

### 本地补的能力

Memory 增加可选 metadata：`created_at`、`last_confirmed_at`、`source_scope`、`source_kind`。

- 旧文件无需 migration 即可读取，read 不写盘、不改 mtime；
- 新写/确认路径显式落 metadata，forget/archive 保留原 metadata；
- staleness 只降低 influence，不把旧事实自动判错或删除，truth-locked 事实仍可召回；
- BM25 + relative floor 后、limit 前加入纯 Go token/Jaccard 多样性重排；
- strongest lexical top hit 始终保留，近重复 memory 不再占满 topK；
- doctor/debug 可解释 BM25、rank、diversity、staleness 和 source；
- 多样性可配置关闭，项目配置不能覆盖用户级 recall policy。

固定中英 corpus 覆盖函数名、文件名、错误短语、命令参数、CJK、近重复 memory、旧事实和 archived memory，
用于验证 Recall@K、重复率、注入 token 和 top-1 稳定性。

### 删除条件

上游提供同等 old-format/no-write 兼容、可解释 provenance/staleness 和不损伤 top-1 的 diversity ranking 后方可替代。

## Core Patch 15：Canonical Tool Contract Benchmark

提交：`8ce9192d test(tool): benchmark canonical contract`

### 结论

Reasonix 继续只有一份 canonical ToolKind、registry、schema 和 executor。当前没有可靠 A/B 证据证明为 DeepSeek、Codex、
OpenCode 各复制一套工具方言能稳定提高首次调用成功率，因此**没有实现第二套 dialect executor**。

本 patch 固定 builtin tool 数量、contract bytes、schema order、allocations 等回归基线。未来只有固定任务、模型和 effort 的
A/B benchmark 证明收益后，才允许增加只负责 name/description/schema/envelope 的薄 projection；permission、sandbox、
路径约束、执行、event 和 evidence 仍必须共用 canonical executor。

### 删除条件

这个 patch 可以在上游已有等价 canonical contract benchmark 时合并；不能因为想试工具方言就先删除单一执行真源的约束。

## Core Patch 16：普通运行时配置只读

提交：本 patch，`fix(config): make ordinary runtime loading read-only`

### 修复的问题

此前普通 CLI、Desktop、ACP、Serve 和 Boot 启动路径会顺带执行 legacy/import/schema/MCP
迁移，并把重新渲染后的默认配置覆盖回 `~/.reasonix/config.toml`。这会把用户刻意维护的注释、未被当前
renderer 覆盖的本地选择和配置布局替换掉；表现为光标、模型组合、状态栏等习惯“偶尔恢复默认”。

### 新的稳定契约

- `config.LoadForRoot` 与所有普通运行时入口严格只读；
- 配置无效时返回带文件路径和 TOML 位置的明确错误，不允许退回默认值继续运行；
- deprecated 配置只产生警告，不在 read/startup 时改盘；
- legacy import、schema upgrade、MCP tier migration 只能由显式 migration/repair 操作执行；
- CLI、Desktop、ACP、Serve 和 Boot 遵守同一契约，不允许某个前端私自回写；
- 配置读取测试同时校验文件 bytes/mtime 不变，项目级 MCP discovery 不创建用户配置。

### 删除条件

只有上游所有普通启动与读取路径都满足 no-write-on-read、invalid-config fail-loud，并将 migration
限定为显式用户操作时，才可删除本 patch。仅修复某一个 CLI 入口不算等价。

## Core Patch 17：可配置 Hybrid TUI 信息架构

提交：本 patch，`feat(cli): add configurable hybrid TUI architecture`

### 修复的问题

此前 `docs/reasonix-tui-ia-selector.html` 只能生成设计 JSON；实际 CLI 只有零散的
`input_prompt`、`show_usage`、`lazy_reasoning` 配置。用户输入带、assistant 身份、activity、
composer 边框和 status 两层结构主要是固定渲染，无法作为一个可验证、可追更的信息架构契约。

### 新的配置契约

- `[ui.transcript]`：`profile`、`density`、`turn_separator`、`user_prompt`、`assistant_marker`；
- `[ui.transcript.show]`：role、activity、image understanding、recap、turn metrics；
- `[ui.composer]`：prefix、frame；
- `[ui.status]`：one/two layout，以及 cache、path、cost 可见性；
- `hybrid` preset 对应已确认的 selector JSON，显式子字段可以逐项覆盖 preset；
- 旧 `ui.input_prompt`、`ui.show_usage` 继续兼容，缺少新表时保持旧 Reasonix 行为；
- 非法枚举值在配置加载阶段明确报错，不静默降级；
- 所有字段只影响 TUI presentation，不进入 provider message、tool schema 或 cache-stable prefix。

### Hybrid 真实渲染

- 用户输入使用全宽 band；assistant 使用 `● Reasonix` 身份锚点；
- tool activity 使用左侧树线、按类别着色的菱形、动作/参数层级，并在原行补结束耗时；
- completed thinking 和 Image understood 继续复用 disclosure 交互；
- composer prefix 只在首行显示，frame 开关同步影响高度预算、光标位置和鼠标命中；
- 两层 footer 在 status 顶部使用 quiet rule，cache/path/cost 可分别隐藏；窄屏仍按语义组换行。

### 删除条件

上游提供等价的 transcript/composer/status typed config、preset + override 语义、严格校验、
responsive footer 和 live/replay 一致渲染后方可删除。只有颜色或 statusline 自定义命令不算等价。

## 升级上游时的检查清单

每次 `git fetch origin main-v2` 后：

```sh
git rev-list --left-right --count HEAD...origin/main-v2
git diff --name-status origin/main-v2...HEAD
git diff --stat origin/main-v2...HEAD
```

重点检查：

1. 上游是否新增 terminal title、`/title`、或 session title 同步逻辑。
2. 上游是否新增 reasoning expand/collapse UI。
3. 上游是否新增 image paste / vision attachment / OCR sidecar。
4. 上游是否改了 statusline、usage、cache metrics。
5. 上游是否改了 config schema/render。
6. 上游是否改了 `cmd/reasonix/main.go` 或 release ldflags。
7. 上游是否新增 subagent worktree isolation 与完整 lifecycle，而不只是 Desktop Delivery worktree。
8. 上游是否提供 Headless/ACP shared usage ledger，并明确 unknown/partial cost。
9. 上游是否给 memory 增加 provenance/staleness/diversity，同时保持旧格式 no-write-on-read。
10. 上游是否改变 canonical tool schema/order，或新增重复 executor/projection。
11. 上游是否提供 typed TUI information architecture，而不只是新增固定样式。

如果上游已经覆盖某项能力：

1. 先新建临时 worktree 或 backup branch。
2. 删除对应本地 patch。
3. 迁移用户配置。
4. 跑相关测试。
5. 更新本 work log，把该 patch 标为 retired，并写明替代的上游提交。

## 当前验收命令

本地分支当前至少应通过：

```sh
go test ./internal/cli
go test ./internal/config ./internal/control ./cmd/reasonix
go test ./...
make build
./bin/reasonix --version
```

生产安装后应确认：

```sh
command -v reasonix
reasonix --version
```

当前生产 binary 路径：

```text
/Users/jawa/.local/bin/reasonix
```

最近一次旧 binary 备份：

```text
/Users/jawa/Lab/2026-01-06-configure-move-on/versions/2026-07-08-reasonix-buildinfo/reasonix.before-buildinfo
```
