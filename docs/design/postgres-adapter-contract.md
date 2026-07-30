# PostgreSQL 适配器公共合同

状态：已确认

## 目标

固定实验性 `postgres`、`postgres/outbox` 和 `postgres/inbox` 适配器的事务绑定方式、技术 schema、领取锁策略、配置结构和迁移清单访问接口，使 `eventing` 与 `application` 的已确认语义具备唯一确定的 PostgreSQL 实现，同时不把业务 Repository、ORM 或迁移执行能力带入框架。

## 非目标

- 不提供业务 Repository、查询 DSL、struct 自动映射或反射扫描。
- 不提供迁移执行器、迁移历史表或启动时自动迁移。
- 不选择消费者的迁移工具、连接池装配方式或部署编排。
- 不定义 Broker 适配器；`Publisher` 的具体消息产品实现不属于本合同。

## 已确认上游约束

- 事务句柄通过 `postgres` 包私有的 context key 传播，绑定内容同时包含所属数据库/Transactor 实例与 `pgx.Tx`。
- 原始 `pgx.Tx` 不对消费者暴露；消费者 Repository 通过窄化 Querier/Executor 参与事务。允许事务外读取的 Querier 不含 `Exec`，严格 Executor 不含 `Commit`/`Rollback`。
- 写入参与者采用严格模式：未绑定事务时返回基础设施错误；绑定属于其他数据库/Transactor 实例时返回 `application.ErrTransactionConflict`。
- 读取实现必须显式选择允许连接池读取或严格事务读取，不能与写入路径共用静默回退。
- 扫描辅助只覆盖 `Revision` 提取与错误分类映射，不提供通用行到结构体的映射。
- 框架只发布不可变迁移资产与只读访问/渲染 API，消费者拥有迁移执行器、历史和部署编排。

## `R-04` 分阶段状态

- `R-04-A` 已确认 DB/Store 公共 API 与发布编排所有权。
- `R-04-B` 已确认默认 `bento` schema、`outbox`、`outbox_partition`、`inbox` 三张表的完整 DDL、约束和索引；规范来源为 [PostgreSQL 技术表 DDL 决策简报](./postgres-schema-ddl-decision-brief.md)。
- `R-04-C` 已确认同分区并发 Enqueue、Claim、Finalize、Inbox 和指针修复 SQL；规范来源为 [PostgreSQL 并发 SQL 与锁顺序决策简报](./postgres-concurrency-sql-decision-brief.md)。
- `R-04-D` 已确认迁移模板渲染、checksum、schema 标识符与 `CheckSchema` 对象集合；规范来源为 [PostgreSQL 迁移渲染与 schema 检查决策简报](./postgres-migration-schema-decision-brief.md)。
- `R-04-E` 已保存 PostgreSQL 16.12 的 DDL、并发、负向、catalog 与执行计划设计证据；真实实现的仓库内 `I-04` 矩阵已于 2026-07-30 通过，受保护 CI 证据仍待 `B-05` 绑定后执行。

## 技术 schema 与表名

框架技术表位于独立 schema，默认 `bento`，由适配器构造选项配置。适配器在所有 SQL 中使用完全限定名，不依赖 `search_path`。

采用独立 schema 而不是在消费者默认 schema 中使用 `bento_` 前缀，原因是两类表的生命周期完全不同：框架技术表随框架版本迁移，业务表随产品迁移。schema 隔离使权限授予、备份策略、监控范围和迁移历史都能按 schema 粒度独立处理，也避免框架表名与消费者既有表名冲突。schema 名可配置是因为部分私有化客户环境限制 schema 创建权限或强制命名规范。

框架不创建 schema，也不要求运行账号持有 `CREATE` 权限。schema 的创建属于消费者迁移作业的第一步，框架迁移资产假定 schema 已存在。

### `outbox`

