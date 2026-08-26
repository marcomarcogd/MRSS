# Changelog

All notable changes to MRSS will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.0.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [1.7.0] - 2026-08-19

### 中文

#### 24 小时 AI 日报

- 新增独立的“日报”中心，可按本地时间每天汇总上一个计划边界至当前边界之间的订阅文章；首次启用后的首份定时日报覆盖完整 24 小时，并正确处理夏令时造成的 23/25 小时周期。
- 支持全部订阅或指定订阅、关注重点、AI Profile、报告语言、标题模板和 1–12 个可编辑栏目；AI 目录草案必须由用户确认后才会保存。
- 生成前会刷新所选订阅；单个订阅失败或超时不会丢弃本地数据，报告会以“部分完成”状态继续生成。无文章时不会调用 AI。
- 程序会先清洗 HTML、去重并按标题、RSS 原摘要、关注重点、栏目要求和来源多样性免费筛选文章；每栏最多选择 8 篇，总量控制在 16–40 篇范围内，不使用本地算法伪造 AI 摘要。
- 默认“AI 摘要并保存”会复用来源、正文和 AI 配置均匹配的已有 AI 摘要，只为其余入选文章批量生成普通文本摘要，并立即写回文章缓存；随后打开单篇文章可直接复用，不再重复消耗 Token。
- AI 生成中断后会保留已经成功写回的文章摘要和报告进度，继续生成不会重复处理已完成内容。AI 不可用、达到限额或返回错误时会暂停，不再静默降级为 TextRank。
- 新生成的日报改为由程序将 AI 返回的普通文本、Markdown 或 HTML 清洗并转换成安全的段落、子标题和有序/无序列表；重复栏目标题、整份报告中串入的其他栏目、原始标签和重复内容会在保存前处理，旧版日报继续按原格式兼容显示且不会被重写。
- 栏目内容按较小来源分块生成，并根据来源数量和剩余用量动态分配输出预算；识别 OpenAI/OpenRouter、DeepSeek、Claude、Gemini 和 Ollama 的长度截断后，MRSS 会保留最后完整句子并只续写未完成部分，最多自动续写三次。仍未完成时保留进度供用户继续，不把半截内容标记为完成或自动改成本地摘要。
- 文章批量摘要遇到截断时会从每批 6 篇缩小到 3 篇，再缩小为单篇；已经成功保存的摘要和报告内容块不会重复生成。相同来源且高度相似的段落或列表项会去重，不同来源的相似观点仍会保留。
- 日报设置新增独立的“文章摘要方式”。只有用户明确选择“本地 TextRank”时才执行离线本地摘要；该选择不受全局文章摘要来源影响。
- 云端内容处理默认关闭。每个用户首次启用定时日报、手动开始 AI 生成或使用 AI 优化目录前，都必须在授权弹窗中确认实际 AI Profile 和脱敏端点，并明确同意发送标题、RSS 摘要、本地正文缓存、关注重点和目录要求；未授权时不会发出任何 AI 网络请求。
- AI 优化目录会自动处理常见返回格式；格式异常时会自动修正一次，仍失败则提示更换模型或手动编辑，并保留现有目录。
- 授权可随时撤销；切换 AI Profile 或端点会立即使旧授权失效并暂停定时日报，只有同一端点的模型变化无需重复授权。未配置可用 AI 服务时仅执行本地降级生成，不尝试默认远程地址。
- 日报不会逐篇访问原网页。云端生成由客户端直接连接用户选择的 AI 服务，可能产生对应服务的 Token 费用并受第三方数据保留政策约束；MRSS 不运营中转服务器，API Key、自定义鉴权值和端点查询密钥不会写入日报历史、日志或配置快照。

#### 历史、漏跑与通知

- 新增日报历史列表与结构化详情，支持状态筛选、分页、已读状态、继续 AI 生成、主动改用本地摘要、删除、复制 Markdown 和下载 Markdown；栏目内来源引用可打开现存 MRSS 文章，文章已清理时仍保留标题、订阅、作者、时间和 URL 快照。详情页和 Markdown 文末不再重复罗列全部来源。
- 生成中的日报详情现在显示当前阶段、分析进度、文章数量、输入/输出 Token 和总体进度；生成失败、完成或中断后会立即切换为最新结果，不再出现左侧状态已变化而右侧仍停留在旧内容的问题。
- 新增独立调度器，不受订阅刷新模式影响；同一周期不会重复生成，应用退出时会妥善停止尚未完成的任务。
- 应用关闭期间漏跑后，可选择补最近一期、补齐全部或全部跳过；跨过单期睡眠会自动补跑，跨过多期会保留提示直到用户明确处理。
- 新增应用内提醒、系统通知和无内容提醒选项。系统通知只在用户主动启用时请求权限；拒绝授权或发送失败会写入日志并回退到应用内状态，不影响报告保存。服务器模式继续生成日报，但不发送桌面系统通知。
- 每份完成日报现在最多发送一次 Windows 系统通知和一次 MRSS 应用内提醒。Windows 使用单一基础通知，正文优先显示“重点速览”的首条纯文本内容；重复完成回调和重复点击会按日报 ID 去重，点击通知只聚焦应用并打开对应日报一次，不再触发额外通知。

#### 数据与兼容性

- 日报配置、历史和来源信息单独保存，并为文章记录不可变的首次收录时间；旧文章优先按发布时间回填，缺少发布时间时使用迁移时间。
- 默认排除隐藏文章，但包含已读和未读文章。自动日报不会重复引用已被成功定时报告收录的文章，手动日报允许再次选择同一周期。
- 同步上游 v1.3.27：桌面应用运行时在 `127.0.0.1:1234` 向本机集成提供 REST API，并限制为回环访问和受保护的跨域请求。
- 构建基线升级到 Go 1.27 与 Wails v3 beta，并吸收设置保存、更新进度、活动栏布局、macOS 签名、空标签管理和重要文章清理等上游修复。
- AI 搜索现在按标题、摘要和正文命中排序，显示匹配词、命中字段和安全的上下文片段；列表与卡片结果均可打开文章，并按当前搜索结果切换上一篇或下一篇。
- AI 搜索、文章聊天、摘要、翻译、AI Profile 测试及订阅和第三方集成功能现在只显示简短、可本地化的错误原因，不再把服务商 JSON、鉴权信息或完整响应显示在页面；Toast 在窄窗口和连续长文本下也不会超出视口。
- 文章 AI 对话会在发送前保存用户问题、成功后保存回答和思考内容；新建或切换对话后可重新打开刚才的历史，AI 请求失败时已发送的问题仍会保留，同秒创建的会话与消息使用稳定顺序。
- 失败日报会先检查当前内容和设置是否仍可继续：可以继续时显示“继续生成”，内容或设置变化时显示“重新生成”。重试会更新当前日报，不会在列表中产生重复日报。
- 撤销日报云端授权只会撤销许可并暂停定时任务，不再回读旧配置覆盖设置弹窗中尚未保存的 AI Profile、目录或订阅草稿。只有切换 AI Profile 或实际端点时需要重新授权，同一端点仅更换模型仍沿用已有授权。
- AI 用量上限、进度和超限状态会随设置输入即时更新，保存后回读后端规范值；设置窗口打开期间会刷新实际用量。
- AI 配置测试会在密钥未修改时使用安全保存的真实密钥，输入新密钥时测试当前表单值，避免把掩码当作密钥造成鉴权误报。
- 日报文章摘要与栏目撰写改用普通文本协议，由程序负责文章编号映射、栏目结构、来源校验和 Markdown 组装，不再强制 DeepSeek、Gemini、Claude、Ollama 或 OpenAI 兼容服务返回严格 JSON；诊断日志只记录安全的 HTTP 状态和生成阶段，不记录密钥、文章内容或服务商响应正文。
- 文章批量写入遇到 SQLite 忙或锁定时会整批回滚并有限重试，重试耗尽后向刷新和日报链路返回真实错误，不再提交部分数据后报告成功。
- Windows 预发布测试同时提供安装包和带 `portable.txt`、许可证的独立便携 ZIP，避免便携测试误用安装版数据目录。
- Windows 安装包和便携包仍未使用 Authenticode 证书签名，Publisher 可能显示为未知，Microsoft Defender SmartScreen 仍可能提示。本版本不会绕过 UAC、Defender 或 SmartScreen。

### English

#### 24-hour AI daily reports

