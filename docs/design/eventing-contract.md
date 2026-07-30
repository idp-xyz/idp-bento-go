# `eventing`、Outbox 与 Inbox 公共合同

状态：已确认

## 目标

为 TMS 与 Parcel 提供与消息产品无关、可持久化、可重复投递且可验证的集成事件合同。框架只拥有技术信封、发布端口、Outbox/Inbox 幂等协议和失败分类，不拥有消费者的业务事件、订阅关系、重试业务规则或消息中间件。

## 已确认决定

- 领域事件继续留在 `domain.Event`；只有消费者应用层明确映射后的事实才能成为 `eventing.Envelope`。
- `eventing` 使用已序列化 JSON 载荷的非泛型信封，避免异构批次、Outbox 持久化和 Publisher 被 Go 具体载荷类型耦合。
- 信封显式包含来源、事件类型、结构版本、作用域、主题、分区键、关联/因果和 W3C Trace Context；不提供任意 Headers/Metadata map。
- Publisher 单条发布，不定义具有部分成功歧义的通用批量发布接口。
- `Enqueue` 与 `Publish` 在边界内强制校验信封，无效信封不能进入持久化或传输；信封校验规则本身属于兼容性契约，收紧规则等同破坏性变更。
- 交付保证为 At-Least-Once，不声称 Exactly-Once。发布结果不确定时允许重复投递，消费端必须使用 Inbox 或等价事务性幂等机制。
- Outbox 按分区键保持入队顺序；不同分区不承诺全局顺序。前序事件重试期间阻塞同一分区后续事件，前序进入 `ABANDONED` 后允许后续继续。
- Outbox 使用 `Attempt` 与 `Failures` 两个计数器：只有通过 `RecordFailure` 显式记录的可重试发布调用失败（包括 `ErrPublishUncertain`）消耗失败预算并驱动 `ABANDONED`，租约过期不被误判为发布失败。
- Inbox 幂等键固定为“消费者标识 + 来源 + 事件 ID”，不包含租户/作用域，避免重复事件通过改变作用域绕过抑制。
- Inbox 占用使用有界等待获取语义；已被未提交事务占用时在短超时内返回包装了 `ErrInboxBusy` 的错误，强制当前事务回滚且不 ACK，再由消息适配器重新投递；不做无界锁等待。
- Inbox 业务副作用、幂等占用与成功状态必须位于同一数据库事务；外部不可事务化副作用应再次写入本地 Outbox，不能在 Inbox 事务内直接声称原子完成。
- `v0.1` 不提供消息路由、订阅注册表、Schema Registry 客户端、Broker 适配器、自动反序列化到业务结构、通用死信重放或 Saga。

## 首版公共 API