信封逐列保存：`ID`、`Type`、`Version` 分别映射到 `event_id`、`event_type`、`event_version`，`payload` 使用 `jsonb`。技术列包括全局 identity `enqueued_seq`、状态、可领取时间、UUID 租约、两个计数器、三个终结/失败时间和稳定失败分类。

主键 `(source, event_id)` 实现 `ErrAlreadyEnqueued` 去重；唯一约束 `(partition_key, enqueued_seq)` 同时支持同分区顺序扫描和分区指针复合外键。`attempt`、`failures`、`event_version` 使用 `bigint`，但 CHECK 将其约束在 `uint32` 范围，且 `failures <= attempt`。

状态形状由 CHECK 固定：

- `PENDING` 没有租约或终态时间；经过 `RecordFailure` 后可以保留最后失败时间和分类。
- `LEASED` 同时具有 UUID token 与租约到期时间，没有终态时间。
- `PUBLISHED` 只有 `published_at` 作为终态时间，清除租约字段。
- `ABANDONED` 只有 `abandoned_at` 作为终态时间，清除租约字段且必须具有 FailureCode。

活跃记录使用 `(recorded_at, enqueued_seq) WHERE status IN ('PENDING', 'LEASED')` 部分索引，服务待发布数量和最老记录延迟观测。全局 identity 允许空洞；它只提供单调比较值，不承诺连续编号。

### `outbox_partition`

分区状态表只保存 `partition_key`、`head_enqueued_seq` 和 `claimable_at`，不重复保存 `head_source` 或 `head_event_id`。复合外键 `(partition_key, head_enqueued_seq)` 引用 Outbox 的同名唯一约束，从数据库层保证指针不会跨分区或指向不存在的行。

该外键不能证明指针指向最早非终态事件；这个不变量由 `R-04-C` 的锁顺序和修复 SQL 以及 `R-04-E` 的并发/负向测试保护。分区在最后一个非终态事件终结时删除，外键不级联删除 Outbox 历史。

索引固定为 `(claimable_at, partition_key)`。领取只扫描 `claimable_at <= now()` 的分区；租约到期后 `claimable_at` 自然到期，因此崩溃持有者不会把分区永久排除。使用 `WHERE leased_until IS NULL` 的部分索引被明确否决，因为过期租约字段仍非空，会导致分区永远无法重新领取。

### `inbox`

| 列 | 类型 | 说明 |
| --- | --- | --- |
| `consumer` | `text NOT NULL` | 与 `source`、`event_id` 共同构成主键 |
| `source` | `text NOT NULL` | 事件来源 |
| `event_id` | `text NOT NULL` | 事件标识 |
| `status` | `text NOT NULL` | `PROCESSING`/`PROCESSED`/`REJECTED` |
| `received_at` | `timestamptz NOT NULL` | 首次接收时间 |
| `settled_at` | `timestamptz` | 进入终态的时间 |
| `failure_code` | `text` | `REJECTED` 时的稳定分类 |

主键 `(consumer, source, event_id)` 即幂等键。状态形状 CHECK 固定三种组合：`PROCESSING` 没有终结字段，`PROCESSED` 只有 `settled_at`，`REJECTED` 同时具有 `settled_at` 与 FailureCode；终结时间不得早于首次接收时间。

`PROCESSING` 只允许存在于未提交事务。DDL 使用 `DEFERRABLE INITIALLY DEFERRED` constraint trigger 监听 `INSERT` 和 `UPDATE`，在提交时按幂等键重新查询最终行；仍为 `PROCESSING` 时抛出 SQLSTATE `23514`。重新查询而不是直接检查触发事件的 `NEW.status`，是为了允许同一事务中的合法 `INSERT PROCESSING -> UPDATE PROCESSED/REJECTED`。`23514` 表示违反事务协议并保留为基础设施错误，不映射为并发占用的 `ErrInboxBusy`。

三张表的全部列、命名约束、函数、触发器和索引以 [PostgreSQL 技术表 DDL 决策简报](./postgres-schema-ddl-decision-brief.md) 为准。

## 事务绑定

