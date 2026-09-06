# Denova Agent Prompt 与 Tool Schema 写法审计

> 2026-09-06 更新：本文关于工具输入使用 `oneOf` / `anyOf` 分支的建议及实施记录已被替代，保留作历史审计背景。当前内置工具直接展示对象字段，以枚举说明操作，以基础 Schema 检查类型，以宿主校验条件必填、互斥和状态一致性；`ask` 由宿主根据 `options` 推导自由输入模式，模型不再填写 `allow_free_text`。以工具实现及 `builtin_schema_protocol_test.go` 的实际协议请求检查为准。


> 日期：2026-08-13
> 状态：核心改造已实施；本文同时保留审计依据、对照样例和后续评测标准。
> 对照基线：本地 `../claude-code`、`../codex`、用户提供的最新 Claude Code system prompt 与当前 Denova 工作区。

## 一、结论摘要

Denova 不需要推倒重写 Prompt 基础设施。现有 composition、context placement、tool registration 和 Schema 反射已经足以支撑高质量实现。本轮审计更需要解决的是模型实际读到的文字：是否精简、足够、直接，是否把每条信息放在最能发挥作用的位置。

本文采用一个首要标准：**给模型当前任务所需的最少充分 context。** “稳定”“有来源”“可缓存”只说明一段内容可以可靠注入，不说明它值得占用模型注意力。任何 system rule、context block、tool description 或字段说明，都应能回答“它会改变模型的哪一个决策”；回答不了就应删除、延迟加载或留在 host metadata 中。

现有基础中应保留的部分包括：

- system source 有 ID、来源、用途、字节上限、溢出策略、Hash 和 admission manifest；
- runtime context fragment 有来源、位置、渲染方式和硬上限；
- 稳定上下文与回合动态上下文已经有不同 placement；
- 工具按 Agent capability 和运行时可用性注册；
- 多个批量工具支持逐项成功/失败，不会因为一个坏输入丢掉整个昂贵调用；
- `submit_interactive_turn` 已经使用互斥的 `oneOf` 操作分支；
- Skills 已经支持渐进式披露，`/configuration` 的 reference 设计尤其合理。

真正的问题不是“指令不够多”，而是：

1. **Context 信噪比不足。** 部分 Agent 一次收到身份、工作流、来源边界、字段表示、重试方式、完整目录和动态状态；其中很多内容当前回合用不到，且可以由 Tool、Schema 或按需读取承担。
2. **同一语义重复出现。** 文件读写、状态同步、游戏提交、导演 Patch 和重试规则同时出现在 system prompt、回合注入、Skill、tool description、字段 description 和工具反馈中。每个副本都看似正确，合在一起却稀释了当前目标。
3. **自然语言承担了本应由 Schema 表达的结构。** `ask`、`task`、`skill`、`todo` 的分支条件没有完整进入 `required`、enum、长度约束或 `oneOf`，迫使长 description 补充结构规则。
4. **项目指令需要统一边界。** 根目录 `AGENTS.md` 与 `CREATOR.md` 都是低频变化、项目级、用户拥有的长期指令，适合作为历史之前各自独立的 stable leading context，而不是 Agent 内建 system definition 的组成部分。
5. **缺少完整的 provider-visible 请求快照。** 现有测试不能一次性显示每类 Agent 最终收到的 system、project instruction、历史、当前请求、工具 description 和 Schema，因此难以审查总长度、语义重复和无效 context。

审计建议的顺序是：

1. 先渲染完整 provider-visible 请求，量出每段 context 的字节、token 和语义重复；
2. 建立本文的 Prompt/Schema 写作规范，删除不能影响当前决策的文字；
3. 把根目录 `AGENTS.md` 与 `CREATOR.md` 统一为早期项目指令，并保持独立来源和稳定顺序；
4. 让 Schema 承担结构约束，再按 Agent 做效果评测。

### 本轮实施结果

| 领域 | 已落地的设计 | 模型实际看到的变化 |
| --- | --- | --- |
| Project instructions | 新增通用 `CombineContextSources` 和统一 Project Instructions ContextSource；General、写作、游戏、后台 Director、Image 及其子 Agent 复用同一边界。 | 根目录 `AGENTS.md` 与 `CREATOR.md` 分别成为一个 User-role leading message；缺失文件跳过，两者都存在时按该顺序位于历史和当前请求前。 |
| Cache boundary | ContextSource identity 由 Agent kind、workspace、resource 列表和注入上限构成；每个文件的正文 revision 在 materialize 时独立计算。 | 修改 `CREATOR.md` 不改变更早的 `AGENTS.md` message；配置或正文变化时只从对应边界开始旋转缓存。 |
| Prompt ownership | 缩短通用 runtime contract、session-state wrapper、SubAgent wrapper、Game turn prompt 和 Director per-run prompt；稳定规则留在 system/tool，本轮消息只保留当前动作和动态上下文。 | 不再每回合重复 difficulty enum、Actor/lore 生命周期、通用优先级声明和完整 Director 文档教程。 |
| Tool Schema | `ask`、`task`、`skill`、`todo` 使用 provider-visible JSON Schema；action 分支改为关闭的 `oneOf`。 | 每个分支只显示合法字段、required、enum、数量与长度边界；`ask` 区分 free-text/choice，并表达唯一 recommended option。 |
| Batch resilience | 保留 task/skill/todo 的逐项执行和逐项回执，没有因 Schema 变严而退回整批失败。 | 模型首次更容易构造合法输入；单个坏 item 仍不会丢掉其他成功结果。 |
| Provider-visible verification | 复用 `Session.Inspect` 的真实组装管线，增加项目 Agent 集成断言和 ToolInfo Schema contract 测试。 | 测试直接检查最终 message 顺序、stable-prefix 计数、两个项目指令文件的唯一性和完整 `oneOf` Schema，不另造模拟 renderer。 |

这一批改动不引入 Tool Search、新配置或兼容层。Denova 现有的 capability-based tool registration 已经可以只向每个 Agent 暴露所需工具；在没有 Schema token 占比证据前，再加一轮工具发现反而会增加 context 和延迟。

## 二、审计范围与方法

本次覆盖：

- 所有注册的 Denova Agent kind；
- system prompt 组合、用户自定义 Prompt 和优先级；
- stable context、turn-dynamic context 和当前请求注入；
- 中断恢复、Automation、Image、Compaction 等专用 Prompt；
- 内置 Skills 与仓库本地 Agent Skills；
- 工具 description、操作分支和字段 description；
- Prompt cache、上下文组装和相关测试。

对照时重点学习实际写法，而不是只比较实现架构：

- Claude Code：system prompt 如何写角色与通用工作方式，Read/Edit/Ask/Agent/Tool Search 如何拆分 tool description、usage 和字段 description；
- Codex：基础指令如何用短句表达默认行为，`view_image`、`update_plan`、`request_user_input`、`apply_patch` 等工具如何把语义说明与 JSON Schema/grammar 分开；
- Denova：`internal/agents/prompts`、`internal/agents/context`、独立 `agent` package、工具注册、产品工具、Skills、Automation、Image、Interactive 主流程。

本文最初的判断基于改造前的工作区；上表和后文的“实施后”片段已按当前代码更新。保留旧写法片段是为了记录对比依据，不代表当前实现仍有双路径。

## 三、参考产品如何写 Prompt 与 Tool Schema

### 3.1 Claude Code：把“能力、用法、参数”分开写

Claude Code 的成熟之处不是所有 Prompt 都短，而是很多高频工具已经形成稳定的写法层次：