```go
package eventing

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

const (
	SpecVersionV1   = "1.0"
	SpecVersion     = SpecVersionV1
	JSONContentType = "application/json"

	MaxEventIDLength       = 128
	MaxSourceLength        = 255
	MaxEventTypeLength     = 255
	MaxScopeLength         = 255
	MaxSubjectLength       = 512
	MaxPartitionKeyLength  = 512
	MaxCorrelationIDLength = 128
	MaxCausationIDLength   = 128
	MaxTraceParentLength   = 512
	MaxTraceStateLength    = 512
	MaxFailureCodeLength   = 128
	MaxInboxConsumerLength = 255
	MaxPayloadBytes        = 1 << 20
	MaxOutboxClaimLimit    = 1000
)

type EventID string
type EventType string
type EventVersion uint32
type FailureCode string

const (
	FailureEnvelopeInvalidOnPublish FailureCode = "envelope.invalid_on_publish"
	FailureClaimLimitExceeded       FailureCode = "outbox.claim_limit_exceeded"
)

type Envelope struct {
	SpecVersion   string
	ID            EventID
	Source        string
	Type          EventType
	Version       EventVersion
	Scope         string
	Subject       string
	PartitionKey  string
	OccurredAt    time.Time
	RecordedAt    time.Time
	CorrelationID string
	CausationID   string
	TraceParent   string
	TraceState    string
	ContentType   string
	Payload       json.RawMessage
}

func (e Envelope) Validate() error
func (e Envelope) Clone() Envelope
func (e Envelope) MarshalJSON() ([]byte, error)
func (e *Envelope) UnmarshalJSON(data []byte) error

type Publisher interface {
	Publish(ctx context.Context, envelope Envelope) error
}

type PublisherFunc func(ctx context.Context, envelope Envelope) error

func (f PublisherFunc) Publish(
	ctx context.Context,
	envelope Envelope,
) error

type OutboxWriter interface {
	Enqueue(ctx context.Context, envelope Envelope) error
}

type OutboxClaim struct {
	Now         time.Time
	Limit       int
	LeaseFor    time.Duration
	MaxAttempts uint32
}

type DeliveryRef struct {
	Source     string
	EventID    EventID
	LeaseToken string
}

type Delivery struct {
	Ref      DeliveryRef
	Envelope Envelope
	Attempt  uint32
	Failures uint32
}

type DeliveryFailure struct {
	FailedAt    time.Time
	RetryAt     time.Time
	Code        FailureCode
	MaxFailures uint32
}

type OutboxClaimer interface {
	Claim(ctx context.Context, claim OutboxClaim) ([]Delivery, error)
}

type OutboxFinalizer interface {
	MarkPublished(
		ctx context.Context,
		ref DeliveryRef,
		publishedAt time.Time,
	) error
	RecordFailure(
		ctx context.Context,
		ref DeliveryRef,
		failure DeliveryFailure,
	) error
	Abandon(
		ctx context.Context,
		ref DeliveryRef,
		abandonedAt time.Time,
		failure FailureCode,
	) error
}

type InboxConsumer string

type InboxKey struct {
	Consumer InboxConsumer
	Source   string
	EventID  EventID
}

type InboxStatus uint8

const (
	InboxProcessing InboxStatus = iota + 1
	InboxProcessed
	InboxRejected
)

type InboxReceipt struct {
	Key InboxKey
}

type InboxStartResult struct {
	Receipt  InboxReceipt
	Status   InboxStatus
	Acquired bool
}

type InboxStarter interface {
	Start(
		ctx context.Context,
		key InboxKey,
		receivedAt time.Time,
	) (InboxStartResult, error)
}

type InboxCompleter interface {
	MarkProcessed(
		ctx context.Context,
		receipt InboxReceipt,
		processedAt time.Time,
	) error
	MarkRejected(
		ctx context.Context,
		receipt InboxReceipt,
		rejectedAt time.Time,
		failure FailureCode,
	) error
}

var (
	ErrInvalidEnvelope        = errors.New("eventing: invalid envelope")
	ErrUnsupportedSpecVersion = errors.New("eventing: unsupported spec version")
	ErrInvalidRequest         = errors.New("eventing: invalid request")
	ErrAlreadyEnqueued        = errors.New("eventing: event already enqueued")
	ErrPublishRejected        = errors.New("eventing: publish rejected")
	ErrPublishUncertain       = errors.New("eventing: publish outcome uncertain")
	ErrInboxBusy              = errors.New("eventing: inbox entry is being processed")
	ErrLeaseLost              = errors.New("eventing: delivery lease lost")
	ErrInvalidTransition      = errors.New("eventing: invalid state transition")
)
```

具体实现可以拆分到多个 Go 文件。`OutboxWriter`、`OutboxClaimer`、`OutboxFinalizer`、`InboxStarter` 和 `InboxCompleter` 保持小接口，应用、发布进程和消费进程只依赖自己需要的能力。

## 信封字段合同

