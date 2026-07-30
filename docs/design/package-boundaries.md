# 包边界设计

状态：已确认

## 目标

为 `idp-bento-go` 建立最小、可验证的包边界，使核心技术契约能够被 TMS 与 Parcel 复用，同时避免基础设施依赖和产品业务语义进入核心。

## 已确认原则

- 核心包只依赖 Go 标准库。
- 基础设施适配器位于独立包中，核心不得反向导入适配器。
- `v0.1` 只公开领域原语、应用契约、事件契约和测试工具所需的最小 API。
- PostgreSQL、Outbox、Inbox 等适配器在 `v0.x` 阶段属于实验性 API。
- 单一消费者专属能力留在消费者仓库或框架 `internal`。
- 框架不得包含 Gatekeeper、TMS 或 Parcel 的业务类型和适配器。
- 框架提供窄化的聚合 Repository Port，但不提供通用 Repository 实现、查询 DSL 或通用 CRUD。

## 已确认包结构

```text
标准库核心
  domain
  repository
  eventing
  application
  testkit

可选实验性适配器
  postgres
  postgres/outbox
  postgres/inbox

框架内部
  internal/*
```

`v0.1` 不公开 `contracts`、`testing`、`adapter`、`ports`、`common`、`observability` 或 `otelkit` 等宽泛或重复标准契约的包名。HTTP 和具体消息产品适配器尚无双消费者证据，暂不进入公共包结构。

### 公共包职责

| 包 | 职责 | 允许依赖 |
| --- | --- | --- |
| `domain` | `EventType`、`EventVersion`、进程内领域事件与零值可用的 `EventBuffer`；不提供业务模型基类 | Go 标准库 |
| `repository` | `Revision`、`Loaded`、小能力 Repository Port 和稳定错误分类；作用域键由消费者拥有 | Go 标准库 |
| `eventing` | 版本化 JSON 信封、Publisher、Outbox 租约/终态、Inbox 事务幂等和稳定错误分类 | Go 标准库 |
| `application` | 泛型 Handler、Clock、IDGenerator、Transactor 和显式应用编排契约；不提供 Bus 或 Repository Registry | `domain`、`repository`、`eventing`、Go 标准库 |
| `testkit` | 不可变固定时钟、顺序 ID、记录式 Publisher、合法信封和真实端口合同运行器 | 全部公共核心包、Go 标准库 |

### 实验性适配器职责

| 包 | 职责 | 状态 |
| --- | --- | --- |
| `postgres` | 基于 `pgx/v5` 的 Transactor、`Revision` 与错误分类辅助、迁移清单访问；不提供业务 Repository 或通用行映射 | `EXPERIMENTAL` |
| `postgres/outbox` | PostgreSQL Outbox 与分区状态表的存储、领取、租约和终态持久化实现 | `EXPERIMENTAL` |
| `postgres/inbox` | PostgreSQL Inbox、有界等待幂等占用和消费结果持久化实现 | `EXPERIMENTAL` |

包路径不增加 `adapter/` 或 `pgx/` 层级：`postgres` 已经明确基础设施属性，额外目录只增加导入长度而不形成新的所有权边界。`pgx/v5` 是该适配器的实现依赖，不进入核心端口名称。

## PostgreSQL 迁移边界

### 框架拥有

- 专用 schema（默认 `bento`）下 `outbox`、`outbox_partition`、`inbox` 三张技术表及其索引和约束的版本化 SQL 资产。
- `postgres/outbox`、`postgres/inbox` 及其他框架技术适配器所需表、索引和约束的版本化 SQL 资产。
- 每项迁移的稳定标识、内容校验和、依赖顺序、最低兼容框架版本和升级说明。
- 空库安装、重复检测以及从上一受支持框架版本升级的集成测试。
- 只读迁移清单/资产访问契约，使消费者迁移命令可以组合执行，而无需复制框架 SQL。
- 清单只公开 ID、规范模板 checksum 和最低框架版本；SQL 模板保持包内，通过唯一 schema token 按 ID 受控渲染。
- 已发布迁移只增不改；修正错误必须新增迁移，不能重写历史。

### 消费者拥有

- TMS、Parcel 各自的迁移工具、迁移命令、历史表、数据库连接、凭据和执行权限。
- 业务表、业务索引、种子数据、领域数据修复与全部消费者专属迁移。
- 框架技术迁移和消费者业务迁移之间的执行顺序、部署门禁、维护窗口、备份和回退。
- 私有化环境中的审批、审计以及数据库管理员运行手册。