- catalog description 通常只有一句能力概述，例如 Read 的入口描述只说明读取本地文件；
- tool prompt 只补充模型做选择和正确调用所需的 usage，例如绝对路径、分页、支持的文件类型和失败条件；
- 字段 description 很短，直接说明值是什么、何时填写，例如 `file_path` 是绝对路径，`offset`/`limit` 只在文件过大时使用；
- Zod 直接承担 `strictObject`、required、enum、min/max 和默认值，不再用长段文字重复同一结构；
- Agent/Skill 的发现列表只提供名称和 `whenToUse`，完整说明在真正调用后再加载。

几个具体写法值得吸收：

1. **先写动作，再写选择条件。** `Reads...`、`Performs...`、`Asks...` 直接以能力开头，不先解释内部实现或设计动机。
2. **参数描述靠近参数。** 路径、单位、默认值、适用条件写在字段上，而不是埋在 system prompt。
3. **例子只用于消除表示歧义。** Bash 的 command description 给少量好坏尺度示例；普通路径或布尔字段不堆例子。
4. **按需披露。** Skill 列表有总预算和单项长度限制；Tool Search 初始只暴露可发现能力，完整 JSON Schema 在需要时加载。
5. **动态信息不污染通用描述。** 可用 Agent 列表可以独立注入，避免一项变化让整个工具 description 变长或变化。

Claude Code 也有长期累积的大段 Git、安全和 Feature 特例，其中存在重复的 `IMPORTANT`、`NEVER` 和流程复述。Denova 应学习其高频工具的简洁写法、参数就近说明和 progressive disclosure，不应把这些历史沉积当成目标风格。

#### 3.1.1 用户提供的最新 Claude Code system prompt

本轮又对照了用户提供的最新完整 system prompt，其中最值得吸收的不是 Claude 的品牌身份，而是 `Harness` 部分对日常执行行为的短句表达：

```text
You are an interactive agent that helps users with software engineering tasks.

Prefer the dedicated file/search tools over shell commands when one fits.
Independent tool calls can run in parallel in one response.

Write code that reads like the surrounding code: match its comment density,
naming, and idiom.

Report outcomes faithfully: if tests fail, say so with the output; if a step
was skipped, say that; when something is done and verified, state it plainly.
```

这些句子有四个共同特点：

- 先说模型要做的动作，不先解释 host 架构；
- 一句只管一个高频决策：选什么工具、能否并行、怎样跟随代码风格、如何报告结果；
- 把“权限被拒绝”写成可执行的修复行为：`adjust, don't retry verbatim`，而不是给出一长串拒绝类型；
- 结果报告强调事实一致，包括失败和跳过的验证，比泛化的“小心”提醒更能改变模型行为。

对应的 Denova General Agent 已收敛为：

```text
You are Denova's general-purpose project Agent. Complete research,
development, writing, organization, and automation tasks in the current Project.

Understand the request and relevant project state, then use the available
tools as needed. Follow applicable project instructions such as AGENTS.md or
CLAUDE.md, including instructions near the files you change.

Prefer dedicated file and search tools when they fit; independent tool calls
may run in parallel.

Write code in the surrounding style, including its naming and comment density.
Make the smallest complete change and verify it in proportion to its impact.

Report the actual outcome: what changed, what was verified, any failure or
skipped check, and any remaining limitation.
```

通用 runtime contract 也只保留了对应的修复句：

```text
If a tool call or permission is denied, adapt to the result instead of
repeating the same request unchanged.
```

明确不吸收的部分包括：Claude Code/Anthropic 品牌身份、模型家族与 Fast mode 信息、特定机器环境、Memory 文件体系、`[[name]]` 链接习惯、安全场景政策、Claude 专属 Skill fallback 和产品个性。这些内容要么属于另一个产品的 host contract，要么与 Denova 当前 Agent 的决策无关；直接复制只会增加上下文，甚至造成错误能力暗示。

### 3.2 Codex：短句、显式默认值、结构交给 Schema

Codex 的基础 Prompt 主要使用直接、可执行的陈述：一句话描述默认行为，必要时再给极少量例外。它的 Tool Schema 更能体现这种写法：

- `view_image` 的 tool description 只回答“做什么、何时用”；`path`、`detail`、`environment_id` 各自用一句话说明，默认值写在对应字段；
- `update_plan` 的 description 只有用途、输入形态和“最多一个 in_progress”这个跨字段不变量；`step` 和 `status` 不重复整个计划工作流；
- `request_user_input` 把选项数量、互斥性、推荐项和自动 Other 放在最接近它们的 `options`/`questions` Schema 上；
- `apply_patch` 的 description 只说明它是编辑文件的 freeform tool，具体表示由 Lark grammar 约束，而不是再写一份自然语言格式规范；
- 大多数对象列出 required 并设置 `additionalProperties: false`，模型不需要从段落中猜哪些字段有效。

Codex 的字段文案通常遵循一个固定句式：

```text
<字段表示什么>。<省略时的默认行为或何时填写>。<必要的单位、来源或互斥关系>。
```

它不会在每个字段解释后台如何实现，也不会把字段名已经表达清楚的内容扩写成一段教程。复杂度优先进入 enum、required、array bounds、`oneOf` 或 grammar；只有 Schema 无法表达的选择语义才留在自然语言中。

### 3.3 两个产品共同体现的写作原则

成熟写法的共同点可以归纳为：

- **最少充分，而非形式完整。** 只提供完成当前判断所需的信息；背景正确但当前无用，仍然属于噪音。
- **一条规则只有一个主要位置。** 全局行为在 system，工具选择在 tool description，参数合法性在 Schema，修复方法在 tool result，当前任务在 turn message。
- **正向默认优先。** 先告诉模型应该做什么；只有一个常见且代价明确的误用需要阻止时，才增加一句 `Do not`。
- **强词有预算。** `MUST`、`NEVER`、`IMPORTANT` 只保留给会造成明显错误结果、不可逆副作用或协议失败的少数约束；重复使用会让所有规则失去层级。
- **默认值和省略语义必须明确。** 模型最常犯的不是不知道字段含义，而是不知道该省略、传空值还是使用默认值。
- **结构约束不写成散文。** required、enum、长度、数组数量、互斥分支和禁止额外字段优先由 Schema 表达。
- **例子必须赚回 token。** 只有抽象说明不足以确定格式或选择时才给一个最小、规范的例子。
- **Context 需要选择，不是打包。** 目录、Skill、历史、状态和参考资料优先摘要、筛选或按需读取，不因为“可能有用”就全部注入。

### 3.4 Denova 的目标写法

Denova 后续新增或重写 Prompt 时，默认使用以下最小结构：

```text
System prompt
1. Role and objective
2. Decisions or invariants that apply to every turn
3. Completion/output contract

Tool description
1. Capability
2. When to choose it, only if the name is insufficient
3. One cross-field or transactional rule that Schema cannot express

Turn context
1. Current task
2. Only the state/evidence needed for this task
3. Expected result for this turn
```

不要求每个 Prompt 都机械包含三部分；某部分没有新增信息就省略。每句话在进入模型请求前都应通过四个问题：

1. 删除它会让模型做错哪个具体决策？
2. 这条信息是否已经在更合适的 system、Tool、Schema、Skill 或 tool result 中？
3. 当前回合是否真的需要，还是可以通过工具按需取得？
4. 能否用约束、枚举、字段名或更短的正向句子表达？

