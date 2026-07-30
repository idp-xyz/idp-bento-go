# `testkit` 公共合同决策简报

状态：已确认

Date: 2026-07-29

## 主题与范围

本简报确认 `R-05`：固定 `testkit` 的公共 API、确定性测试替身、合同运行器、并发与失败语义，以及公共测试工具和 PostgreSQL 专属测试基础设施之间的边界。

本简报不提供业务 Repository 实现，不创建 PostgreSQL 容器或迁移工具，不拥有 TMS/Parcel 的领域事件、聚合、作用域键或业务夹具，也不决定 `R-06` 的公共 API 快照格式。

## 已验证事实

- `application.Clock` 和 `application.IDGenerator` 已进入稳定核心，两个消费者都需要在领域事实与事件映射测试中消除系统时间和随机 ID。
- `eventing.Publisher` 是消费者发布编排的边界。测试发布成功、明确拒绝、结果不确定和重试时，需要能观察调用顺序与成功结果，但不应绑定具体消息产品。
- Repository Port 只表达加载、创建和条件更新；聚合构造、强类型作用域键、字段映射和 SQL 全部由消费者拥有。框架无法提供一个不泄漏业务语义的通用内存 Repository。
- Outbox/Inbox 的可移植状态机属于核心合同，而 PostgreSQL DDL、锁、执行计划、SQLSTATE 和多连接竞争属于实验性适配器实现证据。
- 生产包反向依赖测试工具会把测试状态、注错能力和 `testing` 依赖带入生产依赖图，必须由架构门禁阻止。
- 两个消费者需要复用相同合同语义，但其业务类型、测试数据、数据库生命周期和 CI 安全边界不能进入框架制品。

## 评估选项

### A. 提供完整内存 DDD/Repository/Outbox/Inbox 实现

优点是消费者单元测试启动快。代价是框架必须猜测聚合身份、作用域、事务隔离和查询语义；内存实现还会绕过真实 SQL、锁、约束和提交失败，容易产生只能通过假实现的测试。

### B. 只提供固定时钟和固定 ID

公共面最小，但两个消费者会重复实现 Publisher 记录、错误注入和 Repository/Eventing 合同矩阵。适配器语义会再次退化为消费者纪律。

### C. 确定性替身 + 工厂驱动真实实现的合同运行器

框架提供只表达稳定技术语义的固定时钟、顺序 ID、记录式 Publisher 和合法信封；Repository、Outbox、Inbox 合同运行器只接收消费者或适配器构造的真实端口。PostgreSQL 专属生命周期和竞争夹具留在适配器测试或 `internal`。

## 决定

采用选项 C。

- `testkit` 是稳定核心中的测试专用公共包，只依赖 Go 标准库及 `domain`、`application`、`repository`、`eventing`。
- 确定性替身不读取环境变量、不使用全局状态、不启动 goroutine、不做网络或数据库访问。
- 固定时钟不可变；需要推进时间时用新的时间创建新实例，不提供可变 `Advance` 或 `Reset`。
- 顺序 ID 生成器并发安全；已取消的调用不消耗 ID，序列耗尽返回稳定错误而不是重复最后一个 ID。
- 记录式 Publisher 在任何记录或错误计划消费前执行 context 与信封校验；保存和返回的信封全部深拷贝 Payload。
- Repository、Outbox、Inbox 合同运行器使用工厂为每个子测试建立隔离的真实实现，不提供通用内存 Store。
- PostgreSQL 容器、迁移、schema 检查、锁竞争、SQLSTATE、执行计划和版本矩阵不进入公共 `testkit`。
- 生产包不得导入 `testkit`；框架与两个消费者都必须用生产依赖图门禁检查该规则。

## 首版公共 API

