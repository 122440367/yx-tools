## Context

yx-tools 是 Go 1.22 单二进制 CLI，使用标准库 `flag` 定义 `test`、`upload`、`proxy` 等命令。结果上传集中在 `cmd/yx/main.go` 的 `doUpload`，当前函数会直接打印并 `os.Exit`，无法在全部成功、失败和取消出口统一发送结束通知。配置由 `internal/app/config.go` 写入权限为 `0600` 的 `yx-config.json`。

项目已有嵌入二进制的本地 Web UI，其中有一段把当前设置转换为 cron 参数的逻辑，但它依赖本地 Go API，参数和上传目标覆盖也不完整。`docs/` 适合作为无构建步骤的 GitHub Pages 静态发布根目录。行为合同见 `specs/feishu-task-notification/spec.md` 和 `specs/test-command-generator/spec.md`。

## Goals / Non-Goals

**Goals:**

- 将飞书建模为独立、显式启用的任务通知，覆盖 `yx test` 和 `yx upload` 的成功、失败、取消及耗时摘要。
- 保留测速、结果写入、结果上传和飞书通知各阶段的真实状态与错误链。
- 只依赖 Go 标准库接入飞书，并提供有界重试、幂等保护和取消收尾。
- App Secret 只存在于当前进程，不进入 yx-tools 管理的持久化配置。
- 为 GitHub Pages 提供无后端、无构建依赖、简体中文、移动端可用的单行 `yx test` 命令生成器。

**Non-Goals:**

- 不向飞书发送 IP、端口、单条延迟、速度或丢包明细。
- 不把飞书加入 `-upload`，也不替代 GitHub、Worker 或 Telegram 上传。
- 不给 `proxy` 或嵌入式本地 Web UI 增加飞书通知。
- 不支持飞书自定义机器人 Webhook、多个接收者或交互卡片。
- 不让 GitHub Pages 运行测速、验证凭据或保存任何表单状态。
- 首版不提供中英文切换或多行命令。

## Decisions

### 1. 飞书使用独立且显式的通知参数组

新增 `-notify feishu`，并配套：

- `-feishu-app-id`
- `-feishu-app-secret`
- `-feishu-receive-id`
- `-feishu-receive-id-type`，缺省为 `chat_id`

`notificationFlags` 统一绑定到 `test` 和 `upload`。保存过目标配置也不会自动启用通知，每条命令仍必须显式带 `-notify feishu`。一次命令只接受一个 receive ID；多人场景使用飞书群聊。App Secret 必须由每次启用通知的 CLI 调用提供，不从配置文件回退。

这比单个 `-feishu` 布尔开关更清楚地表达通知通道，也允许 `-upload github -notify feishu` 等组合。

### 2. 使用统一任务结果模型和单一收尾点

引入内部 `taskOutcome` 与 `uploadOutcome`，记录操作类型、开始时间、结束时间、单调时钟耗时、结果数量、上传模式/数量、分阶段状态和主错误。`doUpload` 返回结果和错误，不再调用 `os.Exit`。

`runTest` 和 `runUpload` 采用以下生命周期：

1. 解析参数；启用通知时解析非秘密目标字段，并要求当前 CLI 提供 App Secret。
2. 记录开始时间并执行测速或独立上传。
3. 固化结束时间和分阶段状态。
4. 生成不含结果明细的任务摘要。
5. 尝试发送飞书通知。
6. 通知成功后只保存非秘密目标字段。
7. 用 `errors.Join` 合并独立错误，在命令边界打印一次并决定退出码。

`yx test` 得到 0 条有效结果时视为失败。测速成功但结果上传失败时，总状态为失败，同时摘要分别显示“测速成功”和“上传失败”。只要用户显式请求的飞书通知最终失败，整体命令也返回非零，但输出必须说明主任务是否已经成功。

### 3. 用户取消使用独立的短收尾上下文

收到 Ctrl+C 后，主 context 立即取消测速、上传及其下游请求。若飞书通知已经通过参数预检，使用 `context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)` 创建独立收尾 context，尝试发送“已取消”纯文本摘要。

通知完成、5 秒到期或再次中断后退出。取消始终保持非成功状态，通知错误与取消错误合并，不能把取消改写成普通通知失败。

### 4. 飞书客户端位于独立 app 通知模块

新增 `internal/app/notify_feishu.go`，提供：

- `FeishuTarget`：App ID、当前命令提供的 App Secret、Receive ID、Receive ID Type。
- `TaskSummary`：操作、状态、开始/结束时间、耗时、结果数量、上传阶段信息和安全错误摘要。
- `NotifyFeishu(ctx, target, summary) error`。