成熟产品对项目指令文件的早期注入值得采用：这类文件高相关、低频变化，并持续影响项目内的大多数决策。Denova 根目录的 `AGENTS.md` 与 `CREATOR.md` 都符合这个条件；完整 lore 目录、所有 Skill reference 或与当前回合无关的状态则不符合。

### 3.5 参考 Prompt 与 Denova 当前写法对照

以下示例只截取影响写法判断的最小原文。`建议方向` 用于说明如何吸收表达方式；其中核心方向已按“本轮实施结果”落地，但示例不是要求逐字复制参考产品。

#### 示例一：General Agent 身份与默认工作方式

Codex 基础 Prompt 先用一句话定义身份，后续规则直接描述行为：

```text
You are Codex, based on GPT-5. You are running as a coding agent in the
Codex CLI on a user's computer.

- When searching for text or files, prefer using `rg` or `rg --files`.
```

来源：`../codex/codex-rs/core/gpt-5.2-codex_prompt.md`

Denova 改造前的 General Prompt：

```text
You are Denova's General Agent, comparable in role to Codex, Claude Code,
or OMP. Provide general research, development, writing, organization, and
automation services inside Projects explicitly added by the user.

Before a substantial task, inspect applicable project guidance such as
AGENTS.md, CLAUDE.md, README files, and contribution guides...

File discovery and content search respect .gitignore by default...

A Project that happens to point at a Denova data directory receives no
additional hidden restrictions...
```

来源：`internal/agents/prompts/composition.go`

对比结论：

- “comparable in role to Codex, Claude Code, or OMP”不改变模型决策，可以删除；
- Project root、就近项目规则、完成后验证是每轮通用行为，应保留；
- `.gitignore` 的精确例外和 Denova data directory 语义更接近工具/运行时行为，只有模型确实需要据此选择操作时才值得进入 system；
- system 身份不需要列举产品覆盖的所有名词，目标和完成标准更重要。

建议方向：

```text
You are Denova's general-purpose Project agent. Complete research,
development, writing, organization, and automation tasks in the current
Project.

Understand the user's goal and existing structure, follow the nearest
applicable project instructions, take the smallest complete action, and
verify the result before reporting it.
```

#### 示例二：Read Tool 的 description 与字段说明

Claude Code 把入口能力和字段用途分开：

```text
DESCRIPTION = "Read a file from the local filesystem."

file_path: "The absolute path to the file to read"
offset: "The line number to start reading from. Only provide if the file is
too large to read at once"
limit: "The number of lines to read. Only provide if the file is too large
to read at once."
```

来源：`../claude-code/src/tools/FileReadTool/prompt.ts`、`FileReadTool.ts`

Denova 当前写法：

```text
Read one bounded resource using the parameters supported by its registered
adapter. HTTP(S) URLs are not supported; use web_fetch. Available adapters: ...

path: "Project-relative or absolute local path of the UTF-8 text file to read.
External paths remain subject to permission."
offset: "One-based first line to return; defaults to 1."
byte_offset: "Zero-based UTF-8 byte offset within the selected first line,
used only with an exact next_byte_offset continuation."
limit: "Maximum selected lines to return; defaults to 2000."
```

来源：`agent/tools/read_tool.go`、`agent/tools/filesystem_read_adapters.go`

对比结论：

- Denova 的字段 description 已经达到参考产品水平：含义、单位、默认值和填写条件都靠近字段；
- 顶层 description 中 “parameters supported by its registered adapter” 是实现视角，模型只需要知道能读什么以及 HTTP 应改用哪个工具；
- adapter 名单如果不会帮助模型构造参数，就不应占用 description；具体合法输入已经由 `anyOf` Schema 提供。

建议方向：

```text
Read one bounded local or registered resource. Use web_fetch for HTTP(S) URLs.
```

#### 示例三：Ask Tool——弱化安全措辞，补足输入语义

Codex 的 `request_user_input` 先说明使用目的，再把约束放到最近的 Schema 节点：

```text
Request user input for one to three short questions and wait for the response.

label: "User-facing label (1-5 words)."
description: "One short sentence explaining impact/tradeoff if selected."
options: "Provide 2-3 mutually exclusive choices... Do not include an Other
option; the client will add a free-form Other option automatically."
```

来源：`../codex/codex-rs/core/src/tools/handlers/request_user_input_spec.rs`

Denova 当前入口 description：

```text
Ask one to three questions when required input cannot be inferred. For choice
questions, prefer two or three concise options; use four only when every option
is materially different. Write questions, options, and descriptions in the
same language as the user's current input.
```

改造前 `questions` 有 `minItems=1,maxItems=3`，但 `InteractionQuestion`、`InteractionOption` 和 `LocalizedText` 的模型可见字段没有对应 description；choice/free-text 的互斥关系、稳定 ID、recommended 和 host-provided Other 主要留在 runtime validation。

来源：`agent/tools/ask_tool.go`、`agent/interaction.go`

对比结论：

- “cannot be inferred safely”把普通澄清包装成安全问题，范围过窄；偏好、产品选择和需要用户决策的情况同样应该使用 Ask；
- 顶层 description 不应承担嵌套数据教程；
- 模型生成的 Ask 文案应跟随用户当前输入语言，不应要求模型同时生成中英翻译；产品自身的按钮、状态和错误仍由前端 message catalog 本地化；
- required、长度、ID、choice/free-text 分支应由 Schema 表达。

建议方向：

```text
Ask one to three questions when the task needs missing information, a
preference, or a decision. Use the user's current input language.

questions: Questions shown together; provide 1-3.
id: Stable question ID copied into the answer.
prompt: Required user-visible text in the user's current input language.
options: Mutually exclusive choices; omit for a free-text question. Other is
added by the host.
```

choice 与 free-text 现已使用两个互斥 `oneOf` variant；两者只显示各自合法字段。单语言文案、ID 边界、选项数、host 自动 Other 和恰好一个 recommended option 均进入模型可见 Schema。choice 硬限制为 2–4 项，同时提示优先提供 2–3 项；runtime validation 继续作为最终保证。

#### 示例四：任务计划与 Todo Schema

Codex 的 `update_plan` 只在顶层保留一个 Schema 无法独立表达的跨项不变量：

```text
Updates the task plan.
Provide an optional explanation and a list of plan items, each with a step
and status.
At most one step can be in_progress at a time.

step: "Task step text."
status: "Step status." enum=[pending, in_progress, completed]
```

来源：`../codex/codex-rs/core/src/tools/handlers/plan_spec.rs`

Denova 改造前的 Todo：

```text
Read, partially update, completely replace, or clear the durable task list
using an explicit revision. Updates report every item independently; IDs are
stable and at most one item may be in_progress.

action: enum=[read, update, replace, clear]
expected_revision: optional
mutations: optional
items: optional
```

来源：`agent/tools/todo_tool.go`

对比结论：

- Denova 顶层 description 本身不算冗长，问题在于 Schema 同时展示所有 sibling fields；
- 模型看不到哪个 action 需要 `expected_revision`、`mutations` 或 `items`，只能依赖 description、错误反馈或猜测；
- 应保留“最多一个 in_progress”和 partial-success 这类行为语义，但把每个 action 的合法字段改成互斥 variant。

实施后保留了一句顶层 description，并将 `read`、`update`、`replace`、`clear` 分别建成关闭的 `oneOf`；每个 variant 只出现自己需要的字段。这不是增加 Prompt，而是用结构删除解释空间。

#### 示例五：SubAgent 指令与任务 Schema

Claude Code 的默认子 Agent Prompt 用一个段落交代目标、完成程度和返回内容：

