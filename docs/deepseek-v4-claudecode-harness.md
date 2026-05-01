# DeepSeek V4 Pro[1m] ClaudeCode Harness

## 目标

让 ClaudeCode 继续负责终端 UI、权限、工具执行和 Team Agents 展示；ds2api 负责把 DeepSeek V4 Pro[1m] 规范成 ClaudeCode 能稳定消费的模型协议。

不 fork ClaudeCode，不依赖泄露源码，不复制客户端实现。ClaudeCode 只当黑盒客户端，harness 只实现公开可观察的请求、响应、工具调用和流式行为兼容。

## 架构

```text
ClaudeCode
  -> Anthropic/OpenAI-compatible request
  -> ds2api client profile
  -> ClaudeCodeHarness
       - request normalization
       - prompt contract
       - final output repair
       - tool dialect parsing
       - schema validation
       - Team Agents state rules
       - failure classification
  -> DeepSeek V4 Pro[1m]
  -> stream state machine
  -> ClaudeCode-compatible tool/text events
```

## 模块边界

1. `client profile`
   - 识别 ClaudeCode、OpenCode、Codex。
   - 每个客户端有独立 prompt contract、tool policy、流式输出要求。

2. `request normalizer`
   - 保留真实执行工具。
   - 过滤 TaskCreate、TaskUpdate、TodoWrite、TodoRead 这类 UI 追踪工具。
   - 保留 Agent、TaskOutput，并按 Team Agents 规则限流。

3. `prompt compiler`
   - 固定工具调用 XML/JSON 契约。
   - 固定 DeepSeek V4 Pro[1m] 的 `reasoning_effort=max`。
   - 明确：需要读、改、跑、验证时必须发工具调用，不能只说“我会继续”。

4. `tool dialect parser`
   - 支持 `<tool_calls>`、`<ToolCall>`、`<invoke>`、`<function_call>`、`<tool_use>`、裸 JSON tool object。
   - 支持 reasoning 和 late title 中的工具调用提升。
   - 无法唯一确定工具时返回协议错误并落 raw sample。

5. `schema validator`
   - 按客户端传入 schema 校验。
   - 只做确定性修复，例如 `filePath -> file_path`、字符串数字转 number、`globe -> glob`、`{command}` 推断 Bash。

6. `stream runtime`
   - 用状态机处理 reasoning、visible text、tool buffer、FINISHED、title、close、empty output。
   - reasoning-only 超时后走账号 failover，不能让 ClaudeCode 前台长时间空等。

7. `Team Agents runtime`
   - 一轮最多 4 个后台 Agent。
   - TaskOutput 只能使用当前 prompt 中 running/completed 或 task-notification 里的 task id。
   - 子代理默认走 `deepseek-v4-flash`，主线保留 `deepseek-v4-pro[1m]`。

8. `golden tests`
   - 每个 `tests/raw_stream_samples/failure-*` 都必须变成回归测试。
   - 测试输入 raw stream，断言输出工具调用、错误码或可见文本。

## 当前落地

首版代码落在 `internal/harness/claudecode`。

已集中：

- Task notification -> TaskOutput
- “等待 agent 返回” -> TaskOutput
- thinking 中合法工具调用 -> visible tool calls
- visible text 中裸 JSON tool object -> tool calls，并保留前后普通文本
- 流式完整 XML/JSON tool block 解析统一走 harness；OpenAI adapter 只保留增量缓冲和代码围栏状态
- 流式增量缓冲、代码围栏状态和 XML/JSON 捕获状态机统一走 harness；OpenAI adapter 只保留 HTTP/SSE 出口薄封装
- 嵌套 `<tool_calls>` 与 `Agent({...})` 函数式 tool body 解析
- 空 `<tool_calls>` 容器风暴会被识别为协议噪音，再按可见承诺合成 Agent 或交给 missing-tool gate
- “启动 Team Agents/并行代理” 承诺 -> 4 个后台 Agent calls
- TaskOutput task_id 合法性校验辅助
- “承诺读/改/跑/启动代理但没有工具调用” -> 协议错误
- 历史 raw failure 样本回放，覆盖 Team Agents 承诺、非法工具语法、空 `<tool_calls>` 容器风暴、中文“让我定位”类漏判
- Team Agents 状态账本集中维护 running/completed/missing/notification task id，TaskOutput 只能引用当前仍有效的 id
- 流式工具调用采用事务边界：进入 XML/JSON 工具块后如果 stream 结束仍不完整，直接返回 `upstream_invalid_tool_call`，不再把半截工具块当普通文本泄漏给 ClaudeCode
- `tests/scripts/add-raw-failure-golden.sh <sample> auto` 可按 raw sample 的 meta/stream 信号确定性加入 golden
- 工具提示已针对截图中这类 `Error editing file` 做收敛：`Edit` / `Update` / `MultiEdit` 必须使用从最新 `Read` 精确复制的 `old_string`，并在编辑失败或文件刚被改动后先重新读取再重试。
- “我正在并行处理三项改动”这类进行中状态文本如果没有真实工具调用，会按 missing-tool 处理，避免 ClaudeCode 前台停在一句状态说明上。
- “编译通过，现在运行测试验证”这类命令执行承诺如果没有真实 Bash / execution tool，会按 missing-tool 处理，避免模型说要跑命令但前台空等。
- DeepSeek V4 Pro 原生支持并行工具调用，harness 不再合并多个 Bash / execute_command / exec_command。ClaudeCode 客户端会收到多个独立的 tool_use 事件，客户端负责并行分发执行。
- DeepSeek 返回“有消息正在生成，请稍后再试”时，client 层会先做短退避重试，并保留正常 SSE 首行回放，避免把临时会话忙碌直接暴露成 ClaudeCode 的 `API Error: 502`。
- fenced JSON 中的 `{"tool":"..."}` 不再被当作普通文本放行；它不是 Claude Code 可执行 tool_use，会按 missing-tool 触发补偿重试。
- “已集成到 handler_chat.go / 已更新 / 测试通过”这类完成声明如果最近上下文里没有 Edit / MultiEdit / Write / Bash / Update 等真实执行工具证据，也会按 missing-tool 处理，避免模型凭空宣布已改代码。
- OpenAI Chat / Responses 主路径已接入 managed-account completion failover：无账号侧文件引用时，session / PoW / completion 状态失败会切到下一个账号并重建 session/payload；有 `ref_file_ids` 时不跨账号切换，避免文件上下文错绑。
- managed-account 调度已支持会话亲和：同一调用方携带相同 conversation / thread / session 标识时，默认 3600 秒内优先续租同一个 DeepSeek 账号，减少 DeepSeek V4 Pro[1m] 长上下文和账号侧缓存浪费；账号失败后亲和关系会迁移到 failover 后的新账号。Claude Code 未显式携带会话标识时，会从首个足够长的用户任务、模型和工作目录提示派生弱亲和 key，短“继续”类请求不会派生。