`postgres.Transactor` 实现 `application.Transactor`。`WithinTransaction` 显式以 PostgreSQL `READ COMMITTED` 开启 `pgx.Tx`，不继承客户数据库可能被修改的默认隔离级别；首版不暴露隔离级别选项。开启事务后，把一个同时包含所属数据库/Transactor 实例标识与事务句柄的不导出结构写入派生 context，使用 `postgres` 包私有的 context key 类型。消费者不能伪造绑定，也不能取得带 `Commit`/`Rollback` 的原始事务，但必须能通过所属数据库实例解析窄化 Querier/Executor。

事务参与者从 context 取句柄时有三种结果。取到且实例标识匹配时正常参与该事务。取到但实例标识属于另一个数据库/Transactor 时返回包装 `application.ErrTransactionConflict` 的错误。完全取不到时，RequireExecutor 返回 `ErrTransactionRequired`；ReadExecutor 才允许使用构造时注入的连接池。

context key 必须是包级单一 key 而不是每个 Transactor 实例一个 key。若每实例一个 key，两个 Transactor 会各自找到自己的绑定或都找不到，跨事务冲突将无法被检测到。

候选公共 API 形态为：

```go
package postgres

const DefaultSchema = "bento"

type Querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

type Executor interface {
	Querier
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
}

var (
	ErrTransactionRequired = errors.New("postgres: transaction required")
	ErrInvalidConfig       = errors.New("postgres: invalid config")
	ErrSchemaIncompatible  = errors.New("postgres: schema incompatible")
	ErrMigrationNotFound   = errors.New("postgres: migration not found")
	ErrMigrationIntegrity  = errors.New("postgres: migration integrity failure")
)

type DB struct {
	// unexported fields
}

type Option interface {
	postgresOption()
}

func WithSchema(schema string) Option
func WithTracerProvider(provider trace.TracerProvider) Option
func WithMeterProvider(provider metric.MeterProvider) Option
func WithLogger(logger *slog.Logger) Option

func NewDB(pool *pgxpool.Pool, options ...Option) (*DB, error)
func (db *DB) Schema() string
func (db *DB) Transactor() application.Transactor
func (db *DB) ReadExecutor(ctx context.Context) (Querier, error)
func (db *DB) RequireExecutor(ctx context.Context) (Executor, error)
func (db *DB) CheckSchema(ctx context.Context) error
```

同一个 `DB` 必须始终返回同一个事务 owner 身份。`ReadExecutor` 在匹配事务存在时返回该事务的窄化 Querier，没有事务时返回注入 pool 的窄化 Querier，存在其他 owner 时返回 `ErrTransactionConflict`；它不暴露 `Exec`，因此不能被用于事务外写入。`RequireExecutor` 只接受匹配事务并返回含 `Exec` 的 Executor。实现必须返回包内私有包装器，不能把 `pgx.Tx` 或 `*pgxpool.Pool` 直接装入接口后返回，否则调用方仍可通过类型断言重新取得被删去的能力。两类包装器都不允许调用方取得 `Commit`/`Rollback` 或保存原始 `pgx.Tx`。

`Option` 使用包内未导出方法封闭，消费者只能使用框架提供的 `With*` 函数，不能自定义或注入未验证 Option。`NewDB` 对 nil pool、重复或无效 Option、非法 schema 以及显式传入的 nil provider/logger 返回包装了 `ErrInvalidConfig` 的错误。省略可观测性 Option 使用 no-op。构造只保存依赖并建立稳定 owner，不获取连接、不检查 schema、不启动 goroutine；消费者在启动门禁中显式调用只读的 `CheckSchema`。`Schema` 返回构造时已经验证且之后不可改变的 schema 名。

`CheckSchema` 在独立的 `REPEATABLE READ READ ONLY` 事务中验证 UTF8 数据库编码和当前二进制依赖的必需表、列、identity sequence、约束、索引、函数与触发器子集，允许兼容扩展对象。已有框架事务 context 返回 `application.ErrTransactionConflict`；catalog 访问、连接、取消和超时错误保留原错误，只有确认对象缺失或定义不兼容才包装 `ErrSchemaIncompatible`。完整对象 manifest 与比较规则以 `R-04-D` 决策简报为准。