### 执行规则

- 框架库和应用服务不得在启动时自动执行迁移，也不得要求生产运行账号持有 DDL 权限。
- 消费者使用独立迁移作业或管理命令，在部署新二进制前显式应用所需迁移。
- 迁移资产随精确框架版本锁定；消费者不得复制、编辑或重新编号后形成私有分叉。
- 升级优先采用 expand/contract：先增加向后兼容结构，完成新旧二进制过渡后，再由消费者显式安排破坏性收缩。
- 自动向下迁移不是框架承诺。涉及数据丢失或不可逆转换时，回退依赖兼容二进制、前向修复、备份与恢复方案。
- 框架 release notes 必须声明最低数据库 schema 版本；消费者启动时可以执行只读兼容性检查，但不得自动修复。
- schema 检查只验证当前二进制依赖的 UTF8 编码与必需对象子集，允许经过兼容性测试的扩展对象，不读取或接管消费者迁移历史。

## 依赖规则

- `domain`、`repository`、`eventing` 互不依赖并且只依赖 Go 标准库。
- `application` 可以依赖 `domain`、`repository` 和 `eventing`，不得依赖任何适配器。
- `testkit` 可以依赖全部公共核心；生产包和消费者生产代码不得依赖 `testkit`。
- `postgres` 可以实现核心端口并依赖 `pgx/v5`，但核心不得反向导入 `postgres`。
- `postgres/outbox` 与 `postgres/inbox` 可以依赖父级 `postgres` 和 `eventing`；两者不得互相导入。
- 实验性 PostgreSQL/Outbox/Inbox 适配器可以依赖锁定版本的 OpenTelemetry API 和语义约定，但不得依赖具体 SDK、Exporter、Collector 或消费者观测包。
- 未来新增的适配器不得通过另一个适配器的内部实现形成隐式强依赖。
- `internal` 只支持框架自身实现，消费者不得通过复制或路径技巧访问。

CI 必须根据 `go list -deps` 或等价静态检查验证上述导入规则，并阻止消费者产品包、数据库模型或业务词汇进入框架。

## Domain 边界

- `domain` 不定义 `AggregateRoot`、`Entity`、`ValueObject` 基类、标记接口或通用领域错误。
- 消费者分别拥有聚合标识、租户、状态、生命周期、业务不变量和事件具体载荷。
- `Event` 只提供事件类型、结构版本和发生时间；事件 ID、租户、聚合引用、Trace Context 和序列化属于应用映射与 `eventing`。
- `EventBuffer` 的零值可用，按顺序记录事件，返回防御性 slice 副本，并只在成功事务后 `Acknowledge`。
- `EventBuffer` 不并发安全，不自动发布、序列化或写 Outbox；聚合实例在单个命令事务中只由一个 goroutine 修改。

## Application 与事务边界

- Command/Query 是消费者请求结构，框架不提供 marker 接口；统一使用 `Handler[Request, Response]` 与 `HandlerFunc`。
- Clock、IDGenerator 在应用层使用，Domain 不导入 `application`。
- `Transactor` 使用派生 context 传播不透明事务句柄；该 context 只在同步回调范围内有效。
- 事务句柄可以隐式传播是因为缺失时立即失败；租户、主体和授权范围因失败静默且后果为跨租户泄露，必须显式传参。
- 写事务参与者采用严格模式：context 中没有事务句柄时返回基础设施错误，不静默回退连接池；读取能力通过显式区分的 ReadExecutor 与 RequireExecutor 选择是否允许事务外连接池访问。
- 同一 Transactor 嵌套使用 Required 传播并共享事务；内层错误或 panic 设置 rollback-only。
- 不同 Transactor 不能在同一框架事务 context 中隐式开启第二个事务，必须返回 `ErrTransactionConflict`。
- 回调失败、取消和 panic 回滚；panic 原样重新抛出。提交结果不确定返回 `ErrCommitUncertain`。
- 框架不提供 RequiresNew、保存点、自动重试、分布式事务、Command Bus、Query Bus 或通用 Unit of Work Repository map。
- 领域事件只有在业务写入和适用 Outbox 成功提交后才 `Acknowledge`。

## Eventing 边界