- Added a dedicated Daily Reports center that summarizes feed entries between adjacent local-time schedule boundaries. The first scheduled report after enabling covers a complete prior 24-hour window, while daylight-saving transitions correctly produce 23- or 25-hour periods.
- Reports can cover all feeds or selected feeds and support a focus prompt, AI Profile, report language, title template, and 1–12 editable outline sections. AI-generated outline drafts are never saved until the user confirms them.
- Selected feeds are refreshed before generation. A feed failure or timeout does not discard locally available entries; the report continues with a partial status. Empty periods do not call an AI service.
- MRSS first sanitizes HTML, removes duplicates, and ranks articles locally from titles, original RSS summaries, focus instructions, section requirements, and source diversity. It selects up to eight articles per section and keeps the overall AI workload within 16–40 articles without using a local algorithm to imitate AI summaries.
- The default “Generate and save AI summaries” mode reuses existing AI summaries only when provenance, article content, and AI configuration still match. Missing summaries are generated in portable plain-text batches and written back immediately, so opening those articles later does not spend tokens again.
- Interrupted generation retains successfully cached article summaries and report progress. Provider failures or usage limits pause the report and never silently switch to TextRank.
- Newly generated reports now convert AI plain text, Markdown, or HTML into program-owned safe paragraphs, subheadings, and ordered or unordered lists. Duplicate section headings, unrelated sections from an accidentally returned full report, raw tags, and repeated content are removed before saving. Existing reports keep their legacy rendering and are never rewritten.
- Sections are generated in smaller source-based parts with an output budget derived from source count and remaining usage. When OpenAI/OpenRouter, DeepSeek, Claude, Gemini, or Ollama reports a length stop, MRSS keeps the last complete sentence and continues only the unfinished part for up to three automatic continuations. Persistently truncated work remains resumable and is never marked complete or silently replaced by local content.
- Truncated article-summary batches shrink from six articles to three and then to individual articles. Successfully cached summaries and completed report blocks are not regenerated. Highly similar content is deduplicated only when it cites overlapping sources, while comparable views from independent sources are retained.
- Daily Report settings now include an independent article-summary mode. Offline TextRank runs only when the user explicitly selects it and does not follow the global article-summary provider setting.
- Cloud processing is off by default. Before scheduled reports, manual AI generation, or AI outline optimization can contact a provider, every user must review the actual AI Profile and redacted endpoint and explicitly consent to sending titles, RSS summaries, locally cached bodies, focus instructions, and outline requirements. No AI network request is made without that consent.
- AI outline optimization now handles common response formats automatically. If the format is invalid, MRSS makes one repair attempt, then suggests another model or manual editing while preserving the existing outline.
- Consent can be revoked at any time. Changing the AI Profile or endpoint invalidates the prior grant and pauses scheduling; changing only the model at the same endpoint does not require consent again. With no usable AI service configured, MRSS uses local fallback generation and never tries a default remote endpoint.
- Reports do not fetch each original web page. The client connects directly to the user-selected AI service, which may charge tokens and apply its own data-retention policy; MRSS operates no relay server. API keys, custom authorization values, and endpoint query secrets are never written to report history, logs, or configuration snapshots.

#### History, missed runs, and notifications

- Added paginated report history and structured details with status filters, read state, continued AI generation, explicit local fallback, delete, Markdown copy, and Markdown download. Inline source references open existing MRSS articles and retain title, feed, author, timestamp, and URL snapshots after an article is removed. The detail view and Markdown no longer append a duplicate full source list.
- Active digest details now show the current stage, analysis progress, article count, input/output tokens, and overall progress. Failed, completed, or interrupted runs immediately switch to their latest state instead of leaving the right pane on stale content.
- Added an independent scheduler that is unaffected by feed refresh mode. The same period is not generated twice, and unfinished work stops cleanly when the application exits.
- After missed periods, users can backfill the latest period, backfill all periods, or skip all. A single period crossed during sleep is backfilled automatically; multiple periods keep prompting until explicitly handled.
- Added in-app reminders, system notifications, and an optional empty-period notification. Permission is requested only when system notifications are enabled. Denied permissions or delivery failures are logged and fall back to in-app state without affecting the saved report. Server mode continues generating reports without desktop notifications.
- Each completed report now emits at most one Windows system notification and one MRSS in-app reminder. Windows uses one basic notification whose body prefers the first plain-text highlight. Duplicate completion callbacks and notification clicks are deduplicated by report ID; clicking focuses MRSS and opens the matching report only once without causing another notification.

#### Data and compatibility

- Report configuration, history, and source information are stored independently, with an immutable first-seen timestamp for articles. Existing articles are backfilled from their publication time when available, otherwise from migration time.
- Hidden articles are excluded by default, while both read and unread articles are included. Successful scheduled reports do not reuse already reported articles; manual reports may intentionally cover the same period again.
- Synchronized upstream v1.3.27: while the desktop app is running, it exposes a REST API for local integrations on `127.0.0.1:1234`, restricted to loopback and protected cross-origin requests.
- Raised the build baseline to Go 1.27 and Wails v3 beta, and incorporated upstream fixes for settings saves, update progress, activity-bar layout, macOS signing, empty tag management, and protected-article cleanup.
- AI search now ranks title, summary, and body matches and explains each result with matched terms, matched fields, and a safe context excerpt. List and card results both open correctly, with previous/next navigation scoped to the active search.
- AI search, article chat, summaries, translation, AI Profile tests, feeds, and third-party integrations now show short, localizable failure reasons instead of rendering provider JSON, authentication details, or full responses. Toasts remain within the viewport even in narrow windows or with unbroken legacy error strings.
- Article chat now saves the user's question before the request and persists the answer and thinking content after success. Newly created or switched conversations can reopen the previous history, failed requests retain the submitted question, and same-second sessions and messages have stable ordering.
- Failed reports first check whether the current content and settings can still continue. The interface shows Continue generation when possible and Regenerate after content or settings change. Retrying updates the current report instead of adding duplicates to the list.
- Revoking daily-report cloud consent now revokes permission and pauses scheduling without reloading saved settings over unsaved AI Profile, outline, feed, or notification edits. Reauthorization is required only when the selected AI Profile or actual endpoint changes; model-only changes at the same endpoint retain consent.
- AI usage limits, progress, and exceeded state update immediately while editing; saved values are normalized by the backend and actual usage refreshes while Settings remains open.
- AI Profile tests now use the securely saved key when it is unchanged and the current form value when a new key is entered, preventing masked keys from causing false authentication failures.
- Article summarization and section writing now use a portable plain-text protocol. MRSS maps article IDs, owns section structure, validates sources, and assembles Markdown locally instead of requiring strict JSON from DeepSeek, Gemini, Claude, Ollama, or OpenAI-compatible endpoints. Diagnostics record only the safe HTTP status and generation stage, never keys, article content, or provider response bodies.
- Article batch writes now roll back and retry as a unit when SQLite is busy or locked. Exhausted retries propagate a real error to feed refresh and daily-report refresh instead of committing partial data and reporting success.
- Windows pre-release artifacts now include both an installer and an isolated portable ZIP containing `portable.txt` and the required licenses.
- Windows installer and portable artifacts remain unsigned with Authenticode. Publisher may appear as unknown and Microsoft Defender SmartScreen may still warn. This release does not bypass UAC, Defender, or SmartScreen.

## [1.6.2] - 2026-08-18

### 中文

#### Windows 自动更新修复

- 修复 v1.6.0–v1.6.1 点击更新后 MRSS 退出、安装界面被隐藏且无法完成升级的问题。安装版现在由独立辅助进程等待主程序退出，通过 Windows 提权接口静默运行 NSIS 安装包，并在安装成功后自动重启 MRSS。
- Windows 便携版改为由同一辅助进程在主程序退出后替换可执行文件并自动重启，避免运行中的 EXE 无法安全覆盖自身。
- 每次下载使用独立临时目录；即使上一次安装器仍占用文件，重新下载也不会因同名临时文件被锁定而失败。失败和恢复过程写入 MRSS 数据目录下的 `update.log`。
- v1.6.1 及更早版本自身仍包含旧更新器，因此升级到 v1.6.2 需要一次性手动下载安装包或便携包；安装 v1.6.2 后，后续版本可继续使用应用内自动更新。

#### Windows 签名说明

- v1.6.2 的 Windows 文件仍未使用 Authenticode 证书签名。静默更新需要写入现有 Program Files 安装目录时，Windows 会显示 UAC 提权确认，发布者仍可能显示为未知；本版本不会绕过 UAC、Defender 或 SmartScreen。

### English

#### Windows auto-update fixes

- Fixed the v1.6.0–v1.6.1 regression where MRSS closed after Update was clicked while the installer UI was hidden and the upgrade could not finish. Installed builds now use a detached helper that waits for MRSS to exit, runs the NSIS installer silently through the Windows elevation API, and restarts MRSS after a successful installation.
- Windows portable builds now use the same helper to replace the executable only after the running process exits, then restart automatically.
- Every download uses a unique temporary directory, so a locked installer from an earlier attempt cannot make a retry fail at the same path. Update and recovery details are recorded in `update.log` beside the existing MRSS application log.
- Because v1.6.1 and earlier still contain the old updater, upgrading to v1.6.2 requires a one-time manual installer or portable-package download. In-app automatic updates work again after v1.6.2 is installed.