| 字段 | 要求 | 所有权与用途 |
| --- | --- | --- |
| `SpecVersion` | 必须为受支持版本；`v0.1` 仅为 `SpecVersionV1` | 标识技术信封格式，不是业务事件版本；`SpecVersion` 常量表示新建信封的当前版本 |
| `ID` | 非空、来源内稳定且不重复 | 由消费者在写 Outbox 前生成；重试不得更换 |
| `Source` | 非空且稳定 | 标识生产产品/模块，不使用部署实例名或临时地址 |
| `Type` | 非空、稳定 | 消费者拥有的业务事件类型，不从 Go 包名或结构体名自动推导 |
| `Version` | 从 `1` 开始 | 标识 `Type` 的载荷结构版本，不与 Repository Revision 混用 |
| `Scope` | 非空、不透明 | 承载消费者权威隔离范围；TMS 使用权威租户标识，框架不解析或从 context 推断 |
| `Subject` | 非空、不透明 | 标识事件所述资源或聚合引用，框架不把它解释为业务主键 |
| `PartitionKey` | 非空、稳定 | 决定同一逻辑流的发布顺序；通常由作用域和资源身份显式构造 |
| `OccurredAt` | 非零 | 业务事实发生时间 |
| `RecordedAt` | 非零 | 集成事件信封形成时间；两种时间持久化和序列化时统一为 UTC |
| `CorrelationID` | 可空 | 串联同一业务交互；根交互可以使用自身命令或事件标识 |
| `CausationID` | 可空 | 标识直接触发原因；不得伪造为全局事务 ID |
| `TraceParent` / `TraceState` | 可空、只作技术传播 | 无效值由消费者忽略并创建新 trace，不得拒绝业务事实 |
| `ContentType` | 必须为 `JSONContentType` | `v0.1` 只稳定 JSON 载荷，不提供 Codec Registry |
| `Payload` | 非空、合法 JSON 对象 | 只包含该事件对消费者必要的最小事实，不嵌入任意 Go 类型 |

`Validate` 的所有结构错误必须包装 `ErrInvalidEnvelope`；不支持的 `SpecVersion` 同时包装 `ErrUnsupportedSpecVersion`。校验先按信封自身的 SpecVersion 分派到对应规则；`v0.1` 只实现 `SpecVersionV1`。字符串必须是有效 UTF-8，去除首尾空白后仍非空，不允许 Unicode 控制字符，并严格遵守上述 `Max*Length` 常量。全部长度上限按 UTF-8 字节数解释，Go 校验使用 `utf8.ValidString` 并等价于 `len([]byte(value))` 计数，PostgreSQL DDL 使用 `octet_length`；不能分别按 rune 数和数据库字符数实现。`Payload` 必须是最大 1 MiB 的合法顶层 JSON 对象；数组、标量、空值和超过 `MaxPayloadBytes` 的载荷无效。超过上限的内容应改为受控对象存储引用，不能扩大单个事件承载文件或批量数据。Trace Context 只做长度和控制字符检查，不因 W3C 语义解析失败使信封无效，但仍必须是有效 UTF-8。

`Type + Version` 唯一标识载荷契约。同一版本允许真正向后兼容的可选字段增加，不允许删除、重命名、改变既有字段含义或缩窄取值范围；不兼容变化必须增加版本。事件类型不得被重新用于另一种业务事实。

`MarshalJSON` 使用以下固定字段名：`specversion`、`id`、`source`、`type`、`version`、`scope`、`subject`、`partition_key`、`occurred_at`、`recorded_at`、`correlation_id`、`causation_id`、`traceparent`、`tracestate`、`content_type`、`payload`。四个可空的关联/因果/Trace 字段为空时省略，其他字段必须出现。时间使用 RFC 3339 Nano UTC，`payload` 作为 JSON 对象嵌入，不得编码为 Base64。`UnmarshalJSON` 接受未知附加字段以允许信封向前兼容，但必须验证信封声明的受支持 `SpecVersion` 和该版本的全部必填字段。编码不承诺 JSON 对象键顺序或字节级规范化，签名、去重和幂等不得依赖整段 JSON 的原始字节相等。

RFC 3339 Nano 是传输格式，不表示所有适配器都能纳秒级往返。PostgreSQL `timestamptz` 只保存微秒精度；PostgreSQL Store 写入前统一转为 UTC，扫描和集成测试按 UTC/微秒精度比较，不得把被数据库截断的亚微秒差异判定为合同破坏。

`Clone` 必须深拷贝 `Payload`。调用方在 `Publish` 或 `Enqueue` 开始后不得继续修改传入信封；实现如果在调用返回后保留数据，必须保存自己的副本。

