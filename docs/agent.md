# Agent 运行与恢复

## 实时对话与执行时间线

前端使用 `marked`、DOMPurify 和 highlight.js 渲染标题、列表、表格、引用与代码块，
支持复制代码和最终回复。仅允许 HTTP(S) 链接；模型输出中的脚本、表单和外部图片不会加载。
用户向上阅读时保持当前位置，可通过“回到最新输出”恢复跟随。

事件划分参考 [Pi Agent Core 的 Event Flow](https://github.com/earendil-works/pi/blob/main/packages/agent/README.md#event-flow)，
在现有 Go Agent 上实现，不引入另一个运行时：

- `model_start` / `assistant_delta` / `model_end`：每轮模型生成与公开回复增量。
- `tool_start` / `tool_end`：用 `call_id` 关联工具名、参数、结果预览、耗时与状态。
  参数校验失败、权限失败、等待审批等提前返回路径也会记录结束事件。
- `GoalCheck` / `ContextCompact`：验收与上下文整理状态。
- `agent_start` / `agent_end`、`turn_start` / `turn_end`：运行与回合边界。
- `tool_update`：工具完成参数校验并真正开始执行。
- `input_queued` / `input_delivered` / `input_removed`：运行中补充与追问的处理状态。

只有实际调用过的工具才显示工具卡片。普通问答可能直接生成回复，不会人为插入工具步骤。
模型内部推理、验收器 JSON 和上下文摘要不会作为正文流式显示。
工具参数预览最多 4000 字符，结果预览最多 6000 字符；大结果仍通过 Artifact 分页读取。

`GET /api/v1/agent/runs/:id/events` 提供 SSE，使用与普通接口相同的 Cookie 或
`Authorization: Bearer ...` 身份验证，不在 URL 中传令牌。前端通过 Fetch 读取流，
主人模式同样可用。首次发送 `run` 元信息，随后发送带递增 `id` 的 `activity`，
终态发送 `done` 并关闭；每 15 秒发送注释心跳。
可用 `?after=N` 或 `Last-Event-ID: N` 从已消费的事件后继续读取。
事件先持久化后通知订阅者，客户端断开不取消任务；“停止生成”通过取消接口单独执行。

DeepSeek 对话请求使用 `stream=true`、`stream_options.include_usage=true`，按工具索引
拼接流式参数并汇总用量。公开回复按约 80ms 批量保存；跨片段的配置密钥和 `sk-` 密钥
仍会脱敏。流缺少完成标记、JSON 损坏或达到输出限制时保留已有输出并显示失败状态。
SSE 中断后前端先读取运行快照，再携带游标重连，不重复提交用户消息或重复执行工具。
已完成和中断运行随会话恢复执行时间线；旧数据没有增量事件时使用原有最终回复。

前端资源构建：`npm ci && npm run build:agent`。提交包含 `static/agent-build/` 产物，
当前仅复制静态文件的 Docker 部署也可直接使用。

## Pi 风格的后端循环

设计对照、实现边界和文件导航见 [agent-pi-design.md](agent-pi-design.md)。
每轮先处理补充指令，再整理上下文、生成回复、校验及执行工具，最后结束回合。
工具调用和其结果之间不会插入新的用户消息。

工具参数按每个工具声明的字段、必填项、类型及 Todo 状态校验；不接受其他工具的字段、
`null` 或多个拼接的 JSON 对象。校验失败不调用执行前 Hook，也不会生成待审批操作。
`PreToolUse` Hook 可检查已校验的参数；`PostToolUse` Hook 可查看实际结果和状态。
后置观察失败会产生提示，保留真实回执。并行工具的 Hook 可能同时运行，Host Hook 应保证并发安全。

仅当整批工具均在只读并行白名单中，才并行执行。包含任意写入、审批、团队或工作流工具时，
整批按顺序执行。`AGENT_TOOL_CONCURRENCY` 设置服务范围的只读工具执行上限，默认为 3，
范围 1–8；设为 1 可使用串行模式。事件反映实际完成顺序，回传给模型的结果保持原调用顺序。
观察到取消后不再启动后续工具；工具 panic 转换为失败回执，不使工作进程崩溃。

`GET /runs/:id` 增加 `phase`、`turns`、`pending_tools` 与 `inputs`，所有路径均相对于
`/api/v1/agent`。当前页面的输入框在生成时仍可输入，并可选择“下一轮补充”或“完成后追问”。

```http
POST /api/v1/agent/runs/<run-id>/inputs
Content-Type: application/json

{"id":"client-request-id","mode":"steer","message":"重点检查边界情况"}
```

`mode=steer` 在下一次模型请求前投递；`mode=follow_up` 在当前任务自然完成并通过目标验收后投递。
每轮最多投递一条，优先处理补充指令。它们共享原 Run 的调用、Token、步骤和时间预算。
`id` 可省略；提供时，同一 Run 内相同 ID 与内容的重试不会重复入队。
同一 ID 携带不同内容返回 409。每个 Run 最多接收 32 条输入，接口沿用聊天身份与限额。

`DELETE /api/v1/agent/runs/<run-id>/inputs/<input-id>` 可移除仍待处理的消息。
输入状态为 `pending`、`delivered`（已加入上下文）、`removed` 或 `unprocessed`。
最终队列检查与结束封口位于同一事务；封口后新消息返回 409，前端保留草稿供下一次发送。
失败、取消、预算耗尽或重启时仍在等待的消息保留为 `unprocessed`，不会自动执行。

## 反向代理和限流

Agent 的 IP 配额使用 Gin 的 `ClientIP()`，与博客的可信代理配置保持一致。
直连请求使用连接来源 IP；只有来自可信代理的请求才解析转发 IP 头。

在反向代理部署中，将实际代理 IP 或受控的代理 CIDR 写入
`config.yaml` 的 `trusted_proxies`，或设置 `TRUSTED_PROXIES` 环境变量，多个值以逗号分隔。
例如，代理地址为 `172.18.0.2` 时可设置 `TRUSTED_PROXIES=172.18.0.2`。
示例地址必须替换为实际地址；不要设置为所有来源。代理地址变化后应同步更新配置。

`deploy/docker-compose.server.yml` 和 `deploy/docker-compose.prod.yml`
均支持传入该环境变量。不配置时保持不信任转发头，此时代理后的访客仍共享代理 IP 配额。
独立的 `cmd/agent` 服务默认不信任转发头，也可通过 `TRUSTED_PROXIES` 配置代理。
HTTPS 终止于代理时，只接受可信代理覆盖写入的单值 `X-Forwarded-Proto: https`。
这同时控制主人令牌访问和访客 Cookie 的 `Secure` 属性；非可信来源伪造该头不会生效。

SSE 响应设置 `X-Accel-Buffering: no` 和 `Cache-Control: no-cache, no-store, no-transform`，
并及时 Flush。Nginx 或其他代理应保留这些头，流式路径禁用缓冲与压缩，并保持至少
30 秒读超时。服务端仅对 SSE 连接滚动刷新写超时，兼容博客默认较短的全局写超时。

## 服务重启

启动时在同一个数据库事务中恢复状态：

- 运行中的 Run 标为 `interrupted`，解除 Session 的活动运行占用。
- 运行中的 Team 恢复为 `idle`，保留已持久化的成员上下文，可再次运行或关闭。
- 运行中的 Workflow 标为 `interrupted`，保留检查点；同名同提示词恢复时跳过已完成步骤。
- 执行中的 Approval 标为 `interrupted`，表示实际结果未知。

中断审批会保留在对应会话页面，并显示“操作结果待核实”。系统不自动重试该操作，
也不允许再次批准同一条审批。尤其是 MCP 操作，外部系统可能已经完成操作，
但本地尚未保存执行回执：应先核对目标系统，再决定是否提出新操作。
已经完成、拒绝或仍待批准的记录保持原状态。

审批允许、拒绝或执行失败后，回执与会话更新在同一事务中保存，下一轮模型可读取
实际结果。审批执行期间占用对应会话，避免新对话覆盖回执；重启中断也会写入未知结果回执。

## 后台结果和大结果读取

一次性 Job 使用关联 Run 的最终状态，包括 `failed`、`cancelled`、`blocked`、
`budget_exhausted` 和 `interrupted`，并在工作台显示错误原因。
周期任务保留调度状态与最近一次运行错误。

`artifact_read` 支持 `offset` 和 `limit`，位置单位为 Unicode 字符，默认及最大分页为
1500 字符。使用返回的 `next_offset` 读取下一页，直到 `eof=true`。
读取分页不会再生成新的 Artifact。

## 数据保留与索引

数据库首次升级时自动回填用户、会话、状态和收件人索引，正常写入、删除与索引更新
处于同一事务。调度器每秒仅查询活动 Job；记忆、笔记、任务和工作台查询按用户过滤，
审批按会话过滤，邮件按收件人过滤，不再加载其他用户的全部记录。

启动时和此后每小时清理运行历史，默认保留 90 天，可通过 `AGENT_RETENTION_DAYS`
设置为 1–3650 天。涉及 Session、Run、Todo、审批、Artifact、邮件、已结束 Job 和工作流。
活动任务、结果未知的审批、可恢复的中断工作流及其关联会话会保留；保留中的会话和
工作区引用的 Artifact 也会保留。用户批准的长期记忆、笔记、任务和团队不会自动删除。
历史限流计数仅保留最近 7 天。旧数据库升级时从升级时刻开始计算保留期。

清理释放的 bbolt 页可供后续写入复用，数据库文件不会因此立即缩小；部署前应备份
`.agent-data/agent.db`。修改了保留期后，下一次清理会按新配置执行。

## 回归验证

```sh
go test ./internal/agent -v
go test ./...
go vet ./...
go test -race ./internal/agent
```

竞态检测需要启用 CGO 并提供受 Go 支持的 C 编译器。
测试使用临时 bbolt 数据库和假模型，不连接真实 DeepSeek 或 MCP 服务。

对话与审批页面验证：安装项目 Node 依赖和 Playwright 浏览器后运行
`npm run test:agent`，覆盖实时增量、工具成功/失败结果、断线续接、停止、历史回放、
Markdown 排版与 XSS 过滤，以及主人/访客切换。
可设置 `PLAYWRIGHT_CHANNEL=msedge` 使用本机 Edge。
这些验证使用本地假模型/HTTP 流，不消耗真实 DeepSeek 调用额度。