```go
package testkit

import (
	"context"
	"errors"
	"testing"
	"time"

	"go.idp.xyz/idp-bento-go/application"
	"go.idp.xyz/idp-bento-go/eventing"
	"go.idp.xyz/idp-bento-go/repository"
)

var ErrIDSequenceExhausted = errors.New("testkit: id sequence exhausted")

type FixedClock struct {
	// unexported state
}

func NewFixedClock(now time.Time) FixedClock
func (c FixedClock) Now() time.Time

type SequenceIDGenerator[ID any] struct {
	// unexported state
}

func NewSequenceIDGenerator[ID any](ids ...ID) *SequenceIDGenerator[ID]
func (g *SequenceIDGenerator[ID]) NewID(ctx context.Context) (ID, error)

type PublishAttempt struct {
	Envelope eventing.Envelope
	Err      error
}

type RecordingPublisher struct {
	// unexported state
}

func NewRecordingPublisher(outcomes ...error) *RecordingPublisher
func (p *RecordingPublisher) Publish(
	ctx context.Context,
	envelope eventing.Envelope,
) error
func (p *RecordingPublisher) Attempts() []PublishAttempt
func (p *RecordingPublisher) Published() []eventing.Envelope

func ValidEnvelope() eventing.Envelope

type RepositoryContractScenario[K, A any] struct {
	Transactor          application.Transactor
	Loader              repository.Loader[K, A]
	Inserter            repository.Inserter[A]
	Updater             repository.VersionedUpdater[K, A]
	Key                 K
	OtherScopeKey       K
	Aggregate           A
	UpdatedAggregate    A
	OtherScopeAggregate A
	Equal               func(got, want A) bool
}

type RepositoryContractFactory[K, A any] func(
	t *testing.T,
) RepositoryContractScenario[K, A]

func RunRepositoryContract[K, A any](
	t *testing.T,
	newScenario RepositoryContractFactory[K, A],
)

type OutboxContractScenario struct {
	Transactor     application.Transactor
	Writer         eventing.OutboxWriter
	Claimer        eventing.OutboxClaimer
	Finalizer      eventing.OutboxFinalizer
	First          eventing.Envelope
	SamePartition  eventing.Envelope
	OtherPartition eventing.Envelope
	Now            time.Time
	LeaseFor       time.Duration
}

type OutboxContractFactory func(t *testing.T) OutboxContractScenario

func RunOutboxContract(
	t *testing.T,
	newScenario OutboxContractFactory,
)

type InboxContractScenario struct {
	Transactor application.Transactor
	Starter    eventing.InboxStarter
	Completer  eventing.InboxCompleter
	Key        eventing.InboxKey
	Now        time.Time
}

type InboxContractFactory func(t *testing.T) InboxContractScenario

func RunInboxContract(
	t *testing.T,
	newScenario InboxContractFactory,
)
```

示例直接使用 `F-40` 已绑定的正式 module path `go.idp.xyz/idp-bento-go`。字段可以按 `gofmt` 对齐拆分到多个文件，但不得增加同义构造器、全局默认实例或可变重置 API。

## 确定性替身合同

### FixedClock

- `NewFixedClock` 遇到零时间立即 panic；零时间会违反领域事件和信封合同，属于测试装配错误而不是可恢复业务结果。
- 构造函数保存传入的精确 `time.Time`，不读取系统时钟、不自动转 UTC，也不丢弃其 location；测试需要 UTC 时必须显式传入 UTC 值。
- `Now` 每次返回同一值。`FixedClock` 只有不可导出值状态，复制后行为相同，并发读取安全。
- 不提供 `Advance`、`Set` 或 `Reset`。多阶段测试通过 `NewFixedClock(clock.Now().Add(delta))` 创建新实例，使每个阶段的时间来源在装配代码中可见。

### SequenceIDGenerator

- 构造函数防御性复制输入 slice；允许空序列，用于直接验证耗尽路径。
- 每次成功调用按构造顺序消费一个 ID。框架不判断泛型 ID 的零值、格式或唯一性；消费者应传入不可变值类型和符合自身规则的 ID。
- `NewID` 先检查 context。已取消或超时返回 ID 零值与 `ctx.Err()`，不消耗序列元素。
- 序列耗尽返回 ID 零值与可通过 `errors.Is` 识别的 `ErrIDSequenceExhausted`；不得重复最后一个 ID、回绕或临时生成随机值。
- 生成器使用互斥保护并发消费，任何 ID 最多返回一次。并发 goroutine 取得 ID 的先后由实际调用竞争决定；需要把指定 ID 绑定到指定步骤的测试必须顺序调用。
- 不提供 `Reset`、当前位置修改或全局默认生成器；每个测试创建独立实例。