Outbox Store 的公共 API 为：

```go
package outbox

type Store struct {
	// unexported fields
}

type Option interface {
	outboxOption()
}

func WithTracerProvider(provider trace.TracerProvider) Option
func WithMeterProvider(provider metric.MeterProvider) Option
func WithLogger(logger *slog.Logger) Option

func NewStore(db *postgres.DB, options ...Option) (*Store, error)

func (s *Store) Enqueue(ctx context.Context, envelope eventing.Envelope) error
func (s *Store) Claim(ctx context.Context, claim eventing.OutboxClaim) ([]eventing.Delivery, error)
func (s *Store) MarkPublished(ctx context.Context, ref eventing.DeliveryRef, publishedAt time.Time) error
func (s *Store) RecordFailure(ctx context.Context, ref eventing.DeliveryRef, failure eventing.DeliveryFailure) error
func (s *Store) Abandon(ctx context.Context, ref eventing.DeliveryRef, abandonedAt time.Time, failure eventing.FailureCode) error
```

Inbox Store 的公共 API 为：

```go
package inbox

const DefaultLockTimeout = 50 * time.Millisecond

type Store struct {
	// unexported fields
}

type Option interface {
	inboxOption()
}

func WithLockTimeout(timeout time.Duration) Option
func WithTracerProvider(provider trace.TracerProvider) Option
func WithMeterProvider(provider metric.MeterProvider) Option
func WithLogger(logger *slog.Logger) Option

func NewStore(db *postgres.DB, options ...Option) (*Store, error)

func (s *Store) Start(ctx context.Context, key eventing.InboxKey, receivedAt time.Time) (eventing.InboxStartResult, error)
func (s *Store) MarkProcessed(ctx context.Context, receipt eventing.InboxReceipt, processedAt time.Time) error
func (s *Store) MarkRejected(ctx context.Context, receipt eventing.InboxReceipt, rejectedAt time.Time, failure eventing.FailureCode) error
```

两个 Store 必须接收同一个 `*postgres.DB`，通过 `db.Schema()` 和 Executor API 与消费者业务 Repository 使用相同事务 owner 和 schema。Store Option 同样使用包内未导出方法封闭，只配置本包可观测性以及 Inbox 的有界等待，不允许再次覆盖 schema 或连接池。nil DB、非法或重复 Option、显式 nil provider/logger 统一返回包装了 `postgres.ErrInvalidConfig` 的错误。具体 Store 实现三个 Outbox 或两个 Inbox 小接口；消费者应把 `*Store` 注入为用例实际需要的窄接口。

## 扫描辅助边界

`postgres` 只提供两类辅助，均不涉及业务结构。

第一类是 `Revision` 语义：从条件更新的 `RETURNING revision` 提取新值，以及把"影响 0 行"统一转换为 `repository.ErrConflict`。第二类是错误分类映射：只有明确匹配业务唯一约束名的 SQLSTATE `23505` 才映射稳定错误，例如 `outbox_pk` 映射为 `eventing.ErrAlreadyEnqueued`；其他唯一约束错误保留为基础设施错误。Inbox Start 路径的 `55P03` 映射为 `eventing.ErrInboxBusy`，延迟约束触发器的 `23514`、连接失败、序列化失败等保留为基础设施错误。`context.Canceled` 与 `context.DeadlineExceeded` 必须保持 `errors.Is` 可判定。

框架不提供行到结构体的映射、列名推导、反射标签解析或查询构造器。消费者编写显式 SQL 与显式扫描；这是"通用 Repository 不得变成隐藏 ORM"这一已确认边界在适配器层的直接后果。

## Outbox 领取