框架不提供开放式 Metadata map。新增跨消费者技术字段必须经过公共 API 与信封兼容性治理；消费者专属信息进入其版本化 Payload，不能悄悄形成未治理协议。

## 校验强制执行点

信封有效性是框架保证，不是消费者纪律。`OutboxWriter.Enqueue` 与 `Publisher.Publish` 在执行任何持久化或传输前必须调用 `Validate`，失败时返回包装 `ErrInvalidEnvelope` 的错误并且不产生任何副作用。消费者仍可在映射后主动调用 `Validate` 获得更早的反馈，但不得依赖这一步来保证边界安全。

强制点设在两处而不是只设在写入侧，因为绕过 Outbox 的同步发布路径和测试夹具同样需要保护，且接口语义不对称会形成长期记忆负担。重复校验的成本是字符串长度检查与一次 JSON 顶层类型判断，相对于数据库往返或网络发布可以忽略。

无效信封一旦落入 Outbox 表会停留在分区队首并阻塞该分区的后续事件，而 `v0.1` 不提供原地重放或修改历史信封的公共 API，修复只能走消费者的运维流程。这是强制校验必须位于持久化边界之内的根本原因。

`Validate` 的规则集是信封兼容性契约的组成部分。放宽规则属于兼容变更；收紧规则（缩小 `Max*Length`、增加字符限制、加严 `Payload` 结构要求）属于破坏性变更，只能通过新的 `SpecVersion` 引入，不得在同一 `SpecVersion` 内的补丁或 minor 中生效。否则升级后按旧规则合法的在途 Outbox 记录会突然校验失败，表现为一批事件在升级后静默消失。

新增 SpecVersion 时必须新增独立常量与独立行为清单，并把 `SpecVersion` 当前版本别名指向新常量；不得改写 `SpecVersionV1`。新版本实现继续接受仍处于支持期的历史版本，Enqueue、Publish 和 Unmarshal 都按信封自身版本校验，使升级前已持久化的 Outbox 记录可以继续发布。历史版本只有在公布 EOL、两个消费者完成迁移并确认没有在途或持久化记录后，才能按允许破坏的版本级别停止支持；历史行为清单永久保留。

发布路径对已持久化信封校验失败时，必须按 `ErrPublishRejected` 处理并直接进入 `ABANDONED`，使用 `FailureEnvelopeInvalidOnPublish` 作为 `FailureCode`。这类记录不消耗重试预算，也不与普通发布失败混在一起，使运维能够立即定位受影响范围。

## Publisher 合同

- `Publish` 每次只处理一个信封，并在传输前强制校验；返回 nil 表示消息传输已明确、持久地接受该事件，不表示任何业务消费者已经处理成功。
- Publisher 不改变信封 ID、类型、版本、作用域、分区键、载荷或因果关系；协议头映射必须可逆地保留这些字段。
- 远端明确拒绝且重试不会改变结果时，错误包装 `ErrPublishRejected`，例如不支持的大小、协议或授权配置。
- 连接在确认前中断且无法判断远端是否接受时，错误包装 `ErrPublishUncertain`。该错误会按 At-Least-Once 语义重试，可能产生重复投递。
- 除 context 取消/超时和明确拒绝外，未分类基础设施错误默认视为可重试；适配器必须保留底层原因供 `errors.Is`/`errors.As` 和安全诊断使用。
- Publisher 不自动重试、不写 Outbox、不修改 Outbox 状态。重试、领取和状态转换由发布进程与 Outbox 适配器共同编排。
- `PublisherFunc` 只进行函数适配，不增加恢复 panic、重试、日志或遥测副作用。

## Outbox 状态机

逻辑状态为：

```text
PENDING --claim--> LEASED --publish success-----------------> PUBLISHED
   ^                  |
   |                  +--RecordFailure, budget remains------> PENDING(at retry_at)
   |                  +--rejected/budget exhausted----------> ABANDONED
   +------lease expired without terminal update----------------+
```