- `eventing.Envelope` 是非泛型、已序列化 JSON 载荷的技术信封；消费者拥有具体事件类型、Payload schema 和订阅关系。
- 信封显式携带 `Source`、`Type`、`Version`、`Scope`、`Subject`、`PartitionKey`、发生/记录时间、关联/因果标识和可选 Trace Context；不提供开放式 Metadata map。
- `Scope` 对框架不透明，由消费者从权威业务上下文显式填入；不得从 `context.Context`、全局变量或部署配置补全。
- `Enqueue` 与 `Publish` 在边界内强制校验信封，无效信封不能进入持久化或传输；校验规则集属于信封兼容性契约。
- Publisher 每次发布一个信封，明确拒绝包装 `ErrPublishRejected`，结果不确定包装 `ErrPublishUncertain`；核心不自动重试。
- Outbox 以 `Source + EventID` 去重，通过租约与条件终结提供 At-Least-Once 交付；同一 `PartitionKey` 只领取最早非终态事件，不承诺跨分区全局顺序。
- Outbox 区分 `Attempt`（领取次数）与 `Failures`（通过 `RecordFailure` 显式记录的可重试发布调用失败，含结果不确定）；只有后者消耗失败预算，租约过期不被误判为发布失败。
- 分区串行的代价由消费者承担：分区键粒度决定阻塞爆炸半径，框架不提供跳过队首事件的能力。
- Inbox 以 `Consumer + Source + EventID` 为幂等键；占用、业务副作用和处理终态必须与消费者本地事务原子提交。
- Inbox 占用为有界等待语义；已被未提交事务占用时在短超时内返回 `ErrInboxBusy` error，强制事务回滚且不 ACK，再由消息适配器重投；不做无界锁等待。
- Exactly-Once、Broker Client、路由注册表、Schema Registry、通用死信重放和 Saga 不进入核心公共 API。

## 可观测性依赖规则

- 核心包只传播 `context.Context`，不导入 OpenTelemetry 类型，也不从 context 推断租户或授权范围。
- 实验性适配器通过构造选项接收 `TracerProvider`、`MeterProvider` 和 `*slog.Logger`，不依赖隐式全局初始化。
- 未配置可观测性时必须使用 no-op 行为，不能改变事务、Outbox、Inbox 或发布结果。
- 事件信封可以包含 W3C `traceparent`、`tracestate` 字符串字段；默认不传播 Baggage。
- SDK、Exporter、Collector、采样、资源属性、端点、TLS 和凭据全部由消费者装配。
- `idp-obs/sdk-go` 不是框架依赖；若消费者使用，它必须以标准 OpenTelemetry provider/exporter 接入。
- 默认遥测不得包含租户 ID、主体 ID、聚合 ID、事件载荷、授权证据、完整 SQL、SQL 参数、令牌或凭据。
- 遥测失败不参与业务事务结果，也不得触发消息副作用重试。

## 公共 API 进入条件

一个符号进入公共 API 前必须满足：

1. 有明确的 TMS 或 Parcel 真实用例。
2. 不包含产品业务语言、状态或数据库结构。
3. 具有行为说明、错误语义和并发语义。
4. 具有框架单元测试和消费者合同测试。
5. 能说明为何不能留在消费者仓库或 `internal`。

只有两个消费者都验证且兼容性语义稳定后，公共 API 才能进入 `v1` 稳定清单。

## Testkit 边界

`testkit` 是测试专用但公开、受 SemVer 治理的核心包。首版只包含：

- 不可变且拒绝零时间的 `FixedClock`。
- 并发安全、按输入顺序消费、context 取消不消耗元素、耗尽返回稳定错误的 `SequenceIDGenerator`。
- 在记录前校验信封、按调用序号注入 error、深拷贝输入与查询结果的 `RecordingPublisher`。
- 每次返回独立合法 Payload 的 `ValidEnvelope` 协议夹具。
- 由消费者或适配器工厂创建隔离真实端口的 Repository、Outbox、Inbox 合同运行器。

合同运行器只通过 `application`、`repository` 和 `eventing` 的公开接口观察行为。它们不得探测 SQL、数据库表或未导出状态，也不得并行运行调用方未声明可隔离的 scenario。工厂在每个子测试创建新 scenario，并通过 `testing.T.Cleanup` 释放自己拥有的资源。

框架明确不提供通用内存 Repository、Fake Transactor、内存 Outbox/Inbox、测试 Service Locator 或消费者业务夹具。此类实现会猜测聚合身份、事务隔离和作用域语义，并让测试绕过真实 SQL 和约束。PostgreSQL 容器、迁移执行、schema 生命周期、SQLSTATE、锁竞争和执行计划夹具保持在 `postgres` 适配器的包内测试或 `internal`，公共 `testkit` 不导入 `pgx/v5`。