Enqueue 先使用 `hashtextextended('idp-bento-go/outbox/' || partition_key, 0)` 取得事务级 advisory lock，再锁已有 `outbox_partition`、写入 Outbox 并重算最早非终态队首。该顺序解决空分区没有行可锁的问题，并保证同分区 identity 顺序与事务序列化顺序一致。哈希碰撞只降低无关分区并发，不影响正确性。

实际行锁顺序固定为 `outbox_partition -> outbox`。Claim 从 `(claimable_at, partition_key)` 索引选出最多 `Limit` 个到期分区并 `FOR UPDATE SKIP LOCKED`；终结操作先无锁解析目标分区，再锁分区行，最后条件更新 Outbox。Claim 不取得 advisory lock，因此不会与已经持有 advisory lock、等待分区行的 Enqueue 形成环。

`OutboxClaim.Limit` 必须位于 `1..eventing.MaxOutboxClaimLimit`，同时限制本次检查的分区数和最多返回的 Delivery 数。指针修复、空分区删除和领取上限终结占用检查名额但不返回 Delivery，因此小批次是正常结果。

每个 Claim/Finalize `UPDATE` 必须在自身 `WHERE` 中复检状态、调用参数时间、MaxAttempts、lease token、租约有效期和当前队首。成功领取原子生成新 token、更新绝对租约时间、递增 `attempt` 并同步分区 `claimable_at`；`failures` 不变。终态操作在同一事务推进下一非终态事件或删除空分区。

指针修复查询始终按 `enqueued_seq` 选择最早非终态事件。错误指针被修复的本轮不返回 Delivery；找不到活跃事件时删除分区行。完整语句、参数与影响行数合同以 [PostgreSQL 并发 SQL 与锁顺序决策简报](./postgres-concurrency-sql-decision-brief.md) 为准。

## Inbox 占用

`Start` 在消费者事务内先通过 `set_config('lock_timeout', value, true)` 设置局部超时，再执行 `INSERT ... ON CONFLICT DO NOTHING`，成功后立即 `SET LOCAL lock_timeout = DEFAULT` 使后续业务语句恢复会话默认值。`WithLockTimeout` 只接受至少 `1ms` 的整毫秒值；Store 自行生成十进制毫秒字符串，不接受任意配置文本。

PostgreSQL 的 `INSERT` 没有 `NOWAIT` 选项。`ON CONFLICT DO NOTHING` 在遇到另一个未提交事务的同键插入时会等待该事务结束；预先执行 `SELECT ... FOR UPDATE NOWAIT` 也无法锁定尚未提交、对探测者不可见的行。因此候选实现使用短 `lock_timeout` 形成有界等待；该行为仍需以仓库内可复现并发脚本验证。

超时触发 SQLSTATE `55P03`，并使当前事务进入 aborted 状态。适配器必须返回零值 Result 和包装了 `eventing.ErrInboxBusy` 的错误，调用方把错误返回给 Transactor 强制回滚并等待消息适配器重投；不得把该路径翻译为 nil error 的普通未获取结果。

已存在终态行时 `ON CONFLICT DO NOTHING` 不产生冲突等待，适配器随后读取该行并返回 `Acquired=false` 与对应终态。

## 配置与运行编排所有权

框架构造函数只接收依赖和适配器自身参数，不读取环境变量、不使用全局可变配置。运行策略由消费者发布进程拥有，不能隐藏在 PostgreSQL Store 中。