- `Enqueue` 必须在消费者业务写入使用的同一个 `application.Transactor` 回调中执行，不得在提交后补写。
- 唯一键为 `Source + EventID`。重复入队返回包装了 `ErrAlreadyEnqueued` 的错误，不静默覆盖、合并或替换已有载荷。
- `Enqueue` 只创建 `PENDING` 记录，不直接调用 Publisher；事务回滚时业务写入与 Outbox 记录同时消失。
- `Claim` 只领取 `available_at <= Now` 且未被有效租约占用的记录；`Limit` 必须位于 `1..MaxOutboxClaimLimit`，`LeaseFor` 和 `MaxAttempts` 必须为正，`Now` 非零。
- `Limit` 同时约束一次调用最多检查的到期分区数和最多返回的 Delivery 数。领取上限终结、指针修复或空分区删除会占用检查名额但不产生 Delivery，因此返回数量可以小于 `Limit`；调用方不得把小批次解释为永久无工作。
- 成功领取原子产生新的不透明 `LeaseToken`，并把 `Attempt` 从 `0` 递增；因此 `Delivery.Attempt` 从 `1` 开始。租约到期后的再次领取形成新 token 和下一次 attempt。
- 队首已有 `Attempt >= MaxAttempts` 且不存在有效租约时，`Claim` 不得产生下一次 Delivery；Store 必须在同一领取事务中把该事件推进为 `ABANDONED`、使用 `FailureClaimLimitExceeded`，再推进或删除分区指针。该内部终结不计入返回 `Limit`，Claim 可以继续扫描其他分区补足批次。
- 同一 `PartitionKey` 在任一时刻最多有一个已领取事件。只允许领取该分区最早的非终态记录，避免并发发布导致同一资源事件乱序。
- `MarkPublished`、`RecordFailure` 和 `Abandon` 必须使用当前 lease token 做条件更新；租约已失效、token 不匹配或状态已改变时返回包装了 `ErrLeaseLost` 的错误，不能覆盖新持有者的结果。
- `RecordFailure` 只接受有效 `LEASED`，要求非零 `FailedAt`、`RetryAt >= FailedAt`、非空低敏感度 `Code` 和正数 `MaxFailures`。Store 在同一事务中把 `Failures` 递增 1；新值小于 `MaxFailures` 时回到 `PENDING` 并使用 `RetryAt`，达到或超过上限时直接进入 `ABANDONED` 并使用 `FailedAt` 作为终结时间。
- `ErrPublishRejected` 通过 `Abandon` 立即进入 `ABANDONED` 且不消耗失败预算；`ErrPublishUncertain` 和其他可重试错误通过 `RecordFailure` 记录，在剩余预算内延迟重试，预算耗尽后原子进入 `ABANDONED`。
- `PUBLISHED` 与 `ABANDONED` 是普通运行路径的终态。`v0.1` 不提供原地重放或修改历史信封的公共 API；修复与补偿由消费者建立显式运维流程。
- `ABANDONED` 表示框架停止自动发布，不等同于已经写入某个 Broker DLQ。是否转发到 DLQ 属于后续具体消息适配器能力。
- 前序事件 `ABANDONED` 后同一分区可以继续发布，因此消费者必须对顺序缺口告警；框架不伪造前序成功。

### 尝试与失败计数

`Attempt` 与 `Failures` 是两个语义独立的计数器，只有后者驱动 `ABANDONED`。

- `Attempt` 记录该记录被成功领取的次数，由 `Claim` 原子递增，用于退避观测、诊断和防护上限。
- `Failures` 记录通过 `RecordFailure` 显式落库的可重试发布调用失败次数，包括结果不确定；它代表已经消耗的失败预算。
- 租约过期是被动超时，没有调用方，因此不经过 `RecordFailure`，不消耗 `Failures`；它的成本体现在下一次 `Claim` 递增的 `Attempt` 上。
- 重试退避基于 `Failures` 计算。租约过期本身已经消耗了一个完整的 `LeaseFor` 时长，再叠加增长的退避会对慢速 Publisher 形成双重惩罚。
- `Failures` 达到 `DeliveryFailure.MaxFailures` 时由 `RecordFailure` 在同一事务内进入 `ABANDONED`，`FailureCode` 使用本次传入的 `Code`。
- `Attempt` 达到 `OutboxClaim.MaxAttempts` 后不得再产生新的 Delivery；若最后一次租约没有终结而到期，下一次 `Claim` 原子进入 `ABANDONED`，`FailureCode` 使用 `FailureClaimLimitExceeded`。