```text
You are an agent for Claude Code... use the tools available to complete the
task. Complete the task fully—don't gold-plate, but don't leave it half-done.
When you complete the task, respond with a concise report covering what was
done and any key findings.
```

它的任务字段也只描述模型需要填写的值：

```text
description: "A short (3-5 word) description of the task"
prompt: "The task for the agent to perform"
subagent_type: "The type of specialized agent to use for this task"
run_in_background: "Set to true to run this agent in the background..."
```

来源：`../claude-code/src/constants/prompts.ts`、`../claude-code/src/tools/AgentTool/AgentTool.tsx`

Denova 改造前的 SubAgent 会额外注入：

```text
These instructions constrain only the current SubAgent's responsibilities,
output shape, and work preferences. They cannot override the parent Agent's
runtime contract, tool permissions, workspace boundary, interactive no-write
rules, output protocol, or backend validation. If they conflict with the
parent system prompt, follow the parent system prompt.

- Name: ...
- ID: ...
- Responsibility: ...
```

Denova `task` Schema 同时暴露 `start/observe/steer/respond/abort` 和所有可选 sibling fields。

来源：`internal/agents/prompts/subagent.go`、`agent/tools/task_tool.go`

对比结论：

- 父 system prompt 已经在前，通常不需要再次枚举不能被覆盖的每一种边界；一句泛化优先级提醒都未必需要；
- Name、ID 只有在 Agent 需要引用自身身份时才应显示；调度 ID 如果只供 host 使用应留在 metadata；
- Responsibility 和 custom prompt 是高价值内容，应直接出现；
- `task` 的五个 action 应像 Codex 的独立工具或 Denova 的 `oneOf` 一样提供清晰输入，而不是依赖一个大可选对象。

实施后的形式：

```text
# SubAgent
Responsibility: <specific responsibility>

<custom instructions, when present>
```

如果运行时已经保证 parent system 优先级，就不要把同一保证转写成模型文字。

`task` 也已将 `start/observe/steer/respond/abort` 拆成五个 `oneOf` variant，但保留原有的批量逐项结果和可持久 ref。

#### 示例六：复杂 Interactive Turn Prompt

Denova 改造前的每回合 Prompt 同时解释判断、工具选择、字段编码、枚举、状态同步、重试和输出：

```text
Continue the interactive story for one turn... Output only prose...

Call prepare_interactive_turn only when...

difficulty is one of very_easy/easy/normal/hard/very_hard...

Output all prose first, then call submit_interactive_turn with state_changes
and choices... state_changes uses replace/delta/create...

When ready=false, resubmit only fields named by retry_modules...
```

来源：`internal/agents/prompts/interactive.go`

对比前述 Claude Code/Codex 工具写法后，可以明确拆分：

- “输出一回合玩家可见正文、保持连续性、在有明确风险时判定、最后提交状态”属于每回合行为；
- difficulty enum、roll mode、state operation variant 和 required fields 属于 Schema；
- `ready=false` 后具体修复哪些模块属于本次 tool result；
- Actor handbook、当前行动和本轮 storyteller rule 属于动态 context；
- backend 如何独立保留模块属于实现说明，除非它改变下一次调用，否则不进入初始 turn Prompt。

在 Tool/Schema 承接相应契约后，turn Prompt 已收敛为：

```text
# Current turn

User action:
<current user action>

<optional storyteller context rule>

Continue the interactive story for one turn using the supplied context and
the system workflow. Output only player-visible prose.

After the complete prose, call submit_interactive_turn with the state changes
and choices established by this turn. End immediately when the receipt is ready.

<only the current state, applicable rule, and selected evidence>
```

这不是简单删词：前提是字段表示已经进入 Schema、重试指令由 tool result 返回、稳定行为只在 system 保留一次。这样模型仍获得足够信息，但当前行动和当前状态会成为最显眼的内容。

## 四、Denova 现有基础与模型可见内容

### 4.1 已经做得好的部分

`internal/agents/prompts/composer.go` 已经是完整的 admission layer：

- source ID 唯一；
- `Source`、`Purpose` 必填；
- 有单 source、总量、metadata、fragment 数量上限；
- 契约类内容超限时拒绝，可选 reference 才允许截断；
- 记录原始/实际注入 Hash；
- 记录最终 instruction Hash 和 manifest。

独立 `agent` package 也要求每个 `ContextFragment` 有 provenance 和 `HardLimit`，并区分：

- `leading_message`；
- `compaction_checkpoint`；
- `final_user_prefix`；
- `final_user_message`；
- `audit_only`。

稳定 leading fragments 在普通回合、重试和 Compaction 中保持同样的 role 与 bytes，这正是前缀缓存所需的底座。

这些能力属于 host 侧质量保障，不应自动转化成更多模型说明。Prompt 质量仍取决于选择：即使一个 fragment 来源完整、边界可靠、缓存稳定，只要它不影响当前决策，就不应进入默认 context。

工具侧同样有良好基础：

- 只注册当前 Agent 允许且运行时可用的工具；
- descriptor 把并发、mutation、post-check、recovery、retention、projection、steering 与自然语言 description 分开；
- 参数归一化可以修复部分模型错误；
- 多个高成本批量操作返回逐项结果；
- 游戏状态操作已经采用互斥 `oneOf`。

### 4.2 `AGENTS.md` 与 `CREATOR.md` 的归属

审计时，IDE 和 Interactive Story 的顺序大致是：

```text
runtime contract                         稳定
output protocol                         稳定
CREATOR.md                              工作区可变
当前导演/teller                         配置可变
文风协议与文风引用                       配置可变
Denova 长篇内置工作流                    稳定
image/call-site 规则                    配置或调用可变
User State Prompt                       用户可变
SubAgent Prompt                         任务可变
```

这里的优先级语义是清楚的，而且由于 Creator、导演和文风配置都低频变化，同一工作区/Session 的 steady-state 前缀命中并不存在明显问题。只有这些配置真正变化的回合，变更点之后的内容才需要重新计算；后续回合会继续命中新前缀。因此，不应把当前顺序定性为 P0 缓存问题。

需要调整的是 identity boundary：`CREATOR.md` 是作者拥有的项目级长期指令，不是 Denova 或某个 Agent kind 的内置身份。把它直接拼入多个 Agent 的 system composition，会让通用 Agent definition、项目指令和配置内容成为一个大字符串，来源边界只能依靠内部 manifest 表达。

实施后的实际顺序：

```text
system: runtime contract                 Denova/Agent immutable
system: output protocol                  Denova/Agent immutable
system: built-in workflow                Denova/Agent immutable

user leading context: AGENTS.md          project/workflow instructions; low churn
user leading context: CREATOR.md         creative instructions; low churn
other stable leading context             project/configuration snapshots

conversation history                     session history
turn-dynamic context                     current state/evidence
current user request                     current instruction
```

两个 fragment 都使用 User role 和稳定的短 framing，按 `AGENTS.md`、`CREATOR.md` 排序并位于历史与当前请求之前，因此可以与 system definition 一起形成长稳定前缀。General、写作、游戏、Director、Image 及其项目子 Agent 使用同一来源。每个文件的 `Source`、`Purpose`、`Resource`、revision/hash 和硬上限分别保存在 host metadata；模型只看到对应短标题、正文和一句“较新的显式用户请求优先”。

这项调整的首要收益是让每份高相关项目指令各自只出现一次，并把 Agent 固有 Prompt 留给真正跨项目成立的行为；来源审计和跨 Agent 复用由 host 侧同时获得。它不是为了修复不存在的 steady-state cache 问题。