### RecordingPublisher

- `NewRecordingPublisher` 防御性复制 `outcomes`。第 N 个通过 context 和信封校验的发布尝试使用第 N 个 error；`nil` 表示成功，计划耗尽后默认成功。
- `Publish` 先检查 `ctx.Err()`，再调用 `Envelope.Validate()`。取消、超时或无效信封不记录 Attempt、不消耗预设 outcome，也不产生 Published 结果。
- 有效调用会记录一个 `PublishAttempt`。预设 error 按原值返回并记录在 Attempt 中；只有 outcome 为 nil 时才把信封加入 `Published`。
- 每次记录前调用 `Envelope.Clone()`；`Attempts`、`Published` 返回新的 slice，并再次深拷贝其中每个 Envelope。调用方后续修改原 Payload 或返回结果都不能改变内部记录。
- Publisher 并发安全；记录顺序是进入受保护发布段的顺序。它不保存 context、不 sleep、不自动重试、不改变信封，也不把 error 自动改写为发布分类。
- 不提供 `Reset`。需要新错误计划或空记录时创建新实例，防止跨子测试共享可变历史。

### ValidEnvelope

- 返回满足当前 `SpecVersion` 全部校验规则的业务无关信封，使用固定 UTC 时间、固定技术测试值和非空 JSON object Payload。
- 每次调用返回独立 Payload；调用方可以从该值出发只修改一个字段以验证边界。
- 该函数仅是协议测试起点，不得用于生产、消费者事件映射或作为 Source/Scope/Subject/PartitionKey 的默认值。
- `R-06` 已把该夹具与 `Envelope.Validate` 规则集纳入兼容性检测；同一 SpecVersion 下收紧规则时，旧夹具和兼容性门禁必须失败。

## 合同运行器合同

### 共同规则

- 三个 `Run*Contract` 只接收 `*testing.T` 和公开核心端口，不导入适配器、数据库驱动或消费者包。
- 运行器为每个具名子测试调用一次工厂。工厂必须返回全新、隔离的 scenario，并通过 `t.Cleanup` 释放其拥有的资源。
- 运行器不调用 `t.Parallel`。是否并行由消费者在确保数据库/schema 隔离后决定。
- 工厂配置错误使用 `t.Fatalf` 报告，端口行为偏离使用普通测试失败报告；所有稳定错误分类都用 `errors.Is`，不比较错误文本。
- 运行器不读取未导出状态、不探测 SQL、不依赖数据表结构。合同通过只证明端口可观察语义，不替代适配器实现证据。

### RepositoryContractScenario

工厂必须提供同一个事务 owner 下的 Loader、Inserter、Updater 和 Transactor。`Aggregate` 与 `UpdatedAggregate` 表示同一作用域、同一身份的两个状态；`OtherScopeAggregate` 使用与 `Aggregate` 相同的消费者业务 ID、不同权威作用域，并与 `OtherScopeKey` 对应。`Equal` 必须比较合同所需的完整聚合持久化结果。

运行器至少验证：

- `Insert` 必须在事务中执行，成功返回 Revision `1`；重复身份返回 `ErrAlreadyExists`。
- Load 返回对应聚合与 Revision；不存在和错误作用域统一返回 `ErrNotFound`。
- 两个作用域中的同业务 ID 可以独立存在并各自加载，不能串权。
- `Update` 以预期 Revision 原子更新并返回严格加一的新值；旧 Revision、不存在目标和错误作用域统一返回 `ErrConflict`。
- Insert/Update 所在事务回滚后不可观察到写入；成功提交后才能观察到状态。
- 事务外 Insert/Update 必须返回非 nil 基础设施错误且不产生写入。
- context 取消/超时保持可识别，不能被伪装为 Repository 稳定分类。

运行器不验证消费者领域不变量、字段级 SQL、业务唯一性、查询模型或授权结论。允许事务外读取的 ReadExecutor、SQL 是否存在额外探测以及 PostgreSQL 行数映射由适配器专属测试补充。

### OutboxContractScenario