分离两个计数器的原因是单一计数器会把"被明确拒绝 N 次"和"租约过期 N 次"压缩成同一个数字，而这两种情况的处置动作相反：前者需要修正事件内容或下游配置，后者需要调整超时与租约时长。单一计数器还会让仅仅是慢的 Publisher 被判定为永久失败——它一次都没有被拒绝，甚至可能每次都实际发布成功（旧持有者仍在飞行中），却因为不断触发租约过期而耗尽预算，表现为升级或抖动后一批事件静默消失。

领取上限是防护性的，不是正常运行路径的一部分。它的默认值应显著高于失败上限（建议 5 倍），使其在稳态下永不触发，只用于终结"每次都在租约到期前崩溃"的 Publisher——这类 Publisher 从不调用 `RecordFailure`，若没有该上限，其记录将永远无法终结并永久冻结所在分区。

失败上限、退避参数、租约时长和 Publisher 超时属于消费者发布进程的显式运行配置，不进入事件载荷。领取上限通过每次 `OutboxClaim.MaxAttempts` 显式传入，使 Store 可以在 Claim 事务中原子执行。`R-04` 必须固定 PostgreSQL 锁查询和 schema，但不得改变上述状态含义。

### 分区串行的代价

同一 `PartitionKey` 严格串行是顺序保证的来源，也是可用性代价的来源。消费者选择分区键粒度时必须理解以下后果。

单个无法发布的事件会阻塞其所在分区的全部后续事件，直到它进入 `ABANDONED`。最大阻塞时长等于该事件全部失败退避之和，加上期间发生的全部租约超时之和。分区键越粗，受影响的事件范围越大：按租户分区意味着一个坏事件冻结该租户的整条集成事件流，按聚合实例分区则只影响单个资源。

框架不限制分区键粒度，也不提供跳过队首事件的能力——跳过会破坏该分区的顺序保证，而顺序保证正是分区串行存在的理由。消费者必须能够根据自己配置的失败上限、退避曲线和租约时长计算出最大阻塞时长，并据此选择分区键粒度与告警阈值。

适配器必须暴露分区队首事件的等待时长指标，使阻塞在达到最大时长之前即可被发现。

## Inbox 状态机与幂等合同

逻辑状态为：

```text
ABSENT --Start in transaction--> PROCESSING
PROCESSING --business effects + MarkProcessed + commit--> PROCESSED
PROCESSING --MarkRejected + commit---------------------> REJECTED
PROCESSING --error/cancel/panic/rollback---------------> ABSENT
```