客户端先调用 `/open-apis/auth/v3/tenant_access_token/internal` 获取 tenant access token，再调用 `/open-apis/im/v1/messages?receive_id_type=...` 发送 `msg_type=text`。接收类型固定 allow-list 为 `chat_id`、`open_id`、`union_id`、`user_id`、`email`。API 基地址或 HTTP 客户端可替换，供 `httptest.Server` 使用。

格式化与 HTTP 发送分离。响应同时检查 HTTP 状态、JSON 解码结果和飞书 `code`；错误用 `%w` 添加小写操作上下文，只在 CLI 边界处理一次。

### 5. 重试有总预算并避免重复消息

普通通知的鉴权和消息请求共享 10 秒总预算。初次请求之后最多重试两次：优先遵守有效 `Retry-After`，否则使用约 500ms、1s 的短退避。只重试明确的 429、临时 5xx 和能够确认未送达的连接错误。

消息发送生成一次稳定幂等标识并在所有重试中复用。如果飞书接口对某类请求无法保证幂等，就不重试可能已经被服务端接受的模糊发送超时，宁可报告通知失败，也不制造重复通知。取消通知受独立 5 秒预算约束，不延用普通通知的完整重试周期。

### 6. 摘要字段固定并进行秘密清洗

飞书首版使用发往单一接收者的纯文本。摘要包含：

- 操作：测速或独立上传。
- 总状态：成功、失败或已取消。
- 本机时区开始和结束时间，例如 `2026-08-16 21:30:05 CST`。
- 人类可读耗时，例如 `2分13秒`。
- 测速有效结果数量。
- 如有上传：上传目标、分阶段状态和上传数量。
- 如有失败：最多约 300 字符的脱敏原因。

首版不加入主机名、yx-tools 版本或完整命令参数。清洗函数接收本次已知的 App Secret、tenant token、GitHub/Worker/Telegram Token 等秘密并替换为 `[REDACTED]`。飞书错误只暴露 HTTP 状态、平台 code 和受限长度的安全 msg，不输出请求体、Authorization 头或完整远端响应。

### 7. 本地配置只保存非秘密飞书目标

`Config` 增加 `FeishuAppID`、`FeishuReceiveID`、`FeishuReceiveIDType`，不增加 `FeishuAppSecret`。缺失 receive ID type 时补为 `chat_id`，现有 JSON 无需迁移。

仅在通知成功后保存非秘密目标字段。App Secret 不写入 `yx-config.json`、日志或其他 yx-tools 管理的存储；但直接 CLI 参数仍会出现在 shell 历史和进程参数中，README 和网页必须持续提示这一风险。

### 8. GitHub Pages 使用模块化静态页面

在 `docs/` 增加 `index.html`、`style.css`、`generator.mjs`、`app.mjs`、`generator.test.mjs` 和 `.nojekyll`。`generator.mjs` 不依赖 DOM，负责：

- 声明 `yx test` 参数、默认值和稳定 argv 顺序。
- 校验条件必填、枚举和数值范围。
- 只输出偏离默认值或主动启用的参数；展开高级字段不会自动加入默认 flags。
- 根据平台和架构推导发布文件名，同时允许用户修改路径。
- 以 POSIX 或 PowerShell 规则生成安全的单行命令。

平台至少覆盖 README 中的 Windows amd64/arm64、Linux amd64/arm64/386、macOS amd64/arm64 和 FreeBSD amd64，生成 `.\yx_windows_amd64.exe`、`./yx_linux_arm64`、`./yx_darwin_amd64` 等匹配路径。页面不提供续行符或多行模式。

### 9. 页面状态瞬时存在，显示命令与复制命令分离

`app.mjs` 只维护当前内存中的表单状态。页面不使用 localStorage、sessionStorage、IndexedDB、URL 参数、分析脚本、远程字体、CDN 或后端 API；刷新后敏感和非敏感字段都恢复默认。

生成器同时维护：

- 真实命令：包含用户输入的 Token 和 App Secret，用于复制执行。
- 显示命令：默认将敏感值替换为圆点，降低肩窥风险。

用户可以通过显式开关临时显示真实敏感值。复制按钮始终复制真实单行命令，成功/失败反馈不得回显秘密。Clipboard API 不可用时，用户需先显示真实值，再手工选择复制。

### 10. 页面采用简体中文和渐进披露

首版界面只提供简体中文，CLI flags、平台名和枚举值保持必要的英文。页面使用居中的单列布局：基础测速参数优先，输入源、高级设置、结果上传和飞书通知用语义化 `fieldset`/`details` 分组。

所有控件使用可见 label、至少 44px 触控高度、可见 focus ring、字段邻近错误和 `aria-live` 复制反馈。375px 起无页面级横向滚动；长单行命令仅在预览容器内部滚动。动画限制在 150–300ms，并尊重 `prefers-reduced-motion`。