三个信封都必须有效且 ID 不同；`First` 与 `SamePartition` 必须拥有相同 PartitionKey，`OtherPartition` 必须使用不同 PartitionKey。它们不得共享可变 Payload。`Now` 非零，`LeaseFor` 为正。

运行器至少验证：

- Enqueue 在事务提交后可领取，回滚后不可见；重复 `Source + EventID` 返回 `ErrAlreadyEnqueued` 且不覆盖原信封。
- 同分区按 Enqueue 顺序只领取队首，不同分区可以在同一批次独立领取。
- Claim 验证 Limit、LeaseFor、Now 和 MaxAttempts；Attempt 从 `1` 开始，租约过期重新领取只增加 Attempt，不增加 Failures。
- 返回的 Delivery 与 Envelope 是防御性副本；调用方修改 Payload 不改变后续领取结果。
- 旧 LeaseToken 或非队首终结返回 `ErrLeaseLost`；成功发布、显式 Abandon 和失败预算耗尽进入终态并推进分区。
- `RecordFailure` 只在显式调用时增加 Failures，预算未耗尽时按 RetryAt 重新可领取，达到 MaxFailures 时原子 `ABANDONED`；Abandon 不消耗失败预算。
- 达到 MaxAttempts 的队首不再产生 Delivery，并以 `FailureClaimLimitExceeded` 终结；单次检查不超过 Limit 个分区。
- 无效信封在 Enqueue 边界返回 `ErrInvalidEnvelope`，不产生持久化副作用。

公共运行器不承诺 wall-clock 性能、具体锁等待、SQL 计划或多 worker 竞争轮数。上述 PostgreSQL 并发证据继续由 `I-04` 和适配器内部集成测试验证。

### InboxContractScenario

工厂提供同一个事务 owner 下的 Starter、Completer 和 Transactor，以及一个合法 Key 和非零 Now。

运行器至少验证：

- 首次 Start 取得 `PROCESSING` receipt，并且必须在同一事务内 MarkProcessed 或 MarkRejected 后才能成功提交。
- 已处理和已拒绝的重复输入返回对应终态且 `Acquired=false`，不能再次执行业务副作用。
- Start 或业务副作用所在事务回滚后不留下可观察占用，后续调用可以重新取得。
- receipt、key、时间或 FailureCode 非法时返回 `ErrInvalidRequest`；目标缺失、已经终结或不是当前事务 `PROCESSING` 占用时返回 `ErrInvalidTransition`。
- 两个独立事务并发 Start 同一 Key 时，后到调用返回 `ErrInboxBusy`，外层事务回滚；不把竞争伪装为普通未取得结果。
- 不同 Consumer 的相同 Source + EventID 是不同幂等域。

PostgreSQL 的具体锁等待上限、`lock_timeout` 恢复、SQLSTATE 映射和延迟约束触发器由适配器内部测试与 `I-04` 验证，公共运行器只验证可观察的端口结果。

## 公共和 internal 边界

### 允许公开

- 本文首版公共 API 中列出的确定性替身、合法信封和三个合同运行器。
- 只使用公开核心接口、业务无关值和 `testing` 标准库的测试辅助。
- 两个消费者都可在自身 `_test.go` 中实例化、且不要求向框架暴露业务源码的合同入口。

### 必须保持包内测试或 internal

- EventBuffer、HandlerFunc、Transactional、Envelope JSON 与错误包装等框架具体实现的白盒单元测试。
- PostgreSQL 容器创建、连接串、数据库管理员凭据、schema 创建/删除、迁移执行和 catalog 探测。
- DDL 负向夹具、SQLSTATE、advisory lock、`SKIP LOCKED`、多连接屏障、执行计划和 10 万分区数据生成。
- 具体 Broker 模拟器、HTTP server、Testcontainers 封装、随机属性测试框架和第三方断言库。
- TMS/Parcel 的聚合、作用域键、事件类型、Payload、数据库行和真实切片数据。

如果未来出现第二种真实 Outbox/Inbox 适配器，先用当前公开运行器验证可移植语义；只有两个实现都需要且不含基础设施细节的新夹具，才可以按公共 API 流程提出。

## 生产依赖门禁