- `InboxKey` 固定由稳定的 `Consumer + Source + EventID` 组成。`Consumer` 表示逻辑订阅者，在部署、扩缩容和版本升级期间保持不变；更换名称等同于建立一个会重新处理历史事件的新消费者。
- `Start`、业务副作用和 `MarkProcessed`/`MarkRejected` 必须使用同一个派生事务 context。`PROCESSING` 不允许作为独立、已提交的长期状态存在。
- 首次成功占用返回 `Acquired=true`、`Status=InboxProcessing` 和有效 Receipt。
- 已存在 `PROCESSED` 或 `REJECTED` 时返回 `Acquired=false`、对应终态和零值 Receipt，不再次执行业务 Handler；消息适配器只在该读取事务成功结束后确认传输消息。
- 并发处理同一键时使用有界等待获取语义。`Start` 不做无界锁等待：在配置的短超时内无法取得占用时返回零值 Result 和包装了 `ErrInboxBusy` 的错误。调用方必须把该错误返回给 `Transactor` 触发回滚，并由消息适配器按其重投策略稍后重新投递。
- `ErrInboxBusy` 表示另一个处理者正在处理中、结果未知；它不同于 `Acquired=false` 且状态为 `PROCESSED`/`REJECTED` 的正常重复结果。前者要求回滚且不 ACK，后者要求事务正常结束后 ACK。
- PostgreSQL 等适配器若因有界等待错误使底层事务进入不可继续状态，必须使用 `ErrInboxBusy` 的 error 路径强制回滚，不得返回一个看似成功但无法提交的普通结果。
- 采用有界等待而不是无界锁等待，是因为后者会让后到的处理者挂起整个前者业务事务的时长；在重投风暴下这会耗尽连接池，而重投在 At-Least-Once 下本就是正常路径且成本低廉。若某个消费者的重投延迟过长，应由该消费者调整其消息适配器的重投策略，而不是让框架恢复无界锁等待。
- 超时时长是适配器配置项，不进入核心契约。`R-04` 固定 PostgreSQL 实现所用的机制与默认值。
- 可重试业务/基础设施失败、取消或 panic 必须回滚 Inbox 占用和全部本地业务副作用，由消息适配器决定重新投递时间；Inbox 不单独保存一个可能与业务状态分离的失败记录。
- 只有确认重试不会成功的结构、协议或业务拒绝才能 `MarkRejected`；FailureCode 必须稳定且不包含载荷、租户、主体、令牌、SQL 或原始异常文本。
- `PROCESSED` 和 `REJECTED` 在普通运行路径中不可逆。清理、保留期和受控重放属于运维/迁移设计，不进入 `v0.1` 公共 API。
- 事务提交成功后才能对 Broker ACK。提交结果不确定时不得假定处理失败；重新投递依赖 Inbox 终态安全抑制重复副作用。
- 在 Inbox 事务中直接调用不可回滚的外部 API 不能获得 Exactly-Once。需要可靠外发时，应在同一事务内写消费者自己的业务结果和新的 Outbox 事件。

## 生产与消费推荐顺序

### 生产

1. 应用层从 `domain.EventBuffer.Pending()` 取得稳定副本。
2. 消费者映射为包含新 EventID、明确 Scope/Subject/PartitionKey 和 JSON Payload 的 Envelope，并调用 `Validate`。
3. 在同一个 `Transactor` 回调中保存业务写入、授权证据和 `OutboxWriter.Enqueue`。
4. 事务明确提交成功后才 `EventBuffer.Acknowledge()`。
5. 独立发布进程领取 Outbox，调用 Publisher，并使用当前 LeaseToken 完成状态转换。

### 消费

1. 验证信封格式、受支持的 `Type + Version` 和来源信任边界。
2. 在本地 `Transactor` 中调用 `InboxStarter.Start`。
3. 若已经 `PROCESSED` 或 `REJECTED`，不重复执行副作用；事务结束后 ACK。
4. 若返回 `ErrInboxBusy`，说明另一个处理者正在处理；把错误返回给 Transactor 以回滚，不 ACK，等待消息适配器重投。
5. 若取得 `PROCESSING`，执行业务校验和本地副作用，然后在同一事务内标记 `PROCESSED`。
6. 对永久不可处理的事件在无业务副作用的事务内标记 `REJECTED`；可重试失败则回滚。
7. 只有本地提交成功后才 ACK；需要外发的后续事实写入同一事务的本地 Outbox。

## 明确拒绝的能力

- Exactly-Once 声明、分布式事务、跨数据库两阶段提交。
- 自动扫描聚合、自动把领域事件映射为集成事件或持久化 Hook 发布。
- 通用 Broker Client、Topic/Subject 命名、订阅路由和消费者注册表。
- 任意 Headers/Metadata map、反射序列化、Go 类型名作为事件类型。
- 通用 Schema Registry、Avro/Protobuf Codec Registry 或动态业务类型加载。
- 自动业务重试、通用死信重放、跳过 Inbox 的“幂等 Handler”标记。
- 从 `context.Context` 推断 Scope、Subject、PartitionKey、租户或业务身份。