框架 CI 和两个消费者的 `bento-contract` 必须对不含测试文件的生产依赖图执行负向门禁：除 `testkit` 自身外，任何生产包的依赖闭包出现框架 `/testkit` 都失败；`*_test.go` 和专用合同测试包可以导入。框架侧已由 `go run ./internal/cmd/dependencycheck` 实现，并用故意导入 `testkit` 的负向夹具证明会失败；同一门禁还阻止核心反向依赖 PostgreSQL、`testkit` 依赖基础设施以及 Outbox/Inbox 相互依赖。精确公共签名、行为和运行器矩阵以 [`testkit` 公共合同决策简报](./testkit-contract.md) 为准。

## 公共兼容性边界

- `domain`、`repository`、`eventing`、`application`、`testkit` 标记为稳定核心；三个 PostgreSQL 包在 `v0.x` 标记为实验性，但仍进入 API diff。
- `api/packages.json` 显式列出允许的公共包与稳定性；逐包 apidiff snapshot 覆盖所有导出声明，未登记的公共包或符号使 CI 失败。
- 业务无关编译夹具使用首版全部公共符号；Envelope 每个 SpecVersion 另有确定性行为清单保护常量、wire、校验接受集合和错误分类。
- 当前 snapshot 必须可重复生成，并与上一发布版本单独比较；修改 snapshot 不能消除破坏性变化报告。
- `v0.x` patch 禁止已知破坏，minor 只允许带变化记录、迁移说明和双消费者结果的受控破坏；同一 SpecVersion 的规则收紧必须改用新版本。

完整公共符号 allowlist、制品布局和版本门禁见[公共 API 与行为兼容性决策简报](./public-api-compatibility-contract.md)。

## Repository 边界

`repository` 提供命令侧聚合持久化的最小能力接口，并拆分为可组合的小能力：`Loader`、`Inserter`、`VersionedUpdater` 等。消费者按聚合需要组合这些能力。

消费者负责：

- 定义 `AwardRepository`、`ParcelRepository` 等语义化 Repository Port。
- 定义租户、聚合标识和其他权威查找键。
- 编写显式 SQL、字段投影、锁和版本条件。
- 定义查询、列表、搜索、分页、聚合和报表端口。
- 在 Unit of Work 中保存业务结果和适用 Outbox 记录。

框架不得提供：

- `List`、`FindBy`、`Where`、`Page`、`DeleteBy` 或任意字段更新。
- 自动租户过滤、自动领域事件发布或跨聚合事务编排。
- ORM Model、反射映射、查询 DSL 或通用数据库 Repository。

通用 Port 不得只使用裸聚合 ID 推定租户范围。消费者必须使用包含权威租户和资源身份的键或等价显式约束，避免不同租户同标识对象串权。

### Repository 键契约

- `Loader` 的键类型由消费者拥有；对于租户或其他权威范围内的聚合，该键必须同时包含范围标识和聚合标识。
- `VersionedUpdater` 必须显式接收同类作用域键和预期版本，持久化适配器使用两者形成条件更新，不得先按裸 ID 查找后在内存中过滤。
- `Inserter` 接收已经包含完整作用域身份的聚合，不额外重复传入一份键，避免键与聚合身份不一致。消费者负责在进入 Repository 前构造并校验该身份。
- `context.Context` 只用于取消、截止时间、跟踪和请求生命周期传播。Repository 不得从其中隐式读取租户、组织或授权范围来补全查询条件。
- 对错误作用域、其他租户中的同 ID 或不存在对象，加载操作统一返回未找到语义，不暴露跨租户对象是否存在。
- 框架不提供通用 `ScopedKey`。TMS 与 Parcel 分别拥有自己的范围类型、聚合 ID、语义化键类型、构造和有效性校验。

`repository` 首版接口形态固定为：

```go
type Revision int64

type Loaded[A any] struct {
	Aggregate A
	Revision  Revision
}

type Loader[K, A any] interface {
	Load(ctx context.Context, key K) (Loaded[A], error)
}

type Inserter[A any] interface {
	Insert(ctx context.Context, aggregate A) (Revision, error)
}

type VersionedUpdater[K, A any] interface {
	Update(ctx context.Context, key K, aggregate A, expectedRevision Revision) (Revision, error)
}
```

`Revision` 是聚合身份范围内的单调技术令牌，不是全局序列。`Loaded` 将聚合与修订号一起返回，使框架不必强迫所有消费者的领域聚合实现共享接口；消费者可以把修订号保存在应用快照、命令上下文或自己的持久化状态中。适配器应使用单条条件 SQL 完成检查、递增和返回，不能先读取再更新。