- 框架 CI 对每个非 `testkit` 生产包运行不含测试变体的 `go list -deps` 或等价导入图检查；依赖闭包出现当前 module 的 `/testkit` 即失败。
- TMS、Parcel 的 `bento-contract` 对产品生产包执行同类检查；`*_test.go`、外部测试包和专用合同测试命令允许导入 `testkit`。
- `testkit` 不使用 `init`、全局注册、环境变量或构建标签改变生产行为。消费者最终二进制不应因测试而包含测试夹具。
- 架构门禁同时验证 `testkit` 不导入 `postgres`、`pgx/v5`、OpenTelemetry SDK 或任一消费者 module。

## 双消费者最小夹具

框架 PR CI 只运行框架自测和不含产品语义的内置编译夹具。不可变候选阶段，TMS 与 Parcel 分别在自己的仓库和安全边界中：

1. 使用 `FixedClock`、`SequenceIDGenerator` 和 `RecordingPublisher` 驱动一个最小真实应用切片。
2. 使用消费者自己的强类型复合键、聚合与显式 SQL Repository 实例化 `RunRepositoryContract`。
3. 对适用 PostgreSQL Store 实例化 `RunOutboxContract` 与 `RunInboxContract`，并继续运行 `I-04` 要求的适配器专属测试。
4. 验证生产依赖图不包含 `testkit`，候选版本和 module checksum 与合同证明一致。

合同运行器、消费者源码和业务数据不跨仓复制。框架只接收候选版本、合同套件版本、消费者提交、module checksum、结果和完成时间。

## 主要代价与缓解

- 工厂式合同需要消费者编写少量装配代码；代价换来对真实事务、真实 SQL 和真实作用域键的验证，并避免共享假 Repository。
- 公开 scenario 结构会进入 SemVer 公共面；`R-06` 必须对其做 API 快照，新增必填字段需要兼容演进或新运行器版本。
- `RecordingPublisher` 不能证明真实 Broker 接受语义；具体消息适配器出现时仍须在消费者仓库建立协议级合同。
- 公共运行器不能替代 PostgreSQL 并发与性能证据；`I-04` 继续阻断适配器完成和发布。
- 固定时钟不支持原地推进会多写少量装配代码，但消除了共享可变时间导致的顺序依赖和并发不确定性。

## 明确拒绝

- 通用内存 Repository、内存 Unit of Work、内存 Outbox/Inbox 或自动回滚对象图。
- Fake/Mock `Transactor`、Repository Registry、Service Locator 或反射式 mock 生成器。
- 可变全局时钟、自动递增墙钟、随机 ID fallback、ID 序列回绕或 Publisher 自动重试。
- 在 `testkit` 中公开 PostgreSQL/Broker 容器、迁移执行器、数据库清理器或客户凭据处理。
- 把 TMS/Parcel 业务对象、事件类型、租户标识或数据库 schema 作为框架示例夹具。
- 让生产代码导入 `testkit`，或依赖测试替身提供生产降级路径。

## 完成判据

`R-05` 在以下内容全部进入实现任务后可验收：

- 所有首版符号具有单元测试、并发测试、取消/耗尽/深拷贝负向测试和 Go 文档。
- 三个合同运行器对一个真实 PostgreSQL 实现通过；刻意移除 Revision 条件、事务严格模式、Envelope 深拷贝或 Inbox busy error 时对应合同必须失败。
- 框架、TMS、Parcel 的生产依赖门禁能用故意导入 `testkit` 的负向夹具证明有效。
- TMS、Parcel 各有一个最小真实切片使用同一候选运行合同，且不把业务源码或数据库快照传回框架。
- `testkit` 的依赖图只包含标准库和公共核心，不包含任何实验性适配器或消费者包。

这些是实现验收，不是当前文档已经实现的能力。

## 后续门禁

`R-05` 已实现，Repository/Outbox/Inbox 公共合同运行器已经建立；Outbox 与 Inbox 运行器已对 PostgreSQL 16.12 真实端口执行并通过。下一步由 TMS、Parcel 在同一不可变候选上运行其拥有的合同工厂。

本决定已经进入 [ADR-0006](../adr/0006-testkit-real-port-contracts.md)。最终实施决策简报已经确认，按实施顺序创建 Go 代码；真实消费者合同仍须由 TMS 与 Parcel 候选证明。