## 五、主要问题与建议

下表保留审计时的问题表述，用于说明改动动机；Project instruction、Interactive turn/Director 分层、Ask/Task/Skill/Todo Schema 和 provider-visible contract 已按本文首部的实施表落地。测量型指标仍作为持续评测标准，不因一次重构在生产代码中新增无证据的复杂度。

| 优先级 | 领域 | 现状 | 影响 | 建议 |
| --- | --- | --- | --- | --- |
| P0 | Context signal-to-noise | 部分 Agent 默认注入完整工作流、目录、状态、来源说明和重试协议，即使当前回合只使用其中很小一部分。 | 有效任务和当前状态被背景文字包围；输入成本、延迟和模型漏读概率同时上升。 | 每个 block 通过“会改变哪个当前决策”检查；不能通过的删除或延迟加载。优先选择当前状态、当前目标和必要约束，不按“可能有用”打包。 |
| P1 | Project instruction boundary | `CREATOR.md` 被直接拼入 Writing Agent、Game Agent 和 Image Agent 的 system composition。 | 用户拥有的项目规则与 Denova/Agent 内置定义耦合；同一项目指令跨 Agent 重复组装，role、revision 和生命周期边界不够直观。 | 复用现有 `ContextLeadingMessage`，以 User role 注入一个独立、attributed、低频稳定的 project-instruction fragment；从各 Agent system composition 中删除 Creator 副本，不保留双路径。 |
| P0 | 契约归属 | 文件操作、写作状态同步、游戏提交、导演 Patch、重试规则跨多个层重复。 | Token 多、注意力分散、文字与后端校验容易漂移。 | system 管行为与优先级；tool description 管选择和事务；Schema 管编码；tool result 管修复；Skill 管可选流程；turn prompt 只管当前任务。 |
| P1 | Interactive Story | `InteractiveStoryTurnInstruction` 同时包含当前行动、判定策略、文风、提交、重试、choices、状态语义。 | 每回合重复大段稳定规则，最容易稀释模型对当前行动的关注。 | 把稳定协议移入 system/tool；回合消息只注入当前行动、动态上下文、当回合规则和完成要求。 |
| P1 | Interactive Director | runtime、system、per-run prompt、提交工具重复文档边界、Patch、finalize、retry 和保密规则。 | 背景 Agent 上下文冗长，多个副本可能漂移。 | system 保留 source-of-truth 与职责边界；工具拥有 Patch/base_hash/finalize；per-run 只给模式、快照、Hash 和目标。 |
| P1 | Tool Schema | 多个重要约束只在 Go runtime validation 中存在。`ask` 最明显。 | 模型第一次看不到完整契约，产生本可避免的无效调用和重试。 | 每个模型填写的嵌套字段都补齐 meaning、required condition、default、边界和 sibling relation；能在 Schema 表达的校验不要只留在运行时。 |
| P1 | Action union | `task`、`skill`、`todo` 使用 action enum 加一组可选 sibling fields。 | Schema 允许无意义组合，模型只能靠错误反馈学习分支要求。 | 像 `submit_interactive_turn` 一样改为 discriminated `oneOf`，保留批量逐项成功。 |
| P1 | Agent routing | `builder.go` 中部分 Agent description 只是“AI novel-writing assistant”一类角色标签。 | 父 Agent 难以在 general-purpose 与专用 Agent 间稳定路由。 | description 用一句 `Use when` 说明触发条件和预期产物；只有与相邻 Agent 经常混淆时才补一句非适用条件。UI 本地化名称另存。 |
| P1 | 完整请求审计 | 现有测试覆盖 admission、substring、Hash、placement、Schema 和 cache metrics，但没有每类 Agent 的完整模型请求快照。 | Code review 很难发现无效 context、语义重复或意外缓存破坏。 | 生成 minimal/full provider-visible snapshot：message role、section ID、字节/token、语义 owner、tool/description/schema Hash。 |
| P2 | Context framing | 部分动态内容使用冗长 wrapper，部分使用 Markdown 标题直接串接。 | wrapper 本身消耗注意力；不同 block 的用途仍需从长段说明中判断。 | host 侧保留完整 provenance；模型侧只在用途确实不同且会影响处理方式时使用短、固定标签，如 `Current state`、`Evidence`、`Project instructions`。 |
| P2 | Tool description | Browser/Interactive 顶层 description 很长并重复字段规则；部分公共工具字段又过于简略。 | 工具目录越来越大，但关键选择条件仍不突出。 | 顶层优先写一至三句：能力、何时选择、Schema 无法表达的一项事务语义；字段表示放 Schema，修复方式放 tool result。 |
| P2 | Skills | 产品 Skills 整体不错，`/configuration` 的 progressive disclosure 尤其好；`novel-standard` 仍重复基础写作契约；`.agents/skills/loop-verify`、`verify-by-browser` 仍是中文模型指令。 | 可选流程重新引入重复规则；仓库本地 Agent 指令不符合新的内部英文规范。 | Skill 只保留 workflow delta、触发条件和完成条件；仓库本地模型可见 Skills 也纳入英文 lint。 |
| P2 | Telemetry | 现在记录 bytes、Hash、manifest 和缓存用量，但不能直接指出第一个发生变化的 Prompt/tool segment。 | 难以定位哪次改动造成 uncached input 增长。 | 记录有序 section fingerprint、首个差异位置、工具顺序和 Schema Hash；按 Agent kind 统计 cached-prefix bytes。 |

关键代码证据：

- Project Instructions ContextSource、稳定顺序及逐文件硬上限/revision：`internal/agents/lifecycle/project_context.go`；
- 书籍 Agent 的统一组装与子 Agent 复用：`internal/agents/builder.go`；
- ContextSource 有序组合：`agent/context_sources.go`；
- system fragment admission 与 manifest：`internal/agents/prompts/composer.go:28`；
- 可直接复用的 leading-message、User role、provenance/hard-limit 和 attributed renderer：`agent/definition.go:62`、`:84`、`:679`、`agent/definition_engine_context.go:190`、`:282`；
- Denova 已有稳定 workspace context 位于 transcript 前的实现：`internal/agents/conversation/model_context.go:220`；
- 精简回合动态 Prompt 与稳定 Game/Director 契约：`internal/agents/prompts/interactive.go`；
- action-specific `oneOf`：`agent/tools/task_tool.go`、`agent/tools/skill_tool.go`、`agent/tools/todo_tool.go`、`agent/tools/tool_schema.go`；
- `ask` 互斥分支、当前输入语言字段与 runtime validation：`agent/tools/ask_tool.go`、`agent/interaction.go`；
- 已有的正确 `oneOf` 示例：`internal/agents/tools/interactive_story.go:170`、`:181`。

## 六、逐类 Agent 审视

### 6.1 General Agent

General Prompt 比较克制，已经清楚表达：Project root、就近读取项目规则、选择最小可验证动作和完成后验证。这可以作为其他 Agent 的简洁度参考。

可优化点：

- 稳定 General workflow 与按需 Trajectory evidence、动态工具提示分层；
- 项目规则发现只保留一个权威版本，避免被 Skill/task 重复；
- general-purpose 子 Agent 的路由 description 先补明确的 `Use when`；只有出现真实路由冲突时再增加非适用边界。

### 6.2 Writing Agent

这是当前最大的维护热点。内置写作流程、CREATOR、导演、文风索引、Image preset、写作 Skill、文件工具 description 存在明显重叠。