### 11. 使用 Go 与零依赖 JavaScript 测试锁定行为

Go 测试覆盖飞书协议、多种 receive ID type、纯文本摘要、时间与错误截断、App Secret 不落盘、0 结果失败、上传部分失败、通知失败非零、取消独立 context、Retry-After、重试上限、幂等标识和模糊超时不重试。

Node 内置 `node:test` 覆盖精简默认命令、所有上传模式、上传与飞书并存、平台/架构文件名、单行约束、POSIX/PowerShell 引用、条件校验、显示遮罩与真实复制，以及所有表单状态不进入 URL 或存储。无需 npm 依赖或打包步骤。

### 12. 压力测试决策清单

以下结论是三轮压力测试后的最终约束，编号与访谈问题对应：

1. 飞书通知覆盖 `yx test` 和独立 `yx upload`，不覆盖 `proxy`。
2. 用户显式请求的通知失败时，整体命令返回非零。
3. 摘要包含开始时间、结束时间、耗时、测速结果数量、上传目标/数量和失败原因；不加入主机名、版本或完整命令。
4. 命令生成器允许通过 `-feishu-app-secret` 直接生成可执行命令，不新增环境变量模式。
5. 对明确的临时错误执行有限重试。
6. 默认生成精简命令，只包含偏离默认值或主动启用的参数。
7. GitHub Pages 直接从主分支 `/docs` 发布，不增加构建部署工作流。
8. 测速流程结束但得到 0 条有效结果时，任务失败并返回非零。
9. 测速成功但结果上传失败时，总状态失败，同时分别展示测速成功和上传失败。
10. Ctrl+C 后使用独立 5 秒上下文尝试发送“已取消”通知。
11. 一次命令只通知一个接收目标；多人通知使用群聊。
12. 首版飞书消息使用纯文本，不使用交互卡片。
13. 开始/结束时间使用运行机器本地时区，耗时单独使用人类可读格式。
14. 飞书中的失败原因经过脱敏并限制为约 300 字符，完整错误保留在 stderr/cron 日志。
15. 网页命令预览默认遮罩秘密，复制真实命令，并提供显式显示敏感值开关。
16. “展开高级参数”只展示更多字段，不把未修改的默认值写入命令。
17. 保存目标配置不会自动通知，每条命令都必须显式使用 `-notify feishu`。
18. App Secret 不保存；App ID、receive ID 和 receive ID type 可在成功后保存复用。
19. 重试复用稳定幂等标识；无法保证幂等时不重试可能已经送达的模糊发送超时。
20. 普通通知总预算 10 秒，初次请求后最多重试两次，遵守 `Retry-After`，否则约 500ms、1s 退避。
21. 页面同时支持 POSIX 和 PowerShell，并按平台/架构生成实际发布文件名，如 `./yx_linux_arm64` 和 `.\yx_windows_amd64.exe`。
22. 页面只生成单行命令，不提供多行或续行模式。
23. 页面不保存任何敏感或非敏感表单状态，刷新恢复默认值。
24. 首版页面只提供简体中文。

## Risks / Trade-offs

- [0 条结果改为非零退出可能影响现有脚本] → 在 README 和发布说明中标为行为变化，并增加回归测试。
- [CLI 收尾重构影响现有上传输出] → 限制重构范围，用现有 api/worker/github/telegram 测试锁定持久化、输出和错误行为。
- [通知延迟命令退出] → 普通通知总预算 10 秒，取消收尾预算 5 秒。
- [发送超时重试可能重复通知] → 复用幂等标识；无法保证幂等时不重试模糊发送超时。
- [通知失败使成功主任务最终非零] → 输出明确区分主任务结果与通知结果，这是显式请求通知后的完整性选择。
- [App Secret 仍会出现在 shell 历史和进程列表] → 永不落盘、输入/预览默认遮罩，并在 CLI 文档和网页复制区域持续警告。
- [静态页面参数与 Go flags 漂移] → 同一变更必须同步帮助、README、生成器元数据和 Node 测试。
- [GitHub Pages `/docs` 设置属于仓库外配置] → 提交可直接发布的静态目录和 `.nojekyll`，记录一次性 Pages Source 设置。

## Migration Plan

1. 添加不含 App Secret 的向后兼容配置字段、飞书客户端和单元测试。
2. 重构 `test`/`upload` 收尾路径，接入状态语义、取消通知、重试和独立通知 flags。
3. 添加静态命令生成器、平台/架构映射及 Node 测试。
4. 更新 CLI 帮助、README 和行为变化说明，运行全部验证。
5. 在 GitHub Settings → Pages 中选择主分支 `/docs` 发布并进行手工可访问性与隐私检查。
6. 回滚时移除通知 flags/客户端和静态页面即可；旧版本会忽略配置中的新增非秘密字段。