OpenAI Chat Completions 和 Responses 的流式、非流式出口都调用同一个 final-output repair 入口，后续新增失败样本优先补 harness 和 golden tests。
Claude stream 的实时 finalize 也复用同一套 harness 检测和修复入口。
最终响应格式化器直接消费 harness 归一化后的 tool calls，不再二次猜测或重新解析同一段文本。

## 不做的事

- 不 fork ClaudeCode。
- 不依赖泄露源码或私有实现细节。
- 不把畸形工具调用当普通文本返回给客户端。
- 不用启发式兜底“猜”业务语义；只能做可证明的协议层确定性修复。
- 不让后台 Agent 无限并发；一轮上限固定为 4。

## DeepSeek V4 Pro[1m] 专用策略

DeepSeek V4 Pro[1m] 的强项是长上下文、强 reasoning、代码规划和大规模上下文整合。ClaudeCode harness 的目标不是把它伪装成 Claude，而是把这些强项稳定接到 ClaudeCode 的工具协议上：

1. 长上下文走 prompt cache 和 HISTORY 切分，减少重复上下文扰动。
2. reasoning 允许很深，但只要出现可执行工具调用，就必须提升成客户端可消费的 tool call。
3. 子代理用 `deepseek-v4-flash`，主线保留 `deepseek-v4-pro[1m]`，避免 Team Agents 把主线预算吃空。
4. 所有工具调用按 schema 排序、校验、归一化，减少同一任务多轮漂移。
5. 失败必须分类并保留 raw sample，样本必须进入回归测试。

## 实现原则

1. 请求进入时识别 client profile，ClaudeCode、OpenCode、Codex 不共享一套宽泛提示词。
2. 工具 schema 是唯一真源；提示词只是约束，实际出站必须 schema 校验。
3. 所有 final output 修复、工具抽取、TaskOutput 校验、missing-tool 判定走 `claudecode.EvaluateFinalOutput`，不再在各 adapter 里复制顺序逻辑。
4. 所有 Team Agents 状态只信当前 prompt 中的 task state 和最新 task notification。
5. 任何“我将读取/修改/运行/启动代理”但没有真实工具调用的输出，都应转成工具调用或协议错误。
6. stream sieve 是工具块事务边界，不完整工具块必须失败并落 raw sample，不能回退成可见文本。

## 后续收敛顺序

1. 增加 Admin 观测：按 client profile 的失败率、账号 failover 次数、golden 自动分类建议。
2. 对 ClaudeCode、OpenCode、Codex 拆独立 profile，不再共享一套 prompt 补丁。
3. 将更多 `tests/raw_stream_samples/failure-*` 升级为明确 golden 断言，而不是只做 smoke replay。
4. 继续把 Claude stream 出口的工具事件组装也薄封装到 harness，adapter 只负责协议编码。
5. 为 ClaudeCode 专门补“工具失败后的下一步策略”：编辑失败 -> 重新 Read；测试失败 -> 读取失败片段并最小修复；上下文过大 -> 自动转 history/current-input-file。
6. 把 raw sample 分类从离线测试推进到线上闭环：失败码、profile、账号、模型、工具名、修复策略和重试结果进入同一条可检索诊断记录。
7. 做真实 ClaudeCode 端到端基准：官方 API、DeepSeek V4 Pro[1m]、DeepSeek V4 Flash 子代理分别跑同一组读改测任务，比较首次可执行工具率、编辑成功率、空等时间和最终 gate 通过率。