建议边界：

- system：写作身份、创作目标、每轮都成立的不变量和完成条件；
- project instruction：根目录 `AGENTS.md` 与 `CREATOR.md` 作为独立 User-role stable leading context，独立于 Agent system definition；
- writing Skill：lite/standard 编排差异与完成标准；
- file/lore tool：精确读写、编辑和重试机制；
- director/style：带来源和优先级的低频配置数据；
- turn message：用户本轮范围、选中文本/引用、当前动态状态。

`novel-lite` 和 `novel-standard` 是合理产品抽象，但两者都重复文件错误处理和状态同步机制。共通部分应有一个稳定 owner。

### 6.3 Game Agent

这是最值得优先精简的 Prompt。现在“只输出剧情正文、状态通过工具提交”的方向正确，但状态模型被 runtime、system、turn prompt、handbook、tool description、Schema 和 retry feedback 多次解释。

模型每回合真正需要稳定看到的只剩少数行为不变量：

- 最终文本只能是玩家可见正文；
- Actor State、Turn history、Director plan 的 source-of-truth 边界；
- 正文与提交状态必须一致。

具体操作字段属于现有 `oneOf` Schema；被拒模块如何重试属于工具结果；handbook 只提供当前有效值。每回合 Prompt 应缩成“本轮行动、必要状态/规则、输出目标”，不再完整复述工具协议。

### 6.4 Interactive Director Agent

Director 的写入边界和原子提交协议是清楚的。system/per-run prompt 可以缩短，让工具独占 Patch 表示、accepted/retry documents 和 finalize 语义。

per-run message 只需要：

- opening、keep、patch 或 replan 模式；
- 有界证据与精确 revision/hash；
- 哪些文档需要决策；
- 当前规划目标。

### 6.5 Configuration workflow

配置工作流由普通 General 或 Writing Agent 加载 `/configuration` Skill 完成，是目前 progressive disclosure 最好的示例：

- root Skill 只定义共通事务流程；
- resource reference 按需读取；
- list/get/revision/apply/read-back 验证清楚；
- 批量读支持 partial success；
- mutation receipt 紧凑。

资源 Schema 和 mutation semantics 应由 `describe` 与 reference 权威提供，不应复制进 Agent system prompt。配置任务无需独立 runtime kind。

### 6.6 Agents Project maintenance

Agent Profile 作为受管 Agents Project 中的普通文件，复用标准 General Agent、多会话、Files 与 Project Versions。Trajectory 不注入默认 Prompt；只有任务需要证据时才通过只读 `trajectory://` 资源按需读取。维护型任务的完成条件可固定成四项：evidence、Profile Diff 或显式 no-op、validation、预期行为影响。

### 6.7 Image Agent

Image 流程已经把 `purpose`、有界 `source_context`、call-site system prompt、tool prompt、Skill 分开，这是好设计。

建议：

- `source_context` 只用一个短标签说明它是生成素材；除非内容的使用方式确实不同，不增加通用风险说明；
- Image system definition 只保留 Agent 固有工作流；`AGENTS.md` 与 `CREATOR.md` 由共通 project-instruction leading context 注入，preset/call-site 继续作为各自独立来源；
- 图片参数编码由 image tool Schema 负责，不在每个图片 Skill 重复；
- “生成中文图片 Prompt”如果是明确 provider/产品要求，应保留。它不同于把同一内部提示写成中英双份。

### 6.8 Version Summary 与 Tool Agent

这两类 Prompt 小而明确，方向正确。如果 provider 支持 strict structured output，结构化任务应优先使用 response schema，而不是只靠“仅输出 JSON”文字；后端校验仍需保留。

Version Summary 的最终中文摘要是用户可见内容，当前用英文内部指令要求它输出中文是合理的。

### 6.9 Context Compaction Agent

Compaction 任务本身复杂，因此可以比普通 Agent Prompt 长，但长度仍应由实际 merge 决策证明。保留 incremental merge、durable facts、冲突与不确定性、游戏/工作区差异和单一输出 schema；来源标签或解释如果不改变保留/丢弃决策就应省略。

可优化点：

- source-kind-specific 规则可压成小型矩阵；
- checkpoint schema 保持单一代码来源；
- 对完整 compaction request 做快照；
- 验证 stable leading context 与当前最终请求跨压缩保持不变。

### 6.10 Automation

Automation 已经注入 bounded trigger evidence，并区分 task prompt 与 scope。当前仍以 Markdown 串接 task instruction、host rule、evidence、confirmed summary、user prompt；建议减少成少数固定块，并只保留会改变本次执行的 scope、confirmed input 和 evidence。完整来源、Hash 与预算记录继续留在 host manifest，不必全部展示给模型。

本地化的默认 Automation Prompt 属于用户可见/用户可编辑的任务内容。中文用户获得中文默认任务是合理的；这不代表 infrastructure label 或 system guidance 也应双语重复。

## 七、工具 Description 与字段 Schema

### 7.1 应保留的好模式

- `submit_interactive_turn.state_changes.items` 使用互斥 `oneOf`；
- task、Skill、config、lore 等批量操作保留逐项结果；
- read/write/edit 的名称和输入结构能直接说明基本能力；
- Interactive 工具通过 retry modules/receipt 让模型只修复被拒部分；
- Tool descriptor 把 host scheduling、mutation、recovery、retention 留在运行时，不要求模型阅读这些内部机制。

### 7.2 具体缺口

#### `ask`

Schema 应直接告诉模型：

- `questions` 为 1–3 个；
- question `id` 和 option `value` 的稳定 ID 字符集与长度；
- `prompt`、option `label`/`description` 使用用户当前输入语言；
- free-text question 没有 options，必须 `allow_free_text=true` 且不能 multiple；
- choice question 必须有 2–3 个互斥选项；
- 必须且只能有一个 recommended option；
- `other` 是保留值，因为 host 会自动添加；
- label/description 的长度预期。

Ask 的模型输出不承担产品本地化职责；按钮、状态与宿主生成的权限选项继续由 message catalog 或宿主双语数据提供。

#### `task`

建议拆成：

- `start`：只接受非空 `starts`；
- `observe`：非空 `refs`，可带 cursor；
- `steer`：refs + input；
- `respond`：非空 interaction responses；
- `abort`：refs + reason。

`TaskRef.agent/session/run`、cursor、idempotency 和 detached 分别写在对应字段；partial-success 与 retry 信息由 tool result 返回，不放进每个输入字段。

#### `skill`

拆成 `list` 与 `read` 两个 variant。描述 query 匹配语义、limit 默认值/上限、`source`/`id` 的精确身份，以及每个 ref 都会独立返回结果。

#### `todo`

拆成 `read`、`update`、`replace`、`clear`。描述 `expected_revision` 的使用条件、mutation ID、各 mutation 合法字段，以及最多一个 `in_progress` 的不变量。

### 7.3 统一的 description 写法

Tool description 默认控制在一至三句，按以下顺序写；后面的信息不存在就停止：

```text
<动词开头的能力说明>。
Use when <仅凭名称无法判断的选择条件>。
<Schema 无法表达、且调用前必须知道的一项事务或副作用语义>。
```

字段 description 默认只写一句：

```text
<值的准确含义>；<省略时的行为或填写条件>；<必要的单位、ID 来源或互斥关系>。
```

具体规则：

