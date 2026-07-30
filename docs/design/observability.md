# 可观测性边界

状态：已确认

## 目标

让 PostgreSQL 事务、Outbox、Inbox 和事件发布在 TMS、Parcel 的私有化环境中可诊断，同时避免 `idp-bento-go` 自建与 OpenTelemetry 重复的抽象、绑定具体 exporter 或泄露消费者业务数据。

## 决策状态

| 决定 | 状态 |
| --- | --- |
| `v0.1` 提供框架适配器可观测性 | `CONFIRMED` |
| 不定义自有 `Tracer`、`Meter`、`Logger` Port | `CONFIRMED` |
| Trace/Metric 使用 OpenTelemetry API，日志使用 Go `log/slog` | `CONFIRMED` |
| SDK、Exporter、采样、Collector 和凭据由消费者拥有 | `CONFIRMED` |
| OpenTelemetry API 精确版本 | `BOUND`：`v1.44.0`；SDK、Exporter 与桥接实现仍由消费者选择 |

## 架构边界

- `domain`、`repository`、`eventing` 和 `application` 不导入 OpenTelemetry SDK、Exporter 或 Collector 客户端，核心继续保持标准库优先。
- 不创建自有 `Tracer`、`Span`、`Meter`、`Counter`、`Logger` 或 `Observer` Port。OpenTelemetry API 和 `log/slog` 已经提供标准契约，再包装只会形成不完整且泄漏实现的抽象。
- 实验性 `postgres`、`postgres/outbox` 和 `postgres/inbox` 适配器可以通过显式构造选项接收 OpenTelemetry `TracerProvider`、`MeterProvider` 和 `*slog.Logger`。
- 未提供 provider 或 logger 时使用可测试的 no-op 行为，不依赖全局初始化，不改变事务和消息处理结果。
- 框架可以依赖 OpenTelemetry API/语义约定包，但不依赖具体 SDK、Exporter、Collector 地址、认证方式或客户配置。
- TMS、Parcel 分别拥有服务名、资源属性、采样、批处理、导出协议、TLS、凭据、保留期和告警规则。
- `idp-obs/sdk-go` 只有在提供标准 OpenTelemetry provider/exporter 且具备独立版本与私有化依赖保证时，才可以由消费者装配；`idp-bento-go` 不直接依赖它。

## 传播规则

- 进程内传播使用 `context.Context`，不得从 context 推断租户或授权范围。
- 跨进程事件只传播 W3C Trace Context 所需的 `traceparent` 和 `tracestate` 技术字段；`eventing` 以字符串字段表达，不导入 OpenTelemetry 类型。
- 默认不传播 OpenTelemetry Baggage，避免把主体、租户、客户或业务标签无边界扩散到事件与下游系统。
- 缺失或无效 Trace Context 不影响事件处理，消费者创建新的 trace；不得因观测上下文问题拒绝业务事实。

## `v0.1` 最小遥测

### PostgreSQL 与事务

- 事务开始、提交、回滚和冲突的计数与耗时。
- 连接获取和执行错误分类，不记录完整 SQL 文本或参数。
- Repository 操作只记录稳定技术操作名和结果分类，不记录聚合、租户或主体标识。

### Outbox

- 待发布数量、最老记录延迟、领取数量和领取耗时。
- 发布成功、失败、重试、放弃/死信数量与发布耗时。
- 分区队首事件的等待时长，用于在达到最大阻塞时长之前发现分区被单个事件阻塞。
- 领取次数与通过 `RecordFailure` 显式记录的可重试发布调用失败次数分别统计，使租约过期、明确失败和结果不确定在诊断时可区分。
- 事件类型只能在受控低基数集合中作为属性；事件 ID、聚合 ID、租户 ID 和载荷不得成为 metric label。分区键因基数不可控，不得作为 metric label，只能进入受控 trace 或日志。

### Inbox

- 接收、首次处理、重复抑制、处理成功和处理失败数量。
- 处理延迟和幂等占用冲突；`ErrInboxBusy` 有界等待超时单独计数，用于识别重投风暴。
- 不记录事件载荷、主体信息或消费者业务结果。

### 日志

- 使用 `log/slog` 结构化日志，不建立框架私有日志接口。
- 默认字段只包含组件、操作、结果分类、重试次数和安全的错误摘要。
- 日志不能包含令牌、凭据、完整 SQL、SQL 参数、事件载荷、租户 ID、主体 ID、客户标识或授权证据正文。

## 可靠性规则

- 遥测导出失败、采样失败或 logger 不可用不得使业务事务回滚，也不得把已经成功的事件标记为失败。
- 遥测代码不得在业务事务中执行同步网络导出。
- 指标标签必须是有界低基数；动态标识只能进入受控 trace/log 且默认关闭或脱敏。
- 原始 FailureCode 即使有字符与长度限制也可能形成高基数，不得直接作为 metric label；只能映射到框架固定枚举或显式 allowlist 的 failure class。
- 遥测初始化错误在启动装配阶段报告；运行时采用 no-op 或降级，不形成无限重试和资源泄漏。
- 框架定义稳定的 instrumentation scope 名称和指标语义；重命名或改变单位按公共兼容性变更治理。

## 消费者合同

- 不配置 OpenTelemetry 时，全部适配器行为和测试仍然成立。
- 注入 provider 后能够形成事务、Outbox、Inbox 和 Publisher 的预期 span/metric。
- Trace Context 能通过 Outbox 信封和 Inbox 提取消费连续传播。
- 遥测导出失败不会改变提交、发布、重试和幂等语义。
- 自动检查禁止的高基数或敏感属性不会进入默认 span、metric 和日志。

## 非目标

- 不提供 Collector、可观测性后端、Dashboard、告警平台或客户环境部署。
- 不规定 TMS、Parcel 的业务 KPI、领域事件指标或授权审计内容。
- 不提供全局 logger/provider 单例或隐藏的自动初始化。
- 不在 `v0.1` 新增独立 `observability`、`telemetry` 或 `otelkit` 公共包；可观测能力位于拥有操作的实验性适配器中。

## 下一步

PostgreSQL 适配器已经通过显式 provider/logger 选项实现事务、Outbox 和 Inbox 遥测，instrumentation scope 与 operation counter 已进入实验性包 API 基线。下一步在受保护 CI 和消费者候选中验证实际 SDK/exporter 装配；框架仍不拥有 SDK、端点或凭据。