三个接口都只接收 `context.Context` 而不接收事务参数。事务句柄由 `application.Transactor` 绑定到派生 context，具体 PostgreSQL 适配器通过窄化 Querier/Executor 解析；核心 `repository` 包不定义事务抽象，也不导入数据库驱动。允许事务外读取的路径只能取得不含 `Exec` 的 Querier，严格写路径才能取得含 `Exec` 的 Executor，使事务外写入在编译期即不可表达。

`Inserter`、`VersionedUpdater` 和其他写入实现必须使用 RequireExecutor，缺少事务立即失败。`Loader` 实现必须明确选择 RequireExecutor 或 ReadExecutor：前者只允许事务内加载；后者在匹配事务存在时加入事务、没有事务时使用构造时注入的连接池、存在其他 Transactor 时返回冲突。消费者的语义化 Repository 和构造函数必须记录该选择，禁止在同一个未命名路径中隐式改变一致性语义。

消费者的语义化 Repository 可以组合这些小接口，但不能只暴露一个无边界的 `Repository[A, ID]`。

### Repository 错误契约

`repository` 只定义以下三个稳定的技术分类：

```go
var (
	ErrNotFound      = errors.New("repository: not found")
	ErrAlreadyExists = errors.New("repository: already exists")
	ErrConflict      = errors.New("repository: conflict")
)
```

- `Loader` 对不存在、错误作用域以及其他租户中的同 ID 统一返回包装了 `ErrNotFound` 的错误。
- `Inserter` 只有在同一作用域内的同一聚合身份已经存在时返回包装了 `ErrAlreadyExists` 的错误。其他业务唯一性冲突由消费者 Repository 映射为消费者拥有的语义化错误。
- `VersionedUpdater` 使用作用域键和预期版本执行单条条件更新；影响 0 行时统一返回包装了 `ErrConflict` 的错误，不通过额外查询区分“不存在”“错误租户”或“版本过期”。
- 调用方使用 `errors.Is` 判断稳定分类，不比较错误文本，不依赖具体数据库错误码。
- 适配器可以使用 `%w` 添加不含敏感标识的操作上下文，但不能改变稳定分类。
- `context.Canceled` 和 `context.DeadlineExceeded` 必须保持可判定；连接、事务、扫描、序列化等其他失败保留为基础设施错误，不得错误归类为 `NotFound` 或 `Conflict`。
- 框架不定义领域不变量错误、业务唯一性错误或 HTTP/gRPC 状态码；这些语义属于消费者应用层和传输适配器。

### Revision 合同

- `Revision` 只能取未持久化的 `0` 或持久化后的正数；适配器不得接受负数。
- `Insert` 成功返回 `1`；重复聚合身份返回 `ErrAlreadyExists`。
- `Update` 必须使用调用方提供的 `expectedRevision`，成功返回 `expectedRevision + 1`；影响 0 行返回 `ErrConflict`。
- 任何事件版本、策略版本、安全版本或 `resourceAuthorizationVersion` 必须由消费者单独建模，不能复用 `Revision`。
- 冲突后框架不隐式重试；消费者根据命令幂等性和业务规则决定是否重新加载、重新授权和重试。

该划分让应用层可以稳定处理技术结果，同时避免 Repository 通过附加存在性查询引入竞态、性能开销或跨租户信息泄露。

## 风险与缓解

- **包数量过多**：按稳定职责分包，不为单个类型创建包。
- **循环依赖**：CI 执行导入图检查，核心到适配器只允许单向依赖反转。
- **实验性适配器被当作稳定 API**：文档和发布说明明确标记实验状态，合同测试记录消费者使用范围。
- **技术信封变成共享业务事件**：`eventing` 只定义事件元数据和版本机制，业务事件类型由消费者拥有。
- **测试工具泄漏到生产**：CI 阻止生产包依赖 `testkit`，最终消费者二进制不需要测试夹具。
- **内存假实现掩盖事务与 SQL 缺陷**：公共合同运行器由工厂驱动真实端口，PostgreSQL 专属竞争和约束继续由适配器集成测试验证。

## 下一步

`R-01` 至 `R-06` 设计合同和本地代码基线已经完成，三个 PostgreSQL 包已通过仓库内 `I-04`。下一步是受保护 CI、跨仓候选和双消费者合同，包依赖方向不得因此放宽。