- 字段名已经清楚表达的内容不复述，例如 `path` 不需要写“the path”；应补充 Project-relative/absolute local、来源或适用范围；
- required、enum、min/max、array bounds、pattern 和禁止额外字段由 Schema 表达，不再用 `must` 重复；
- operation 分支使用 `oneOf`，每个 variant 只展示该分支合法字段；
- default 与 omit 必须区分，尤其避免让模型用空字符串、空对象或 `null` 代替省略；
- 跨字段约束放在最近的 object/array/variant description；只有无法结构化时才进入顶层 tool description；
- 后台 provider、fallback 顺序、存储实现和内部校验流程默认不写，除非它们会改变模型选择、重试或解释结果的方式；
- tool result 负责说明本次哪些输入成功、哪些失败、下一次只需修复什么，不在初始 description 预演所有错误。

### 7.4 删除冗余的优先级

发现同一语义出现在多个位置时，按以下 owner 保留一个主要版本：

| 内容 | 保留位置 |
| --- | --- |
| Agent 的长期目标、每轮行为、最终输出形态 | system prompt |
| 是否选择某个工具 | tool description |
| 输入 JSON 的合法结构 | JSON Schema / grammar |
| 可选的多步领域流程 | Skill |
| 当前目标、状态和证据 | turn context |
| 本次失败原因与最小修复动作 | tool result |

允许的短重复只有一种：遗漏会频繁造成明显错误，而且第二处提醒紧邻实际决策点。即使如此也只重复结论，不复制整段理由和步骤。

## 八、建议的 Provider-visible Context

### 8.1 Provider-visible request layers

优先复用现有 `ContextLeadingMessage`，不为 `CREATOR.md` 新造另一套 Prompt composer：

```text
system definition
  runtime contract、output protocol、Agent built-in workflow

project instructions (early User-role context)
  AGENTS.md、CREATOR.md、其他明确属于当前 Project 的长期用户规则

other stable leading context
  selected preset/director、session state、stable reference snapshot

conversation history / compaction checkpoint

turn-dynamic context
  selected evidence、current state、call-site constraint

current user request
```

这里的 `stable` 只决定排序与缓存方式，不决定是否注入。preset、Director、session state 或 reference 仍必须与当前 Agent 和当前任务相关；否则即使内容完全稳定，也应省略或按需读取。

项目指令文件的目标 fragment contract：

- 每个文件分别保存 `Source`、`Purpose`、规范化 `Resource`、revision/hash 和硬上限；
- `Placement`: `ContextLeadingMessage`；
- `Role`: `User`；
- 模型可见内容只需要一个固定短标题和正文；除非 revision 会影响模型决策，否则不要把完整 metadata 渲染进 context；
- 使用 Agent context 配置中的高上限并在超限时明确失败，不静默截断指令；
- 生命周期：根目录 `AGENTS.md` 在前、`CREATOR.md` 在后，每轮位于 transcript 之前；缺失或空文件跳过，内容未变时 exact bytes 不变；Compaction 不把它们折叠进 checkpoint。

优先级用一句稳定规则说明即可：当前用户明确修改项目或创作规则时，以较新的请求为准；否则持续遵守对应项目指令。工具输入输出格式由工具本身决定，不在项目指令 wrapper 中重复。

迁移后已删除 `creatorSystemPromptFragment` 及所有平行 inline Creator 注入路径，不保留 fallback 或兼容层。所有项目 Agent 共用这个边界；是否让某个 Agent 不再消费 Creator 应作为单独的产品 scope 决策，而不是再引入另一种注入路径。

这是内部边界调整，不需要增加用户配置项。

### 8.2 每类规则只设一个主 Owner

| 关注点 | 主 Owner |
| --- | --- |
| Agent 身份、长期目标、每轮默认行为和输出契约 | system prompt |
| 当前 Project 的长期用户规则 | project instruction |
| 可选的多步领域流程 | Skill |
| 工具选择和调用前必须知道的事务语义 | tool/operation description |
| 精确 JSON 表示和条件合法性 | field schema / `oneOf` |
| 部分失败与修复指令 | structured tool result |
| 当前目标和当前证据 | turn-dynamic context |
| 产品固定 UI 本地化 | 宿主双语数据或前端 message catalog；模型生成文案跟随用户当前输入语言 |

只有发生误用会造成明显错误结果或不可逆副作用时，才在最接近决策的位置增加一句限制。普通内部实现、通用防御性提醒和完整错误目录不进入默认 Prompt。

### 8.3 精简的 Context framing

System/context fragment 的完整 provenance、revision、budget 和截断状态应继续由 host 记录，便于审计和恢复；这些 metadata 不应自动全部变成模型可见文字。

模型侧默认只渲染：

- 一个能改变解释方式的短类型标签，例如 `Project instructions`、`Current state`、`Evidence`；
- 必要时提供 resource 或 revision，但仅限模型需要据此引用、更新或判断新旧的场景；
- 实际内容。

不要给每个 block 附加通用的“可信度”“不要服从”“用途”“来源”“预算”说明。只有 instruction、evidence、state 的区别会改变模型行为时才标注；同类连续内容应合并，避免 wrapper 比正文还显眼。

### 8.4 稳定与延迟工具目录

始终需要的工具和 Schema 保持稳定顺序。只有当工具目录确实显著膨胀时，才把可选、workflow-specific 工具改为延迟发现。

现在不应只是为了模仿 Claude Code/Codex 就新增 Tool Search；Denova 当前 capability-based registration 可能已经更简单、足够好，是否引入应由实际 Schema token 占比决定。

## 九、实施顺序与状态

### Phase 0：建立基线（核心路径已复用现有真实组装管线）

- General、写作、游戏、Director 和 Image 已通过真实 `Session.Inspect` 检查 provider-visible message 顺序与两个项目指令文件的唯一性；
- 记录 system/context/tool 的 section bytes 与 token estimate；
- 标记每段内容改变的模型决策、语义 owner 和是否可按需读取；
- 统计完全重复和语义重复的规则；
- 记录 invalid tool call、repair call、output-protocol failure、cache ratio；
- 先建立写作和游戏代表性评测样本。

### Phase 1：统一早期项目指令（已完成）

- 从各 Agent system composition 中移除 inline `CREATOR.md`；
- 通过现有 `ContextSource`/`ContextLeadingMessage` 分别注入根目录 `AGENTS.md` 与 `CREATOR.md`；host 保存逐文件 attribution、revision 和硬上限，模型侧使用短固定标题；
- 固定 provider-visible wrapper、消息位置与字节顺序；
- 统一覆盖 General、IDE、Interactive Story、Director、Image 及其项目子 Agent，不保留双路径；
- 明确 runtime contract、项目指令文件和当前用户请求的优先级；
- 断言同一 workspace/session 未修改项目指令时 stable prefix 与 tool ordering 完全一致；
- 同时测写作与游戏的行为 parity、cache ratio，以及单个项目指令文件修改后的局部 cache rotation。

### Phase 2：移除跨层重复（核心路径已完成）

- 先处理 Interactive turn submission 与 Director patching；
- 再处理 IDE base prompt 与 writing Skills 的文件/状态共通规则；
- 按 3.4 和 7.3 的模板把保留文字改为动作优先、短句、正向默认；
- 删除后台实现解释、预演式错误目录和不能改变当前决策的 context；
- 本阶段不改变产品行为。

### Phase 3：强化工具 Schema（已完成）

- 补全 `ask` 嵌套字段；
- 把 `task`、`skill`、`todo` 改成 operation-specific `oneOf`；
- 保留宽容解码与批量 partial success；
- 增加 Schema snapshot 和 invalid-call regression。

### Phase 4：按 Agent 精简并评测（结构精简已完成，指标持续观测）