| 所有者 | 参数 | 首版推荐基线 | 强制合同 |
| --- | --- | --- | --- |
| `postgres.DB` | `Schema` | `bento` | 通过 `WithSchema` 配置；适配器不创建 schema |
| `postgres/inbox.Store` | `LockTimeout` | `50ms` | 至少 `1ms` 的整毫秒值；只作用于 `Start` 的冲突插入语句 |
| 消费者发布进程 | `ClaimLimit` | `100` | `1..eventing.MaxOutboxClaimLimit`；限制每次检查和返回的分区数 |
| 消费者发布进程 | `MaxFailures` | `10` | 正数；每次 `RecordFailure` 显式传入，由 Store 原子判断是否耗尽 |
| 消费者发布进程 | `MaxAttempts` | `50` | 每次 Claim 显式传入；建议至少为 `MaxFailures` 的 5 倍 |
| 消费者发布进程 | `LeaseFor` | `60s` | 正数且必须大于 `PublisherTimeout` |
| 消费者发布进程 | `PublisherTimeout` | 小于 `LeaseFor` | 由具体 Publisher 与部署环境决定 |
| 消费者发布进程 | 退避 | base `1s`、factor `2`、cap `5m`、jitter `±20%` | 基于 `Failures`，不得基于 `Attempt` |

PostgreSQL Store 不调用 Publisher、不设置 Publisher 超时、不 sleep、不计算退避，也不启动 worker。消费者发布进程把明确拒绝映射为 `Abandon`，把结果不确定和其他可重试错误映射为 `RecordFailure`，并传入基于下一次 Failures 计算的绝对 `RetryAt` 与 `MaxFailures`。Store 在一次事务中递增计数并决定重排或终结，所有转换均受 lease token 保护且正确推进分区。

`LeaseFor > PublisherTimeout` 由消费者 bootstrap 在启动时校验并进入 TMS/Parcel 合同测试。Store 无法从 `eventing.Publisher` 端口发现超时，因此不得伪造构造期校验。租约短于发布超时会导致仅仅是慢的 Publisher 不断触发租约过期。

`v0.1` 不提供常驻 `Processor`、scheduler 或 worker pool 公共 API。`R-05 testkit` 通过固定时钟、顺序 ID 和记录式 Publisher 支持消费者测试自己的运行策略，但不提供退避策略、常驻循环或 Store 假实现；进程生命周期、并发度、关闭和部署仍由消费者拥有。

## 迁移资产与清单访问

框架把迁移 SQL 以 `embed.FS` 嵌入，并提供只读清单 API：

```go
package postgres

type Migration struct {
	ID                  string
	Checksum            string
	MinFrameworkVersion string
}

func Migrations() []Migration
func RenderMigration(migrationID string, schema string) (string, error)
```

`Migrations` 每次返回防御性复制，调用方修改 slice 或元素不得影响后续结果。清单按六位数字前缀 ID 严格递增；首个迁移固定为 `000001_eventing_store`、最低框架版本为 `v0.1.0`。`Migration` 不暴露 SQL 模板，`RenderMigration` 只按 ID 读取包内资产；未知 ID 返回 `ErrMigrationNotFound`，模板 checksum、UTF-8 或 token 不一致返回 `ErrMigrationIntegrity`，任何错误都不返回部分 SQL。

框架不提供 `Up`、`Down`、`Migrate` 或任何执行入口。启动时不执行 DDL，运行账号不需要迁移权限。适配器可以提供只读的 schema 兼容性检查，在检测到数据库 schema 版本低于最低要求时返回诊断错误并拒绝启动，但不得尝试修复。

schema 只接受 1 至 63 字节、匹配 `^[a-z_][a-z0-9_]{0,62}$`、不以 `pg_` 开头且不为 `public`/`information_schema` 的小写 ASCII 名称。唯一模板 token 为 `{{IDP_BENTO_SCHEMA}}`，渲染时始终替换为正确引用的单一 identifier，不依赖 `search_path`。checksum 对 UTF-8、换行和文件末尾规范化后的模板计算，格式为 `sha256:` 加 64 位小写十六进制，不对客户 schema 的渲染结果计算。完整算法以 `R-04-D` 决策简报为准。

## 可观测性绑定

适配器通过构造选项接收 `TracerProvider`、`MeterProvider` 和 `*slog.Logger`，未提供时使用 no-op。instrumentation scope 名称为包导入路径。

metric 至少覆盖：事务开始/提交/回滚计数与耗时、Outbox 待发布数量与最老记录延迟、分区队首等待时长、领取数量与耗时、发布结果分类计数、`attempt` 与 `failures` 的分别统计、Inbox 接收与重复抑制计数、有界等待超时计数。

