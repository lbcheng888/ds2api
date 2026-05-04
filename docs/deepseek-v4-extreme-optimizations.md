# DeepSeek V4 极限代理体验优化记录

目标不是替代 DeepSeek V4 Pro / Flash 官方 API，而是在编码代理运行时层面补齐官方 API 不负责的能力：工具协议修复、执行证明、子代理调度、重复工作抑制、失败归因和本地验证闭环。

## 已落地能力

### 1. 只说不做拦截

DeepSeek V4 如果输出“我将读取 / 修改 / 运行 / 启动代理”但没有真实工具调用，ds2api 不再把它当正常完成。

已覆盖：

- 中文：“现在直接动手”“把代码写到位”“现在实现”“现在修改”“现在写”
- 英文：“Let me examine”“I will run”“Let me trace and fill”
- 工具清单承诺：“使用 Read / Edit / Bash 执行以下步骤”
- 完成宣称：“已修改”“测试通过”“已启动 N 个子代理”但没有对应工具证据

结果：ClaudeCode / OpenCode 不会停在一句状态说明上空等。

### 2. Team Agents 自动修复与调度

当 `allow_meta_agent_tools=true` 且客户端真实暴露 `Agent` 工具时，明确的子代理启动承诺会被修成结构化 `Agent` tool_use。

已覆盖：

- “准备三个子代理”
- “三个同时启动”
- “同时启动三个代理”
- “分三路把代码写到位”
- “一个改 A、一个改 B、一个研究 C”

调度规则：

- 一轮最多 4 个后台 Agent。
- 同一用户回合已有后台 Agent 启动 / running / completed 证据时，不重复合成。
- 用户要求推进、实现、修复、修改、完成时，Agent prompt 要求直接编辑文件并报告 changed files / verification。
- 纯评估、分析、审查、研究任务保持 read-only。

### 3. 同批工具调用去重

DeepSeek V4 同一响应里重复发相同工具调用时，出口层会按规范化参数去重。

已覆盖：

- `Agent`：按任务描述 / prompt 去重。
- `Bash` / `exec_command`：按 command + cwd 去重。
- `Read` / `read_file`：按 path + offset + limit 去重。
- `Grep` / `Search` / `Glob`：按 pattern/query + scope 去重。
- `TodoWrite` / `update_todo_list`：数组内重复 todo item 去重。

结果：不会重复启动同名子代理、重复执行同一条命令、重复显示同一条任务。

### 4. 流式 missing-tool gate

流式 Chat Completions 在工具模式下会先缓冲疑似工具前承诺文本。

例如模型输出：

```text
Let me first understand the current state by examining key files.
```

如果本轮结束仍没有真实 `tool_calls`，ds2api 返回 `upstream_missing_tool_call`，不会先把这句正文流给客户端再正常 `stop`。

### 5. harness metrics

`/admin/dev/diagnostics` 暴露 harness 观测：

- `repairs`：最终输出修复命中。
- `streams`：流式工具块处理命中。
- `failures`：missing / invalid tool 判定。
- `dedupes`：重复工具调用和重复 todo item 丢弃数量。

结果：排障不再只靠人工读 raw stream。

## 本轮新增

### 1. 请求意图编译器

新增 `internal/harness/claudecode/intent.go`。

作用：

- 把用户“继续 / 按建议推进 / 直接动手”编译成执行授权。
- 把最终文本里的 read / inspect / search / edit / write / run / Agent 承诺编译成结构化意图。
- 把“已修改 / 已运行 / 已验证”编译成需要工具证据的完成宣称。
- 区分纯分析请求和执行请求。

### 2. Agent 调度器

新增 `internal/harness/claudecode/agent_scheduler.go`。

作用：

- 集中处理 Agent 数量、分工抽取、重复抑制、执行/只读 prompt 选择。
- `final_output.go` 不再维护另一套 Agent 合成规则，避免规则漂移。

### 3. 执行证据层

新增 `internal/harness/claudecode/execution_proof.go`。

作用：

- “已修改”必须有 Edit / MultiEdit / Write / apply_patch / 写入型 shell 证据。
- “测试通过 / 编译通过 / 已运行”必须有 Bash / exec / test / build 证据。
- “已启动 Agent”必须有 Agent 证据。
- “将要 / 准备 / Let me / I will”归类为未执行承诺。

### 4. 最终工具事务内核

新增 `internal/harness/claudecode/tool_transaction.go`。

作用：

- 把 final repair、工具解析、schema defaults、TaskOutput 校验、schema/meta 归一化、去重报告收成一个事务入口。
- `EvaluateFinalOutput` 已接入该入口，避免 final repair、Agent 合成、TaskOutput 校验和去重分散在多处。
- OpenAI Chat 非流式和流式出口已在返回客户端前接入最终工具闸门，重复探索不会穿透到客户端。

### 5. 跨轮执行账本

新增 `internal/harness/claudecode/execution_ledger.go`。

作用：

- 按最近 user turn 建立工具证据窗口。
- 记录真实 Read / Search / Edit / Write / Bash / Agent / TaskOutput。
- UI 任务工具不算真实执行证据。
- 给请求意图和执行证据层提供跨轮判断基础。

### 6. 重复探索熔断

新增 `internal/harness/claudecode/exploration_guard.go`。

作用：

- 同一当前任务窗口里重复 Read / Grep / Search / Glob / rg / grep / find / ls / Agent 时阻断。
- `go test`、build、写入命令不误判为探索重复。
- OpenAI Chat 非流式和流式出口返回前已接入，命中时返回 `upstream_repeated_exploration`。

## 为什么能超过官方 API 体验

官方 API 只返回模型输出。ds2api 在模型和编码客户端之间增加了代理运行时：

1. 模型只说不做时，协议层能识别并失败重试。
2. 工具调用畸形时，能按 schema 做确定性修复。
3. 子代理不是一句承诺，而是真实 `Agent` tool_use。
4. 重复任务会被去重或熔断。
5. 每次失败有分类、指标和回归样本入口。
6. Pro / Flash 可以按主线和子代理分工，而不是所有请求走同一种模型调用。

这类能力不依赖模型本身变强，而是让模型输出进入更严格的执行系统。

## 验证口径

每轮优化至少看这些指标：

- 首次可执行工具率：模型第一次回复是否产出真实工具。
- missing-tool 拦截率：只说不做是否被拦住。
- duplicate-tool 丢弃数：重复工具是否被出口层清掉。
- Agent 重复启动数：同一用户回合是否重复开代理。
- 编辑成功率：Edit / MultiEdit 是否基于最新 Read 精确匹配。
- 前台空等时间：reasoning-only 或状态文本是否被及时终止。
- 本地 gates：Go tests、unit-all、webui build、line gate。

## 后续顺序

1. 把 `RunFinalToolTransaction` 继续下沉到 Claude stream 出口。
2. 把重复探索熔断升级为可恢复策略：给模型明确要求进入 Read/Edit 小窗口，而不是只报错。
3. raw failure 自动归因并生成 golden。
4. 账号健康与 Pro/Flash 动态路由。
5. 做官方 API vs ds2api harness 的端到端基准。