- 优先 Interactive Story 和 Director；
- 把 child Agent routing description 改为一句明确的 `Use when` 和预期产物；
- 简化 IDE writing system prompt 与 Skills；
- 再审 Image、Automation、Compaction 和小型结构化 Agent；
- provider-visible parity 确认后删除旧 renderer 或平行 Prompt 路径，不加兼容层。

### Phase 5：可选工具延迟加载（本轮明确不引入）

只有工具 Schema bytes 已成为上下文显著部分时才做。必须比较额外工具发现回合与缓存/token 收益。

## 十、未来改动的验证标准

### 10.1 结构验证

- 普通回合间 stable-prefix bytes 完全一致；
- 每个模型可见 block 都能映射到一个当前决策和唯一语义 owner；
- 同一规则不在 system、Skill、turn context、Tool 和 Schema 中完整重复；
- system definition 中不再含项目指令正文；
- `AGENTS.md` 与 `CREATOR.md` 各自最多注入一次，并始终位于 transcript 和当前请求之前；
- 修改 `CREATOR.md` 只更新其 revision/content，不改变更早的 `AGENTS.md` cache message；
- 每个注入 source 的 host metadata 都有 provenance、purpose、placement 和 hard limit，但模型可见 framing 只保留必要字段；
- 截断不能移除 closing delimiter；
- 工具顺序和 Schema Hash 可重复。

### 10.2 行为验证

- 删除或缩短 context 后，代表性任务成功率和输出质量不下降；
- Tool 第一次调用能从 description 和 Schema 得到足够信息，无需依赖 system prompt 中的重复教程；
- writing lite/standard 仍按用户范围产出并正确同步 durable state；
- 游戏正文、RuleResolution、Actor State、choices、Director update 一致；
- partial module rejection 只重试被拒模块；
- Director opening/keep/patch/replan 与原子发布正确；
- Ask 第一次就能产生与用户当前输入同语言的合法 UI 数据，且不重复翻译；
- Compaction 保留用户意图、source boundary、当前请求和恢复引用。

### 10.3 指标

- 各 Agent kind 总 input tokens，以及 system/project/history/turn/tool Schema 的占比；
- 删除前后完全重复和语义重复的规则数量；
- 代表性任务成功率、首次合法 tool call 比例和平均 repair turns；
- 端到端 latency 与模型输出质量；
- 各 Agent kind cached input tokens/cache-hit ratio；
- stable-prefix bytes/tokens；
- tool schema 总 bytes；
- output-protocol validation failures；
- 写作/游戏质量评测。

精简以行为评测为约束，而不是以“规则看起来完整”为目标。不能因为一句话涉及正确性或安全就默认永久保留；只有它解决了具体、可复现、无法由代码或 Schema 更好处理的问题，才值得占用默认 context。

## 十一、语言边界

建议把语言规则明确为写作规则：

- runtime/system prompt、host 注入上下文、工具 description、字段 description、模型可见工具反馈、代码注释、日志、固定 terminal 输出：英文；
- 用户创作内容和引用原文：保留原语言；
- 用户可见交互：中英本地化，优先用结构化 `zh`/`en` 或前端 message key；
- 创作输出与图片 Prompt：服从用户、故事、preset 或明确产品要求；
- 禁止把同一条模型内部 infrastructure message 写成 `English / 中文`。

仓库本地 `.agents/skills` 被调用时也是模型指令，应遵守同一套内部英文规则。这与产品 Skill 要求模型生成中文创作内容是两件不同的事。

## 十二、主要源码清单

Denova：

- `internal/agents/prompts/composer.go`
- `internal/agents/prompts/composition.go`
- `internal/agents/prompts/runtime_contract.go`
- `internal/agents/prompts/system.go`
- `internal/agents/prompts/product.go`
- `internal/agents/prompts/interactive.go`
- `internal/agents/prompts/chat.go`
- `internal/agents/prompts/model_tasks.go`
- `internal/agents/prompts/subagent.go`
- `internal/agents/context/*`
- `agent/definition.go`
- `agent/definition_engine_context.go`
- `agent/tools/*`
- `internal/agents/tools/*`
- `internal/agents/configresource/*`
- `internal/automation/*`
- `internal/app/image/*`
- `skills/*/SKILL.md` 与 configuration references
- `.agents/skills/*/SKILL.md`
- `config/agent_registry.go`
- `internal/agents/builder.go`

Claude Code：

- `/Users/bytedance/.codex/attachments/8a449d84-748d-4eac-a737-78f3db3e12f2/pasted-text.txt` — 用户提供的最新完整 system prompt 快照
- `../claude-code/src/constants/prompts.ts`
- `../claude-code/src/constants/systemPromptSections.ts`
- `../claude-code/src/Tool.ts`
- `../claude-code/src/utils/zodToJsonSchema.ts`
- `../claude-code/src/tools/AgentTool/built-in/*`
- `../claude-code/src/tools/AgentTool/prompt.ts`
- `../claude-code/src/tools/AgentTool/AgentTool.tsx`
- `../claude-code/src/tools/ToolSearchTool/prompt.ts`
- `../claude-code/src/tools/AskUserQuestionTool/prompt.ts`
- `../claude-code/src/tools/AskUserQuestionTool/AskUserQuestionTool.tsx`
- `../claude-code/src/tools/FileReadTool/prompt.ts`
- `../claude-code/src/tools/FileReadTool/FileReadTool.ts`
- `../claude-code/src/tools/FileEditTool/prompt.ts`
- `../claude-code/src/tools/BashTool/BashTool.tsx`
- `../claude-code/src/tools/SkillTool/prompt.ts`
- `../claude-code/src/utils/attachments.ts`
- `../claude-code/src/utils/messages.ts`

Codex：

- `../codex/codex-rs/protocol/src/prompts/base_instructions/*`
- `../codex/codex-rs/core/gpt-5.2-codex_prompt.md`
- `../codex/codex-rs/context-fragments/src/*`
- `../codex/codex-rs/core/src/context/user_instructions.rs`
- `../codex/codex-rs/tools/src/json_schema.rs`
- `../codex/codex-rs/tools/src/tool_spec.rs`
- `../codex/codex-rs/tools/src/responses_api.rs`
- `../codex/codex-rs/core/src/tools/handlers/view_image_spec.rs`
- `../codex/codex-rs/core/src/tools/handlers/plan_spec.rs`
- `../codex/codex-rs/core/src/tools/handlers/request_user_input_spec.rs`
- `../codex/codex-rs/core/src/tools/handlers/apply_patch_spec.rs`
- `../codex/codex-rs/core/src/tools/handlers/*_spec.rs`
- `../codex/codex-rs/core/tests/suite/prompt_caching.rs`
- `../codex/codex-rs/core/tests/suite/prompt_cache_key.rs`

## 十三、最终建议

后续仍应把这项工作定位成“Provider-visible context 质量优化”：目标不是让 Prompt 看起来覆盖所有情况，而是让模型在每个决策点只看到精简、足够、无重复的信息。

本轮已经把根目录 `AGENTS.md` 与 `CREATOR.md` 统一为独立的 User-role stable leading context，将游戏与 Director 的稳定/动态指令分层，并让 required、enum、边界和操作分支进入 Ask/Task/Skill/Todo Schema。后续修改应继续通过 `Session.Inspect` 审查真实最终请求，并用写作与游戏任务成功率、首次合法工具调用、input tokens、延迟和缓存表现验证；没有行为收益证据的通用安全说明不作为默认保留项。