`partition_key`、`event_id`、`source`、`scope`、`subject`、原始 `failure_code` 与 SQL 文本和参数均不得作为 metric label。FailureCode 的语法限制不能保证低基数；指标只能使用框架固定枚举或显式 allowlist 映射后的 `failure_class`。

## 明确拒绝的能力

- 通用行映射、struct 标签扫描、查询构造器或任何形式的 ORM。
- 迁移执行、迁移历史表、启动时自动 DDL 或 schema 自动修复。
- 全局连接池、全局 Transactor 单例或从环境变量读取配置。
- 保存点、`RequiresNew`、跨库两阶段提交与自动事务重试。
- 消费者业务表、业务索引、租户模型或领域数据迁移。
- 跳过 Outbox 队首事件的运维接口；跳过会破坏分区顺序保证。

## 最小测试合同

- 写事务参与者在未绑定事务的 context 上返回 `ErrTransactionRequired`，不写入数据库；ReadExecutor 按显式合同允许连接池读取，返回类型及动态包装器都不暴露 `Exec`。
- Querier/Executor 均由私有包装器实现；类型断言不能重新取得原始 `pgx.Tx`、`*pgxpool.Pool`、`Commit` 或 `Rollback` 能力。
- DB/Store Option 不能由消费者自定义；重复 Option 安全失败，`Schema()` 在 DB 生命周期内保持稳定。
- 外层事务显式使用 `READ COMMITTED`，不受数据库默认隔离级别漂移影响。
- 另一个 Transactor 的事务 context 上返回 `application.ErrTransactionConflict`。
- 领取只产出每个分区的队首且已到期的事件；被有效租约占用的分区不产出后继事件。
- 空分区并发 Enqueue 通过事务级 advisory lock 串行；即使后到事务的 RecordedAt 更早，也不能取得更小的 `enqueued_seq` 或替换先到队首。
- 队首进入终态后同分区下一个事件可被领取；`outbox_partition` 指针正确推进，最后一个事件终结后分区行被删除。
- 租约过期后可重新领取，`attempt` 递增而 `failures` 不变。
- 并发领取不产生重复领取，也不使同一分区同时持有多个有效租约；该用例必须以多轮并发形式执行，单次通过不构成证据。
- 两个 Claim worker 可以通过 `SKIP LOCKED` 同时领取不同分区；一个 worker 持有分区行时，另一个不能领取该分区。
- 删除 `UPDATE` 内的可领取谓词复检后，并发用例必须失败；该负向用例防止复检在后续重构中被移除。
- `RecordFailure` 递增 `failures`；预算未耗尽时同步分区 `claimable_at`，达到 `MaxFailures` 时在同一事务内进入 `ABANDONED` 并保留本次失败分类。
- 达到 `OutboxClaim.MaxAttempts` 后不再产生 Delivery，并携带 `FailureClaimLimitExceeded` 原子进入 `ABANDONED`；模拟每次在租约到期前崩溃的 Publisher 时分区最终被释放。
- `OutboxClaim.MaxAttempts` 为零时返回 `eventing.ErrInvalidRequest`；达到上限的队首不再产生 Delivery，而是在 Claim 事务内进入 `ABANDONED` 并推进分区。
- Claim Limit 超过 `eventing.MaxOutboxClaimLimit` 时返回 `eventing.ErrInvalidRequest`；修复/清理路径不会使单次事务检查超过 Limit 个分区。
- 最后队首终结与新 Enqueue 并发时，提交后分区指针指向新事件，不得留下活跃事件但缺少分区行。
- 消费者合同验证 `LeaseFor > PublisherTimeout`；PostgreSQL Store 构造不接收也不推断 Publisher 超时。
- 空库安装后的命名约束、复合外键和部分索引与规范 DDL 一致；分区指针不能引用其他分区或不存在的 Outbox 行。
- Inbox 在同一事务内从 `PROCESSING` 进入 `PROCESSED`/`REJECTED` 可以提交；遗漏终结或把终态改回 `PROCESSING` 时，延迟约束触发器以 `23514` 阻止提交。
- Inbox 有界等待在配置时长内返回 `eventing.ErrInboxBusy`，不等待持有者事务结束；错误必须触发外层事务回滚。
- Inbox 成功占用后 `lock_timeout` 已恢复默认值，业务语句不受短超时影响。
- 迁移可安装到空库并从上一受支持版本升级；清单 `ID` 与 `Checksum` 在版本间不变化。
- schema 名称边界、唯一 token、模板 checksum 与未知迁移 ID 的错误合同固定；`Migrations` 返回防御性复制且不暴露 SQL 模板。
- 启动路径不执行任何 DDL；`CheckSchema` 在只读一致性快照中验证 UTF8 编码和必需对象子集，缺失或不兼容时返回可识别错误，兼容扩展对象不因额外存在而失败。
- 未配置 OpenTelemetry 时全部行为与错误语义不变；默认遥测不包含分区键、事件标识、作用域或 SQL 参数。