除 Envelope 结构错误外，空 Consumer、空 Source/EventID、无效 Claim、空 LeaseToken、零时间、空或超长 FailureCode 等调用参数错误统一包装 `ErrInvalidRequest`。FailureCode 只允许小写 ASCII 字母、数字、点、下划线和短横线，并以字母或数字开头；它是稳定分类码而不是异常消息。语法和长度限制不能保证低基数，原始 FailureCode 不得直接作为 metric label。

## 最小测试合同

- Envelope 必填字段、版本、JSON 对象载荷和时间验证；错误可通过 `errors.Is` 识别。
- `SpecVersionV1` 固定为 `"1.0"`，`SpecVersion` 指向当前写入版本；Validate、Unmarshal、Enqueue 和 Publish 按信封自身版本处理，未知版本同时返回两类稳定错误。
- 多字节 UTF-8 字符串按字节而不是 rune 数执行全部 `Max*Length` 边界；PostgreSQL 时间往返按 UTC/微秒精度比较。
- JSON round-trip 保留所有字段，Payload 不被 Base64 编码，时间规范化为 UTC；未知附加字段被忽略，未知 SpecVersion 被拒绝。
- `Clone` 深拷贝 Payload，Publisher/Outbox 不受调用方后续切片修改影响。
- 同一 `Type + Version` 的序列化夹具在框架版本间保持兼容，事件 schema 版本与 Repository Revision 独立。
- PublisherFunc 透明传播 context、信封和错误；Publisher 的 rejected/uncertain 分类可识别。
- `Enqueue` 与 `Publish` 拒绝未通过 `Validate` 的信封，返回 `ErrInvalidEnvelope` 且不产生持久化或传输副作用。
- 已持久化信封在发布侧校验失败时进入 `ABANDONED` 并携带 `FailureEnvelopeInvalidOnPublish`，不消耗失败预算。
- 业务写入与 Enqueue 同事务提交或回滚；重复 `Source + EventID` 返回 `ErrAlreadyEnqueued` 且不覆盖。
- Claim 原子领取、Attempt 从 1 递增、租约过期可重新领取、旧 token 的完成操作返回 `ErrLeaseLost`。
- 租约过期只递增 `Attempt` 不递增 `Failures`；仅 `RecordFailure` 递增 `Failures`，退避基于递增后的 `Failures` 计算。
- `RecordFailure` 在预算未耗尽时回到 `PENDING`，达到 `MaxFailures` 时在同一事务中进入 `ABANDONED`；`Abandon` 不递增 `Failures`。
- 达到失败上限进入 `ABANDONED` 并保留最后一个失败分类；达到领取上限进入 `ABANDONED` 并携带 `FailureClaimLimitExceeded`。
- 持续在租约到期前崩溃的 Publisher 最终由领取上限终结，不会永久冻结分区。
- 同一 PartitionKey 不并发领取且保持入队顺序，不同 PartitionKey 可以并行。
- Claim 拒绝零值、负数或超过 `MaxOutboxClaimLimit` 的 Limit；修复、清理和领取上限终结不会让单次事务检查超过 Limit 个分区。
- 发布成功、重试、结果不确定、明确拒绝、失败预算耗尽和租约丢失路径符合状态机。
- Inbox 首次占用、已处理重复、已拒绝重复和并发占用只产生一次本地业务副作用。
- Inbox 并发占用为有界等待：后到的处理者在配置超时内返回可通过 `errors.Is` 识别的 `ErrInboxBusy`，事务必须回滚，不等待前者事务结束。
- Inbox 处理失败、取消、panic 和数据库明确确认未提交的提交失败均回滚占用与业务副作用；提交结果不确定时不得假定已经回滚，重新投递由可能已经提交的 Inbox 终态抑制重复本地副作用。
- InboxKey 不含 Scope，但跨消费者标识相互独立；改变 Consumer 名称会形成新的处理域。
- Trace Context 缺失或无效不拒绝业务事件；默认日志、span 和 metric 不包含信封敏感字段或 Payload。

## 下一步

PostgreSQL Outbox/Inbox Store 已实现并通过仓库内 `I-04` 和公共 `testkit` 合同。下一步由 TMS、Parcel 在不可变候选上验证各自 Envelope 映射、Publisher 编排和消费事务。