#### Windows signing notice

- Windows artifacts for v1.6.2 remain unsigned with Authenticode. Updating an existing Program Files installation may show a UAC elevation prompt with an unknown publisher. This release does not bypass UAC, Defender, or SmartScreen.

## [1.6.1] - 2026-08-18

### 中文

#### 修复

- 修复上游 Issue [#1006](https://github.com/DevXDojo/MrRSS/issues/1006)：手动清理数据库时不再清空全部文章与正文缓存，只删除同时满足“未读、未收藏、未加入稍后阅读”的文章。
- 已读、收藏、稍后阅读文章及其正文缓存现在会完整保留；被删除文章的关联数据仍通过现有外键级联清理，重复清理保持幂等。
- 移除手动清理操作对七天前翻译缓存和正文缓存的全局清理副作用，避免重要文章内容被误清除。

#### Windows 字体改进

- 内置 Inter Variable 与 Noto Sans SC Variable，并将 Windows 的“系统默认”界面与正文字体调整为 `Inter Variable → Noto Sans SC Variable → Segoe UI/微软雅黑` 回退组合。
- 字体通过锁定版本的本地前端依赖离线加载，不请求 Google Fonts 或其他字体 CDN；用户已选择的界面字体和正文字体继续分别优先，不修改现有设置或数据库。
- macOS 与 Linux 继续使用原有系统字体栈。内置 WOFF2 字体资源合计约 5 MB，因此各平台安装包体积会相应增加，最终大小以 Release 资产为准。

#### 许可与 Windows 签名说明

- Inter 与 Noto Sans SC 均按 SIL Open Font License 1.1 分发，版权声明与完整许可文本已加入 `NOTICE`。
- v1.6.1 的 Windows 可执行文件和安装包仍未使用 Authenticode 证书签名，Publisher 会显示为未知，Microsoft Defender SmartScreen 仍可能提示。本版本不通过关闭 Defender、修改注册表或安全策略规避提示。

### English

#### Fixed

- Fixed upstream Issue [#1006](https://github.com/DevXDojo/MrRSS/issues/1006): manual database cleanup no longer removes every article and cached body. It now deletes only articles that are simultaneously unread, not favorited, and not in Read Later.
- Read, favorite, and Read Later articles now retain their cached bodies. Related data for deleted articles still follows the existing foreign-key cascade, and repeated cleanup remains idempotent.
- Removed the manual cleanup side effects that globally deleted translation and article-body caches older than seven days, preventing protected offline content from being discarded.

#### Windows typography

- Bundled Inter Variable and Noto Sans SC Variable and changed the Windows system-default UI and article stack to `Inter Variable → Noto Sans SC Variable → Segoe UI/Microsoft YaHei`.
- Fonts load offline from exact-version frontend dependencies without Google Fonts or another font CDN. Existing custom UI and content font choices remain independently preferred, with no settings or database migration.
- macOS and Linux retain their existing system stacks. The bundled WOFF2 resources total about 5 MB, so platform packages will grow accordingly; final sizes depend on the Release artifacts.

#### Licensing and Windows signing notice

- Inter and Noto Sans SC are distributed under the SIL Open Font License 1.1. Copyright notices and the complete license text are included in `NOTICE`.
- The v1.6.1 Windows executable and installer remain unsigned with Authenticode. Publisher is therefore shown as unknown and Microsoft Defender SmartScreen may still warn. This release does not bypass Defender or modify Windows security policy.

## [1.6.0] - 2026-08-17

### 中文

#### 新增

- 新增文章翻译方式设置：手动翻译、自动翻译和关闭翻译。新安装默认使用手动模式；旧版已启用自动翻译的用户继续使用自动模式，旧版已关闭翻译的用户迁移到关闭模式。
- 在主文章页与弹窗文章页增加带文字的“翻译”操作，支持请求中防重复、失败重试、原文/译文切换及按文章、目标语言和翻译服务复用现有缓存。

#### 改进

- 重整“设置 → 订阅源”为紧凑的完整列式列表，保留名称/地址、分类、最新文章、更新频率、状态与操作，并继续支持图标回退、长文本提示、加载骨架、错误重试、原有顺序和可选排序。
- Windows 应用图标改为包含 16–256px 多级资源的透明 ICO；系统托盘使用独立的 16–32px 浅色/深色多级图标，并由同一 MRSS SVG 确定性生成。
- 设置弹窗在加载完成后建立保存基线，只提交实际变化的字段；单纯打开、切换标签或关闭设置不再重复保存整份配置。
- Windows 预发布检查实际构建 AMD64/ARM64 可执行文件和 NSIS 安装包，并上传测试产物；安装包构建失败会直接阻止检查通过。

#### 修复

- 修复 Windows 打开或关闭设置时由重复执行 `reg.exe` 引起的命令行窗口闪烁：启动项只在值变化时处理，并改用原生注册表 API，保存失败时会回滚系统状态。
- 修复 Windows 更新安装程序经 `cmd.exe` 中转以及 Python、PowerShell、Node.js 等后台订阅脚本创建可见 Console 的问题，同时继续记录标准输出、错误输出和失败日志。
- 修复订阅源设置列表中长标题、长 URL、缺失图标及大量订阅造成的布局错乱，不改变 Feed API、数据库结构或订阅排序数据。
- 修复点击订阅源整行时设置弹窗被错误关闭的问题；现在整行点击会打开编辑弹窗，FreshRSS 锁定订阅不响应编辑。
- 修复没有任何标签时“管理标签”弹窗因 `/api/tags` 返回 `null` 而闪退的问题，并增加加载、空状态、错误提示和重试。
- 修复手动或关闭翻译模式下打开文章仍可能自动请求翻译的问题，并限制正文翻译查询到当前文章容器，避免主视图与弹窗相互串用。

#### Windows 签名说明

- v1.6.0 的 Windows 可执行文件和安装包仍未使用 Authenticode 证书签名，Publisher 会显示为未知，Microsoft Defender SmartScreen 仍可能提示。此版本没有通过关闭 Defender、修改注册表或安全策略规避提示；正式代码签名能力按已确认计划延期处理。
- 桌面、任务栏、托盘图标以及 100%、125%、150%、200% DPI 下的实际显示需以本次 CI 生成的 Windows 测试包为准，不以 macOS 构建结果代替 Windows 实机验收。

### English

#### Added

- Added manual, automatic, and off translation modes. New installations default to manual mode; existing users who enabled automatic translation remain on automatic mode, while users who disabled translation migrate to off.
- Added a labelled Translate action to both the main and popup article toolbars, with duplicate-request prevention, retry support, source/translation switching, and reuse of the existing per-article, target-language, and provider cache.

#### Changed

- Reworked Settings → Feeds into a compact full-column list that keeps name/source, category, latest article, update frequency, status, and actions while preserving icon fallbacks, full-value tooltips, loading skeletons, retryable errors, stored order, and optional sorting.
- Rebuilt the transparent Windows app ICO with 16–256px frames and added dedicated 16–32px light/dark tray ICOs, all generated deterministically from the canonical MRSS SVG.
- Settings now establish a baseline after loading and submit only fields that actually changed. Opening, switching tabs, or closing the dialog no longer resaves the entire configuration.
- Windows pre-release checks now build real AMD64/ARM64 executables and NSIS installers and upload test artifacts; installer failures now fail the check.

#### Fixed

- Fixed command-window flashes when opening or closing Settings on Windows. Startup registration now runs only when its value changes, uses the native registry API instead of repeated `reg.exe` calls, and rolls back the system state if persistence fails.
- Removed the `cmd.exe` intermediary when launching Windows update installers and prevented Python, PowerShell, Node.js, and other background feed scripts from creating visible consoles while preserving stdout, stderr, and error logging.
- Fixed feed-setting layout breakage caused by long titles, long URLs, missing icons, and large subscription lists without changing the Feed API, database schema, or persisted feed order.
- Fixed feed-row clicks incorrectly closing Settings. Clicking an editable row now opens the edit dialog, while locked FreshRSS feeds remain non-editable.
- Fixed Manage Tags disappearing when an empty tag database returned `null`, with explicit loading, empty, error, and retry states.
- Prevented article-open translation requests in manual and off modes, and scoped body translation to the current article container so the main reader and popup cannot reuse each other's DOM.

#### Windows signing notice

- The v1.6.0 Windows executable and installer remain unsigned with Authenticode. Publisher is therefore shown as unknown and Microsoft Defender SmartScreen may still warn. This release does not bypass Defender or change Windows security policy; production code signing remains intentionally deferred.
- Desktop, taskbar, and tray rendering at 100%, 125%, 150%, and 200% DPI must be verified with the Windows CI test artifacts. macOS build results are not presented as Windows hardware validation.

## [1.5.0] - 2026-08-17

### 中文

#### 新增

- 同步上游 v1.3.26：支持翻译文章摘要，并补齐当前订阅、分类及未分类范围的“全部已读”行为。
- 新增本机中文字体检测，界面字体与文章正文字体可分别选择已安装的思源、Noto、更纱和霞鹜文楷系列字体，不下载或内置字体文件。
- 新增 MRSS 品牌图标，并统一桌面端、服务端、网站、Docker、Skill、安装包与便携包的产品名称和发行资产名称。

#### 改进

- 将复刻版正式更名为 MRSS，源码、问题反馈、应用内更新和发布下载统一指向 `marcomarcogd/MRSS`。
- 更新 Wails、前端依赖与 Tailwind CSS 4，并合并文章图标、快捷键显示、右键菜单及活动栏居中等上游改进。
- 界面字体与字号移动到“常规 → 应用”的语言设置下方；文章正文继续使用独立字体、字号和行高。
- 非便携模式首次启动会在数据库打开前安全迁移旧 `MrRSS` 数据目录；迁移失败时继续使用旧目录，新旧数据库同时存在时优先新目录且不改动旧目录。
- 新设置使用 `MRSS_*` 环境变量和 `MRSS-v1:` 加密标记，同时继续兼容旧 `MRRSS_*` 变量及 `MrRSS-v1:` 加密数据。
- 安装包、便携包、DMG、AppImage、Linux 系统包、Docker 镜像和 Skill 包均附带 GPL-3.0 许可证与对应源码入口。

#### 修复

- 修复 Atom 条目缺少可用链接时文章标题与正文串文的问题，空 URL 不再参与匹配，并使用标题和发布时间兜底。
- 修复无 `pubDate` 文章的首次抓取时间、卡片布局切换、当前范围全部已读及未分类订阅处理问题。
- 修复 macOS WebKit 中原生滚动槽导致活动栏菜单图标偏离中心线的问题，所有图标与底部操作保持对齐。
- 修复快速切换设置标签时待保存内容被取消的问题，并在更新下载过程中显示实时进度。
- 修复旧版文章阅读位置本地存储键未在启动时迁移的问题。
- 移除旧版向外部服务发送启动事件、设备标识和系统版本的统计代码，并在数据目录迁移时清理旧设备标识文件。

#### 升级说明

- v1.4.2 及更早版本使用旧仓库白名单，无法通过应用内更新自动安装 v1.5.0。请从 `marcomarcogd/MRSS` 的 Releases 页面手动下载并安装一次；升级到 v1.5.0 后，应用内更新会继续使用 MRSS 仓库。
- 便携模式仍使用程序旁的 `data/`，服务器模式仍使用当前目录的 `./data`，不会迁移或删除这些目录。

#### 开源与归属

- MRSS 是基于 `DevXDojo/MrRSS` 修改的非官方复刻版，修改日期为 2026-08-17，与上游维护者不存在官方隶属或背书关系。
- 本发行版遵循 GNU GPL-3.0，保留原始许可证、既有版权声明和 Git 历史；对应源码位于 `https://github.com/marcomarcogd/MRSS`，软件不提供任何担保。

### English

#### Added

- Synced upstream v1.3.26, including article-summary translation and correctly scoped mark-all-as-read behavior for feeds, categories, and uncategorized feeds.
- Added local detection for installed Chinese Noto, Source Han, Sarasa, and LXGW WenKai fonts. Interface and article typography remain independent, and no font files are downloaded or bundled.
- Added a distinct MRSS icon and unified product and asset names across desktop, server, website, Docker, skills, installers, and portable packages.

#### Changed

- Renamed the fork to MRSS. Source code, issue reporting, in-app updates, and release downloads now point to `marcomarcogd/MRSS`.
- Updated Wails, frontend dependencies, and Tailwind CSS 4, and integrated upstream improvements for article icons, shortcut labels, context menus, and activity-bar alignment.
- Moved interface font and size controls below Language in General → Application while keeping article font, size, and line height independent.
- On first non-portable launch, the legacy `MrRSS` data directory is migrated safely before the database opens. MRSS falls back to the legacy directory if an atomic rename fails and leaves it untouched when both databases exist.
- New settings use `MRSS_*` environment variables and the `MRSS-v1:` encryption marker while remaining compatible with legacy `MRRSS_*` variables and `MrRSS-v1:` encrypted values.
- Installers, portable packages, DMGs, AppImages, Linux system packages, Docker images, and skill packages now include the GPL-3.0 license and a corresponding source-code reference.

#### Fixed

- Fixed article title/content mismatches for Atom entries without usable links. Empty URLs no longer match, with title and publication time used as fallbacks.
- Fixed first-seen timestamps for articles without `pubDate`, card-layout switching, scoped mark-all-as-read behavior, and uncategorized-feed handling.
- Fixed native scrollbar gutters shifting activity-bar icons off the centre line in macOS WebKit, keeping every icon aligned with the bottom actions.
- Fixed pending settings changes being discarded when tabs are switched quickly, and added visible progress while an update is downloading.
- Fixed the legacy article reading-position local-storage key not being migrated during startup.
- Removed the legacy external startup analytics code that sent launch events, device identifiers, and system versions, and added cleanup for the old identifier file during data migration.

#### Upgrade notes

- Releases v1.4.2 and earlier trust the old repository and cannot install v1.5.0 through in-app updates. Download and install v1.5.0 once from the `marcomarcogd/MRSS` Releases page; later in-app updates will use the MRSS repository.
- Portable mode continues to use the adjacent `data/` directory, and server mode continues to use `./data`; these directories are not migrated or deleted.

#### Open-source attribution

- MRSS is an unofficial modified fork based on `DevXDojo/MrRSS`, modified on 2026-08-17, and is not endorsed by or affiliated with the upstream maintainers.
- This release is distributed under GNU GPL-3.0 with the original license, existing copyright notices, and Git history preserved. Corresponding source is available at `https://github.com/marcomarcogd/MRSS`; the software is provided without warranty.

## [1.4.2] - 2026-08-13

### Fixed

- Fixed article content mismatches for Atom entries without alternate links by excluding empty URLs from URL matching and using title and publication time fallbacks. (WCY-dt/MrRSS#999)

## [1.4.1] - 2026-08-13

### Changed

- Moved interface font family and size settings to General → Application directly below the language setting.
- Reused the font selector for interface and article typography, and expanded local detection for OFL-licensed Chinese sans-serif, serif, and WenKai families without bundling font files.

## [1.4.0] - 2026-08-13

### Added

- Added configurable interface font family and font size settings for feed lists, article lists, toolbars, settings, and dialogs while keeping article content typography independent.

### Fixed

- Fixed article cards reloading stale layout settings and preventing users from switching away from card layout. (#987)

## [1.3.27] - 2026-08-22

### Added

- Exposed the desktop REST API to local integrations on `127.0.0.1:1234`, with loopback and cross-origin request protections. (issue 1022)

### Changed

- Updated the Go build baseline to 1.27.

### Fixed

- Flushed pending debounced settings saves when settings tabs unmount. (issue 1011)
- Rendered the download progress already tracked by the update flow. (issue 1010)
- Kept activity-bar navigation aligned in macOS WKWebView. (issue 1008)
- Signed and strictly verified completed macOS app bundles before creating DMGs. (issue 1009)
- Fixed Manage Tags when the tag database is empty. (issue 1016)
- Preserved protected articles during manual cleanup. (issue 1006)

## [1.3.26] - 2026-08-16

### Added

- Added configurable interface typography settings for application fonts and sizes while preserving article content typography. (@marcomarcogd)
- Supported translation of article summarys. (#983)

### Changed

- Updated backend and frontend dependencies, including Wails v3 beta and Tailwind CSS 4.
- Standardized article and image gallery icons for favorites, copy link, and opening articles in the browser. (#980)

### Fixed

- Fixed the first-fetched timestamp for articles without a publication date. (@cos-y)
- Fixed switching away from card article layout without redundant per-article settings requests. (#974, #987, #988) (@marcomarcogd)
- Fixed article content mismatches for feed entries without links. (#999) (@marcomarcogd)
- Fixed activity bar icons not being centered consistently. (#975)
- Fixed Windows shortcut modifier labels and replaced the unset shortcut dash with a clear placeholder. (#976, #977)
- Fixed the mark-all-as-read shortcut so it respects the current feed/category, uses the same confirmation dialog as the toolbar, and handles uncategorized feeds correctly. (#978)
- Fixed article context menu ordering and renamed the original-content action to "View Original". (#979)

## [1.3.25] - 2026-07-19

### Added

- Added a downloadable Codex skill package for operating MrRSS through the local API.

### Fixed

- Fixed Linux release builds by upgrading Wails v3 to the latest alpha and installing GTK4/WebKitGTK 6.0 build dependencies.
- Fixed sidebar scrollbars so they stay hidden until the sidebar is hovered or focused. (#465)
- Fixed summary card layout jumps by reserving space before automatic summary generation starts. (#654)
- Fixed the image gallery "view article" action so it opens the selected article in the regular reader. (#572)
- Fixed image-mode feeds and categories switching regular article views into the image gallery. (#568)
- Added a shortcut from the article feed name back to that feed in the regular article list. (#561)
- Added per-article reading position restoration when reopening articles. (#445)

### Changed

- Updated release automation to publish `MrRSS-<version>-skills.zip` as a release asset.

## [1.3.24] - 2026-07-19

### Added

- Added targeted refresh actions for the current article, feed, and category. (#555, #594)
- Added keyboard shortcuts for toggling unread, favorites, and read-later article filters. (#590)
- Added support for using RSS-provided summaries directly. (#910)
- Added per-feed refresh interval support and Turkish translation options. (#695, #902)
- Added controls to disable update notifications and open article links in the system browser. (#801, #903)

### Changed

- Updated Go, frontend, website, and GitHub Actions dependencies for the release branch.

### Fixed

- Fixed database cleanup and refresh regressions that caused existing articles to be fetched again, lose cached data, or reappear after cleanup. (#802, #904, #946) (@rogeryk)
- Fixed repeated feed refresh behavior, unread filtering, bulk selection, and duplicate feed handling. (#562, #619, #669, #826, #873, #896, #917)
- Fixed FreshRSS-synced feeds remaining after the FreshRSS integration is disabled. (#797)
- Fixed lazy-loaded article images, XML encoding detection, proxy usage during full-text fetching, and content loading states for article rendering. (#655, #804, #853, #875, #876)
- Fixed RSSHub route query preservation and subscription failures for some feeds. (#631, #894)
- Fixed AI and translation provider compatibility issues, including Tencent, LibreTranslate, DeepSeek, Ollama, and newsletter sender handling. (#750, #911, #912, #919, #920, #942) (@atoz03)
- Fixed window state and close behavior issues on macOS. (#643, #716, #770, #913)
- Fixed compact/card layout visual jumps during initial settings load. (#663)

## [1.3.23] - 2026-03-26

### Fixed

- Resolved OPML import failure for self-exported feedURL attributes. (#781) (@kv-chiu)

## [1.3.22] - 2026-03-07

**BREAKING**: The logic operator precedence for filter conditions and rules has been standardized to `NOT` > `AND` > `OR`. This means that `NOT` conditions will be evaluated first, followed by `AND`, and then `OR`. Please review your existing filters and rules to ensure they behave as expected with this precedence.

### Added

- Supported floating TOC feature for articles. (@MidnightCrowing)
- Supported Miniflux format OPML import. (#768)
- Supported displaying Youtube and Bilibili video in multimedia gallery view.

### Changed

- Optimized RSSHub connection handling to improve performance and reliability.
- Changed evaluation methods for filter conditions and added logic precedence tips in filter and rule modals. (#756)

### Fixed

- Fixed an issue where the advanced settings for a RSSHub feed can not be saved correctly.
- Enhanced FetchAll to skip feeds with custom refresh intervals. (#774)
- Resolved multiple minor styling inconsistencies. (#751, #752, #753, #755)

## [1.3.21] - 2026-02-27

### Added

- Added support for additional translation providers. (#690)
- Enabled exporting to Zotero. (#735)
- Enhanced error messages for feed refresh failures in the settings page. (#518)
- Introduced an option to mark all articles as read from the bottom of the article list. (#667)

### Changed

- Refactored the dropdown input component to improve usability and added search functionality. (#697)

### Fixed

- Removed leaked thinking content from AI translation results. (@MidnightCrowing)
- Fixed a bug where rules might not apply correctly in certain scenarios. (#698)
- Resolved multiple minor styling inconsistencies. (#510, #648, #650, #697)

## [1.3.20] - 2026-02-13

### Changed

- Disabled closing the pop-up window by clicking on the background to prevent accidental closures.

### Fixed

- Fixed multiple minor styling inconsistencies. (#402, #407, #428, #646, #648, #649, #651, #665, #666, #668) (@RUBisco0211)
- Fixed an issue where the rule addition/editing modal could not be closed. (#647)
- Fixed an issue where some input fields would revert to their previous values after being cleared. (#689)
- Fixed an issue where the "Read Later" feature did not function correctly in the card layout. (#662)
- Fixed an issue where the image gallery could not adjust the number of columns based on the window width. (#652)

## [1.3.19] - 2026-02-07

**NOTE:** After the update, AI-related settings may require reconfiguration due to conflicts introduced by new features.

### Refactored

- Enhanced the tip box and image gallery components for improved consistency and maintainability.

### Added

- Introduced support for multiple AI profiles and configuration management. (#439)
- Implemented Notion integration for direct article export to Notion pages. (#625)
- Added the ability to hide and show the activity bar. (#588)
- Introduced a "show only unread" filter for the image gallery. (#559)
- Added additional filter conditions for article lists and automatic rules. (#642)

### Changed

- Enabled auto-refresh upon feed updates. (#639)
- Enforced caching for cover images in the image gallery. (#500)

### Fixed

- Ensured summary generation awaits full content when applicable. (#629)
- Prevented layout overflow caused by lengthy content. (#574)
- Resolved styling issues in the image gallery view. (#573)
- Fixed multiple minor styling inconsistencies. (#578, #585, #624, #645)

### Removed

- Removed path auto‑completion in the AI handler. (#640)

## [1.3.18] - 2026-01-29

### Refactored

- Refactored all popup windows and context menus for improved consistency and maintainability. (#582)

### Added

- Added AI-powered article search functionality. (#248)
- Added support for saving custom filters. (#223)
- Implemented feed tagging for better organization. (#545)
- Added batch operations to the feed list for efficient management. (#593)
- Added card layout view option for the article list. (#592)

### Changed

- Added confirmation dialog when bulk-marking articles as read to prevent accidental actions. (#560)
- Thumbnail previews now display in compact mode when enabled. (#589)
- Update checks detect firewall-related connectivity issues for users in mainland China. (#621)

### Fixed

- Fixed missing default title assignment when articles lack a title. (#566)
- Fixed multiple minor styling inconsistencies. (#569, #584, #579)
- Fixed styling issues in the image gallery view. (#571, #581)
- Fixed layout shift in list width after navigating to the settings page. (#575)
- Fixed HTTP headers being blocked by Cloudflare for some requests. (#620)
- Fixed broken images in article content caused by incorrect referrer headers. (#597)
- Fixed issue preventing the article summary from being closed. (#591)
- Fixed synchronization errors with FreshRSS. (#598, #600)
- Fixed HTML character encoding issues in the image gallery view. (#596)
- Improved filter-by-category performance by adding a missing database index. (#570)
- Implemented IMAP ID command support for enhanced client identification. (#602)

## [1.3.17] - 2026-01-24

### Refactored

- Refactored the settings page and i18n system for improved maintainability and extensibility.
- Upgraded the Wails version and corresponding Go dependencies.

### Added

- Added support for thumbnail images in the gallery view for easier navigation. (#495)
- Added the ability to filter images by category in the gallery view. (#487, #490)
- Added support for translation between Traditional and Simplified Chinese. (#511)
- Added the ability to copy images to the clipboard. (#515)
- Enhanced the styling and user experience of the gallery mode. (#520)
- Added support for customizing typography styles. (#488)
- Added the ability to mark items above or below as read. (#390, #524)
- Changed the default behavior to open links in an external browser. (#551)
- Added the ability to jump to a specific feed by clicking on it in the settings page. (#548)
- Added support for displaying multiple authors in a single feed. (#554)

### Changed

- Changed the checkbox checked indicator from an asterisk to a checkmark. (#507)
- Improved the feed list in the settings page for better usability. (#498)
- Made the protocol optional when adding or editing feeds. (#502)
- Improved the performance of article content search. (#509)
- Optimized the styling of compact mode. (#488, #504)

### Fixed

- Fixed multiple minor styling issues. (#492, #493, #494, #496, #503, #505, #506, #510, #516, #517, #519, #521, #522, #523, #550)
- Fixed an issue where plain text could not be translated correctly. (#511, #514)
- Fixed an issue where the reading status did not update correctly in gallery mode.
- Fixed an issue where translation occurred even when the feature was disabled. (#541)
- Fixed an issue where custom headers could accept non-ASCII characters. (#549)

## [1.3.16] - 2026-01-15

### Added

- Added compact mode for article list to reduce visual clutter. (#403)
- Enhanced image gallery with multi‑image support and improved navigation. (#457)
- Added support for Anthropic and DeepSeek AI services.
- Added option to hide text overlay in image gallery view. (#486)
- Added indication for feeds using image gallery mode in feed list. (#485)
- Added ability to customize translation service endpoint. (#383)
- Added option to disable automatic feed refresh. (#448)
- Added option to display translated text only (hide original). (#464)

### Changed

- Documents now open in the default browser with added multi‑language support. (#458)
- Import/export no longer shows error messages when no file is selected. (#483)
- Articles with >60% target language content are no longer translated to reduce API usage.

### Fixed

- Fixed conflict between left/right arrow shortcuts and input fields. (#454)
- Fixed article list not scrolling automatically when switching articles. (#451)
- Fixed minor styling issues. (#452, #456, #453, #484)
- Fixed display of future publish times for some articles.
- Fixed summary generation not respecting language settings. (#480)
- Fixed Gemini API integration. (#459)
- Fixed automatic application updates occurring without user confirmation. (#479)
- Fixed intermittent FreshRSS synchronization failures. (#460)
- Fixed view mode reset when switching between articles and images. (#432)
- Fixed XPath feed parsing in certain cases. (#479)

### Refactored

- Refactored sidebar, settings, and summary components for improved maintainability and performance. (#461, #466)

## [1.3.15] - 2026-01-11

### Changed

- Reduced the size of binary files by optimizing lingua-go import. (#450)

### Fixed

- Fixed an issue where the old database can not be migrated correctly in some cases.

## [1.3.14] - 2026-01-10

### Added

- Supported better reverse proxy for website display. (#414)
- Supported RSSHub feed type for better integration with RSSHub instances. (#176, #302) (@cry0404)
- Supported a statistics tab in the settings modal to view usage statistics over time.
- Supported manually sorting rules for advanced users. (#398)
- Supported Gemini service API. (#437)
- Supported language detection to reduce unnecessary translation requests. (#410)
- Added error messages for feeds that fail to refresh. (#429)
- Supported buttons to switch to previous/next articles in the article detail view. (#357)
- Supported -10s and +10s skip buttons in the audio player. (#395)

### Changed

- Cached thumbnail images in the article list to avoid disappearing after restarting the application. (#423)
- Improved the performance of article list rendering.
- Prevented the article content viewer from closing when clicking the same article again. (#434)

### Fixed

- Fixed an issue where URLs were not trimmed correctly when adding or editing feeds. (#413)
- Fixed an issue where the summary could not be regenerated after the article content changed. (#412)
- Fixed some minor style issues. (#396, #397, #402, #407, #425, #428, #430, #443, #449)
- Fixed an issue where the image gallery view showed only 2 columns. (#399)
- Fixed an issue where left and right click actions did not work correctly in the feeds list. (#394)
- Fixed an issue where feeds could not be dragged into collapsed categories. (#394)
- Fixed an issue where links could not be opened in the default browser after extracting the full article content. (#409)
- Fixed an issue where duplicate feeds could be added. (#401)
- Fixed an issue where the article list got stuck in some cases. (#422)
- Fixed an issue where the sidebar width would shrink when feed titles were short. (#433)
- Fixed an issue where the image viewer could not be closed automatically after switching articles or feeds. (#431)
- Fixed an issue where AI summaries were always regenerated in English or were not accurate enough. (#424, #438)
- Fixed an issue where FreshRSS synchronization failed for feeds in some cases. (#440)
- Fixed an issue where translation failures caused many toast notifications. (#436)
- Fixed an issue where FreshRSS articles could not display thumbnail images correctly. (#446)

**Special Thanks** to @EnterMan123 for carefully testing and reporting many of these issues!

## [1.3.13] - 2026-01-03

**BREAKING**: The FreshRSS synchronization feature has been significantly enhanced, offering more options and improved reliability. You may need to remove and re-add your FreshRSS feeds after upgrading.

### Added

- Enhanced FreshRSS synchronization with additional configuration options. (#333, #376)
- Added support for building and publishing multi-architecture Docker images to GHCR. (#349) (@czyt)
- Added support for email newsletter feeds via IMAP. (#313)
- Added more filter conditions, including title regex, FreshRSS feed status, image gallery status, and feed mode. (#372)
- Added display and sorting by last updated time, refresh status, and update frequency in the feed list. (#374)
- Added playback speed and volume controls for audio in article content rendering mode. (#354)

### Changed

- Increased default concurrency and timeout settings for feed fetching based on network speed detection. (#375)
- Disabled auto-close when clicking the background of popup windows to prevent accidental closures. (#355)
- Optimized the display of article publish times in the article list. (#373)
- Applied consistent scrollbar styling across all scrollable areas. (#389)

### Fixed

- Fixed "python command not found" error. (#364)
- Fixed broken links in article content rendering mode that failed to open in the default browser. (#330)
- Fixed incorrect rendering of some images in article content rendering mode. (#327)
- Fixed incorrect translation application in nested structures.
- Fixed search result highlight styling in article content. (#361)
- Fixed incorrect application name display (`{{.info.ProductName}}`) on Windows. (#351)
- Fixed repeated macOS privacy permission dialogs when opening articles. (#337)
- Fixed white screen flash when opening or closing windows. (#384)
- Fixed visual glitches caused by scrollbar thumb and article item borders. (#387, #388)
- Fixed inconsistent height in date input fields. (#391)
- Fixed image gallery not loading more images correctly on scroll. (#385)

### Removed

- Removed HTTPS requirement for feed URLs and API endpoints. (#251)
- Removed automatic article translation during feed refresh.
- Disabled hiding of image feeds in the "All Articles" view. (#386)

## [1.3.12] - 2025-12-29

**BREAKING**: The core system (including the feed fetcher, scheduler, and database cleaner) has been re-architected to improve performance and maintainability. (#350, #366)

The following changes may affect existing configurations:

- *Feeds that are not set to "Use global refresh settings" may no longer be refreshed when fetching all feeds.*
- *All article contents are now cached; enabling "Auto Cleanup" is recommended to prevent excessive database growth.*
- *The maximum refresh interval for intelligent scheduling has been increased from 3 hours to 24 hours.*
- *Feed refresh operations now time out after 5 seconds, followed by one retry with a 10-second timeout.*

### Added

- Added visual indicators for feeds that are currently refreshing or queued to refresh in the feed list.
- Added support for creating new chat sessions and viewing chat history in the AI Chat feature. (#340)
- Added support for rendering chat messages in Markdown format in the AI Chat feature. (#338, #346)
- Added the ability to search within article content. (#361)

### Changed

- Added a user setting to enable or disable automatic installation of updates after download. (#336)
- Keyboard shortcuts can now be enabled or disabled via settings.
- All article contents are now cached to improve loading speed when switching between articles. (#344)
- Improved error messages when adding or editing feeds in XPath mode for better user experience. (#345, #364)

### Fixed

- Fixed repeated macOS privacy permission dialogs when opening articles. (#337)
- Fixed high GPU usage when opening the settings page. (#339)
- Fixed feed refresh failures caused by certain invalid feeds. (#341)
- Fixed incorrect rendering of some images in article content. (#327)
- Fixed server startup failure due to the newly added custom CSS file upload feature. (#343)
- Fixed translation issues in confirmation pop-up windows.
- Fixed keyboard shortcut conflicts when the settings page is open. (#355)
- Fixed the "Mark all as read" button in the article list not working correctly. (#318, #353, #363)
- Fixed articles disappearing from the article list when opened while a filter is applied. (#318, #353, #362)
- Fixed incorrect application name display (`{{.info.ProductName}}`) on Windows. (#351)
- Fixed removal of advanced settings when moving a feed. (#356)
- Fixed incorrect summary display when switching articles before summary generation completes. (#365)
- Fixed error messages caused by NULL DATETIME values. (#347)
- Fixed inability to cancel text selection by clicking on blank areas in articles. (#360)

## [1.3.11] - 2025-12-26

### Fixed

- Fixed the issue of some incorrect styles in settings page.
- Fixed the issue where sidebar disappears.
- Fixed the issue drag-and-drop will not work correctly in some cases.

## [1.3.10] - 2025-12-26

### Added

- Supported import and export feeds in JSON format. (#317)
- Supported choosing auto expand content for each feed. (#306)
- Supported uploading CSS files for customized styling of articles. (#324)
- Supported showing only unread articles in article list. (#318)

### Changed

- Improved I18n translations, icons, and descriptions in settings page for better clarity and user experience.
- Improved UX of feed adding/editing modal. (#317)
- Expand status of categories in sidebar is now persisted across application restarts. (#315)

### Fixed

- Fixed the issue where length limit for AI-generated summaries was not applied correctly. (#323)
- Fixed the issue where the last time of network detect displays 739609 days ago if never detected before. (#314)
- Fixed the issue where multi-layer categories in sidebar do not display correctly. (#322)
- Fixed the issue of incorrect folder path in server mode. (#321)

## [1.3.9] - 2025-12-25

### Added

- Supported customized request headers for AI services. (#301)
- Supported enable automatically extracting full article content from original website. (#306)
- Supported choosing article view mode for each feed. (#309)

### Changed

- Reorganized settings page layout for better user experience.

### Fixed

- Fixed the issue where some articles failed to open when filter is applied. (#304)
- Fixed the issue where folders are not synchronized correctly and articles are duplicated when syncing with FreshRSS. (#305)

## [1.3.8] - 2025-12-24

### Added

- Supported AI setting tests in settings page to verify connectivity and credentials. (#297)
- Supported fetching feeds which require JavaScript rendering using headless browser. (#298)

### Changed

- AI generated summaries are now stored in the database to avoid redundant requests and improve performance. (#295)
- Reduce the frequency of automatic record of window status to improve performance.
- Improved the conversion from HTML to Markdown when exporting articles to Obsidian. (#299)

### Fixed

- Fixed the issue where docker image failed to access local files due to permission issues. (#296)
- Fixed the issue where articles failed to open in default browser. (#294)
- Fixed the issue where importing and exporting OPML files did not work correctly. (#271)
- Fixed the issue of CSP blocking some external resources.

## [1.3.7] - 2025-12-23

### Added

- Supported server mode for self-hosted web application deployment. (#267) (@caoli5288)
- Supported drag-and-drop to reorder feeds or change feed categories. (#288)
- Supported AI Chat on article content. And of course **it's disabled by default**! (#286)
- Supported exporting articles to Obsidian. (#289)
- Supported extracting full article content from original website when RSS feed only provides summary. (#266)

### Changed

- AI summarization is now triggered manually on default to avoid excessive API usage. Users can enable automatic summarization in settings if desired. (#287)
- Added Plugin setting tab in settings page and moved FreshRSS synchronization settings there.
- Improved icons and translations for better user experience.
- Enhanced the conversion from HTML to Markdown when exporting articles to Obsidian. (#299)

### Fixed

- Fixed the issue where concurrent feed refreshes exceed network capacity limit. (#262)

## [1.3.6] - 2025-12-22

### Added

- Supported importing feeds with HTML+XPath / XML+XPath type from OPML files. (#264)
- Supported FreshRSS synchronization. (#245)

### Changed

- Improved error display for customized scripts when adding/editing feeds. (#264)
- Network connection test now supports proxy settings. (#256)

### Fixed

- Fixed the issue where different articles display the same content due to incorrect URL matching. (#257)
- Fixed the issue where import and export of OPML files did not work correctly on macOS. (#263)
- Fixed the issue where localhost cannot be processed correctly. (#257)

### Removed

- Removed single instance lock on Linux platform to avoid D-Bus related issues. (#246)

## [1.3.5] - 2025-12-20

**BREAKING**: AI-based summarization and translation now need a full path instead of just endpoint URL.

> e.g. for OpenAI services, use `https://api.openai.com/v1/chat/completions`. for Ollama, use `http://localhost:11434/api/generate`.

### Added

- Supported ollama and other local LLMs for AI-based translation and summarization. (#251)
- Supported limits and quotas for AI services to control usage and costs. (#252)
- Supported hover to mark articles as read in article list. (#250)
- Supported deelpx translation service. (#247)

### Changed

- Improved AI settings UI/UX for better user experience.
- Refactored docs and workflows to improve maintainability and clarity.
- AI translation and summarization are now cached to reduce redundant requests and improve performance.
- Recent articles are now cached to improve loading speed.
- When AI functionality gets errors, fallback to local summarization/translation automatically.

### Fixed

- Fixed the issue where some opml files cannot be imported and outported correctly. (#249)
- Fixed the issue where proxy settings were not applied correctly for feed fetching. (#256)
- Fixed the issue where software print too much debug logs in production builds.
- Fixed the issue where network connection test fails when some test endpoints are unreachable. (#256)
- Fixed the issue where summarization failures will affect article content rendering. (#242)
- Fixed the issue where article content fetching blocked by feed refreshes.
- Fixed the issue of dark mode styles on Linux platform.

## [1.3.4] - 2025-12-18

### Fixed

- Fixed the issue where window title bar buttons on MacOS overlapping with content area.
- Fixed the issue where window cannot be dragged on MacOS. (#242)

## [1.3.3] - 2025-12-18

### Added

- Supported copying article link and title to clipboard from article actions menu. (#155)

### Changed

- Replace the following functionality with a native implementation using wails3 (#242)
  - Open link in default browser
  - Window events handling (minimize, maximize, close) and management
  - Native window context menu and title bar on MacOS

### Fixed

- Fixed the issue where super loooooooong article titles causing layout breaking in article list.
- Fixed the issue where cutting long article titles in chinese does not work correctly.

## [1.3.2] - 2025-12-17

### Fixed

- Fixed the issue where MacOS window cannot be closed correctly after maximizing. (#221)
- Fixed the issue where images in article content rendering mode cannot be displayed correctly. (#222)
- Fixed the issue where windows app cannot be packaged correctly due to wrong version number format.

## [1.3.0] - 2025-12-17

### Changed

- **BREAKING**: Upgraded from Wails v2 to Wails v3 (alpha) framework (#234)
  - Migrated to new API
  - Replaced external systray library with Wails v3 built-in system tray
  - Updated single instance handling to use v3 API
  - Updated event handling to use v3 hooks
  - Updated build system to use Taskfile and Wails v3 CLI
  - Updated dependencies to work with WebKit2GTK 4.1 and libsoup 3.0
- Changed GitHub Actions workflows compatibility with Wails v3

## [1.2.20] - 2025-12-16

### Changed

- Added more tests for backend and frontend components to improve code reliability.
- Updated dependencies to latest versions for security and performance improvements.

### Fixed

- Fixed issues related to MacOS platform (#212)
  - Updated icons for better appearance.
  - Added more white space on top of the main window for better visual balance.
  - Disabled icon name on tray.
  - Fixed the issue where window cannot be dragged.
  - Fixed the issue where application not closing correctly after maximizing.

## [1.2.19] - 2025-12-15

### Fixed

- Fixed the issue where some settings were not saved and applied correctly. (#201)
- Fixed the issue where macOS application failing to launch after installation.

## [1.2.18] - 2025-12-14

### Added

- Supported image gallery for browsing all images in articles. (#190)
- Supported network latency and bandwidth testing in settings. (#194)

### Changed

- Added number of concurrent feed refreshes according to network situation.

### Fixed

- Fixed the issue where software can open multiple instances. (#198)
- Fixed the issue where number of feeds left to refresh is not accurately displayed during feed refresh. (#194)

## [1.2.17] - 2025-12-13

### Added

- Supported upgrade in portable mode. (#191)

### Fixed

- Fixed the issue where settings cannot be saved and applied by downgrade TailwindCSS version.

## [1.2.16] - 2025-12-13

### Added

- Add toggle button to hide/show article content translations. (#186)

### Changed

- Updated all dependencies to latest versions for security and performance improvements.

### Fixed

- Fixed the issue where MacOS cannot complile correctly for system tray support. (#181)
- Fixed the issue where Linux-ARM64 AppImage cannot run correctly.

## [1.2.15] - 2025-12-13

### Changed

- Supported alpha, beta, and pre-release version tags. (#182)
- Enhanced credential encryption mechanism to improve security during database migration and storage. (#160)

## [1.2.14] - 2025-12-12

### Added

- Supported portable mode for running MrRSS from USB drives with all data stored in a single folder. (#167)
- Supported minimizing to system tray on close action.
- Supported hiding preview images in article list for a more compact view. (#157)

### Fixed

- Fixed the issue where some images wrapped in links cannot be operated correctly.
- Fixed the issue where single-line link cannot be translated correctly.
- Fixed the issue where some links cannot be opened in the default browser.
- Fixed the issue where icons on MacOS were not displayed correctly. (#173)
- Fixed the issue where the window size and position were not restored correctly. (#173)

## [1.2.13] - 2025-12-11

### Added

- Supported media cache system to bypass anti-hotlinking restrictions and cache images/videos locally. (#152)
- Supported proxy settings for network requests.
- Supported intelligent refresh scheduling based on feed update frequency. (#151)
- Supported customizing proxy and refresh settings per feed. (#151)
- Supported read all articles for a specific feed or category. (#156)

### Changed

- Google Translate endpoint is now customizable in settings. (#158)

### Fixed

- Fixed the issue where title and summary cannot be selected and copied in article content rendering mode. (#155)
- Fixed the issue where some articles are rendered with incorrect formatting in article content rendering mode.

## [1.2.12] - 2025-12-10

### Changed

- Settings now support validation and show error messages for invalid inputs. (#147)

### Fixed

- Links in article content rendering mode can now be translated correctly. (#148)
- Fixed the issue where some images were not displayed in article content rendering mode. (#148)

## [1.2.11] - 2025-12-08

### Added

- Supported selecting existing categories when adding or editing a new feed.
- When playing audio or video in article content rendering mode, playback controls are now available.
- Supported customizing the AI prompt for article summarization and translation.

### Changed

- Improved styles for article content rendering mode.

### Fixed

- Fixed the issue where some feeds cannot be handled due to invalid styles in RSS XML.
- Fixed the issue where inline elements (e.g. code, formulas) were not handled correctly in translation.
- Fixed the issue where toast notifications not supporting dark mode caused visibility problems.
- Fixed the issue related to importing OPML files.

## [1.2.10] - 2025-12-07

### Added

- Supported audio and video embedding in article content rendering mode.

### Changed

- Enhanced styling of article content for better readability.

## [1.2.9] - 2025-12-05

### Added

- Supported Baidu Translation and AI-based translation.
- Supported AI-based article summarization using OpenAI-compatible APIs.

### Changed

- Errors from translation services are now logged and displayed to users for better troubleshooting.

### Fixed

- Fixed the issue where the default settings were not applied correctly on first launch.
- Fixed the issue where `PubMed` feed parsing failed.

## [1.2.8] - 2025-12-04

### Added

- Implemented Read Later functionality, articles marked for read later are automatically set to unread.

### Changed

- Last update time now displayed as inline sub-text instead of separate row.
- Added toggle filter shortcut (default: `f`).
- Nav icons use fill style when active for stronger visual feedback.
- Category headers are now sticky for scroll context retention.
- Feed refresh now skipped on startup if last article update interval is within set threshold.
- After each article refresh completes, the app now checks for updates. If a new version is detected, it automatically downloads and installs in the background.
- Changed some default settings.

### Fixed

- Fixed styling issues, including incorrect icon colors in dark mode, inconsistent font sizes, and misaligned elements.

## [1.2.7] - 2025-12-03

### Added

- Supported hiding feeds from timeline.

### Fixed

- Fixed initialization problem by adding progress tracking for single feed and OPML import.

## [1.2.6] - 2025-12-02

### Added

- Added TF-IDF and TextRank algorithms for generating article summaries.
- Added auto-translation support for summary, title, and content in rendering.
- Enhanced multimedia support in content rendering mode.

### Changed

- Improved image viewer with drag support and better zooming.
- Refactored both frontend and backend code for better maintainability.

### Fixed

- Fixed the issue where searching box scrolls with the feed list.

## [1.2.5] - 2025-11-27

### Added

- Supported for user-defined scripts to fetch and parse non-standard RSS feeds.
- Improved shortcuts for popup window actions.
- Supported sorting articles list in settings by various criteria.
- Supported for refreshing individual feeds via right-click context menu.
- Supported for searching feeds in the feed list.

### Changed

- Article list will not refresh during feed refresh, fixing a bug causing the article list to occasionally crash.
- Generate article titles from content when RSS feed items are missing titles.

### Fixed

- Fixed issue where some UI elements did not scale properly.
- Fixed bug causing view mode performe incorrectly when switching articles rapidly.

### Removed

- Removed search box for article list because the filter function covers the same use case.

## [1.2.4] - 2025-11-27

### Changed

- Refactored frontend codebase and landing page for better maintainability and user experience.
- Added tests for critical components to improve code reliability.
- Updated dependencies to latest versions for security and performance improvements.
- Better documentation for developers and contributors.
- Improved CI/CD pipeline for faster and more reliable builds and deployments.

## [1.2.2] - 2025-11-26

### Added

- Added keyboard shortcuts for common actions and corresponding settings in the Settings tab.
- Supported customizing rules with "If [condition], then [action]" format for advanced users.

### Changed

- Improved landing page UI/UX for better user engagement.
- Improved documentation for new users.

## [1.2.1] - 2025-11-26

### Added

- Adds advanced article filtering via a modal accessible from a filter button next to the search box.

## [1.2.0] - 2025-11-25

### Added

- Implements automated feed discovery from friend links with intelligent batch scanning, comprehensive deduplication, real-time progress tracking

### Changed

- Major restructuring of backend code for improved maintainability

## [1.1.8] - 2025-11-24

### Added

- Feed icons now display in the Settings > Feeds tab feed list for better visual identification
- Website homepage link stored for each feed (from RSS feed metadata)

### Changed

- "Open Website" context menu action now opens the feed's website homepage (if available) instead of RSS feed URL, with fallback to RSS URL
- All hardcoded strings now properly use i18n translations for better internationalization support

### Fixed

- Replaced native `prompt()` with custom `showInput()` dialog for consistent UI

## [1.1.7] - 2025-11-24

### Added

- Unread count badge displayed on each feed in the sidebar and "All Articles" button
- "Mark All as Read" button next to the refresh button in article list and feed context menu
- When feeds fail to load, display error message in feed list instead of silently failing
- Implemented input dialog for moving feeds to a new category

### Changed

- Fixed unfavorite icon for better visibility

## [1.1.6] - 2025-11-23

### Added

- "Open Website" option in feed right-click menu
- Startup on boot setting in General settings tab (default off)

### Changed

- "Hide Article" context menu item now shows as a danger button
- Improved settings tab switching style with hover effects and animations
- Fixed unfavorite icon visibility in article detail view

### Fixed

- Fixed software update installation process - updates can now be properly installed

## [1.1.5] - 2025-11-23

### Added

- Switch between viewing the original webpage and RSS content within the app
- Article hiding functionality
- Last article update time display in settings

### Changed

- Improved UI text and image selection prevention

### Fixed

- Fixed issue where translation settings changes didn't clear existing translations

## [1.1.4] - 2025-11-23

### Added

- Auto cleanup sub-settings:
  - Max cache size setting (default 20MB) - controls maximum database size before cleanup
  - Max article age setting (default 30 days) - automatically delete articles older than specified days (except favorites)
- Download progress bar during update download
- App automatically closes after starting installer to prevent conflicts during update
- Automatic cleanup of installation packages after update installation

### Changed

- Settings now auto-save immediately when changed (no need to click save button)

### Removed

- "Save Settings" button at bottom of settings page (replaced with auto-save)

## [1.1.3] - 2025-11-22

### Added

- Automatically detects user's operating system and CPU architecture and downloads appropriate installer from GitHub releases. Then launches installer and prepares for update
- Multi-Platform Support:
  - Windows: x64 (amd64), ARM64
  - Linux: x64 (amd64), ARM64 (aarch64)
  - macOS: Universal (Intel & Apple Silicon)
- Visual feedback during update download and installation

## [1.1.2] - 2025-11-22

### Added

- Initial release preparation
- OPML import/export functionality
- Feed category organization
- Automatically detect and apply system theme preference
- Better defaults for translation settings
- Version check functionality in Settings → About tab

### Changed

- Simplified update check UI
- Improved theme switching mechanism
- Better handling of translation provider selection

### Fixed

- Various bug fixes and stability improvements
- UI refinements for better user experience
- Theme switching issues between light and dark modes
- Translation default language selection
- Update notification display

## [1.1.0] - 2025-11-20

### Added

- **Initial Public Release** of MrRSS
- **Cross-Platform Support**: Native desktop app for Windows, macOS, and Linux
- **RSS Feed Management**: Add, edit, and delete RSS feeds
- **Article Reading**: Clean, distraction-free reading interface
- **Smart Organization**: Organize feeds into categories
- **Favorites & Reading Tracking**: Save articles and track read/unread status
- **Modern UI**: Clean, responsive interface with dark mode support
- **Auto-Translation**: Translate article titles using translation services or AI-based translation
- **OPML Support**: Import and export feed subscriptions
- **Auto-Update**: Configurable interval for fetching new articles
- **Database Cleanup**: Automatic removal of old articles
- **Multi-Language Support**: English and Chinese interface
- **Theme Support**: Light, dark, and auto (system) themes

---

## Release Notes

### Version Numbering

MrRSS follows [Semantic Versioning](https://semver.org/):

- **MAJOR** version for incompatible API changes
- **MINOR** version for backwards-compatible functionality additions
- **PATCH** version for backwards-compatible bug fixes

### Download

Downloads for all platforms are available on the [GitHub Releases](https://github.com/marcomarcogd/MRSS/releases) page.

### Upgrade Notes

When upgrading from a previous version:

1. Your data (feeds, articles, settings) is preserved in platform-specific directories
2. Database migrations are applied automatically on first launch
3. For major version upgrades, please review the changelog for breaking changes

### Support

- Report bugs: [GitHub Issues](https://github.com/marcomarcogd/MRSS/issues)
- Feature requests: [GitHub Issues](https://github.com/marcomarcogd/MRSS/issues)
- Documentation: [README](README.md)
