# Agent 运行与恢复

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

审批页面验证：安装项目 Node 依赖和 Playwright 浏览器后运行
`node --test tests/agent-browser.test.mjs`。
可设置 `PLAYWRIGHT_CHANNEL=msedge` 使用本机 Edge。