## `I-04` 实现验收门禁

`R-04` 设计已经确认并可直接编码。真实实现完成前仍必须验证：实际迁移 manifest/checksum、空库安装和适用升级路径；`Migrations`/`RenderMigration`；`CheckSchema` catalog 负向变体；Store 的错误映射、影响行数、20 轮以上竞争和谓词删除 mutation tests；以及 PostgreSQL 16 最新稳定补丁的完整 CI 证据。

2026-07-30 的真实实现已经把该矩阵固化为仓库测试：

- `postgres/integration_test.go` 使用真实迁移和 `CheckSchema` 驱动公共 Outbox/Inbox 合同运行器，并验证 PostgreSQL major、UTF8 与 READ COMMITTED 基线。
- `postgres/i04_test.go` 覆盖严格事务参与、DDL/SQLSTATE 负向约束、兼容扩展与 schema 漂移、20 轮同分区 Enqueue、20 轮双 Claim、20 轮 Finalize/Enqueue 竞争、旧租约三终结拒绝、错误指针修复、Inbox `lock_timeout` 恢复和 10 万分区领取计划。
- `postgres/outbox/sql_test.go` 对 Claim 与三个 Finalizer 的状态、时间、token、MaxAttempts 和队首关键谓词设置删除守卫；删除任一确认谓词都会使测试失败。
- 首版没有上一 PostgreSQL 迁移版本，升级路径记录为 `not applicable`；从下一迁移开始必须增加上一受支持版本升级测试。

该仓库矩阵已在 `postgres:16.12-alpine` 上通过。尚未绑定的 `B-05` 使受保护 CI 日志与候选证明无法形成，因此 PostgreSQL 本地实现可以描述为完成，但不可描述为已经通过发布级 CI 或稳定发布门禁。

## 实现与外部绑定

- `B-02` 已绑定：`pgx/v5 v5.10.0` 与 OpenTelemetry API `v1.44.0` 已作为直接依赖进入 `go.mod`/`go.sum`。
- `B-03` 已绑定：`v0.1` 初始支持 PostgreSQL major 16，当前设计与本地实现证据均为 16.12；增加其他 major 前必须重跑完整矩阵。
- `B-05` 配置已确认、部署未验证：GitHub Actions、跨仓触发、三年 Object Lock Compliance、OIDC、KMS/DSSE 与 evidence manifest 已固定，但真实 workflow、App、runner/container、S3/KMS/OIDC 标识和演练仍未绑定，继续阻断发布级 `I-04` 证据与 RC。

## 下一步

`R-04` 本地实现和 PostgreSQL 16.12 仓库矩阵已经完成。下一步是绑定 `B-05` 并在受保护 CI 重跑同一测试，然后由 TMS、Parcel 对同一不可变候选执行各自合同。
