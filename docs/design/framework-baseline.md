# idp-bento-go 框架基线

状态：已确认

## 目标

定义 IDP-TMS 与 IDP-Parcel 可以共同复用的最小、可持续 Go DDD+EDA 框架，同时不共享业务所有权、数据、运行时或发布边界。

## 非目标

- 本文档不定义 TMS 或 Parcel 的领域语言、聚合生命周期或业务规则。
- 本文档不让任何消费者成为框架宿主或权威来源。
- 正式 module path、实际 Git 仓库和 `v0.1` vanity 解析方式已固定；本文档不选择 DNS/HTTPS 托管平台、迁移工具或消息中间件。
- 最终决策简报确认前，本文档不授权开始实现。

## 决策登记

| ID | 决定 | 状态 |
| --- | --- | --- |
| `F-01` | 将 `idp-bento-go` 建设为独立、业务无关的共享技术框架 | `CONFIRMED` |
| `F-02` | IDP-TMS 与 IDP-Parcel 是首批两个消费者，并保持独立业务、数据、运行和发布边界 | `CONFIRMED` |
| `F-04` | 使用独立 Git、Go module、CI 和发布周期 | `CONFIRMED` |
| `F-05` | 从 `v0.x` 开始采用 SemVer、消费者精确版本锁定和双消费者合同测试 | `CONFIRMED` |
| `F-06` | 创建代码基线时采用 Go 官方最新稳定版并锁定精确补丁版本 | `CONFIRMED` |
| `F-07` | 分离只依赖 Go 标准库的核心与可选基础设施适配器 | `CONFIRMED` |
| `F-08` | `v0.1` 采用最小公共 API，基础设施适配器保持实验性，单消费者能力保持 internal | `CONFIRMED` |
| `F-09` | 提供窄化的通用聚合 Repository Port，不提供通用 Repository 实现或查询 CRUD | `CONFIRMED` |
| `F-10` | Repository Port 拆分为可组合的 `Loader`、`Inserter`、`VersionedUpdater` 等小能力接口 | `CONFIRMED` |
| `F-11` | 作用域聚合的加载和条件更新必须显式接收消费者拥有的强类型复合键，不使用裸聚合 ID，也不从 `context.Context` 隐式推断租户或权威范围 | `CONFIRMED` |
| `F-12` | Repository Port 只稳定分类 `NotFound`、`AlreadyExists` 和 `Conflict` 三类技术错误，并通过 Go `errors.Is` 判定；领域错误和传输层映射由消费者拥有 | `CONFIRMED` |
| `F-13` | Repository Port 使用业务无关的 `Revision int64` 作为聚合级乐观并发令牌；持久化从 1 开始，更新原子递增并返回新值，事件/安全/策略版本不与其混用 | `CONFIRMED` |
| `F-14` | `v0.1` 公共包固定为 `domain`、`application`、`repository`、`eventing`、`testkit`；实验性适配器使用 `postgres`、`postgres/outbox`、`postgres/inbox`，并通过 CI 强制单向依赖 | `CONFIRMED` |
| `F-15` | 框架拥有其 PostgreSQL 技术表的版本化迁移资产和兼容性测试；TMS、Parcel 分别拥有迁移工具、执行、权限、顺序、历史与业务迁移，禁止应用启动时自动迁移 | `CONFIRMED` |
| `F-16` | 框架 CI 只运行框架自测；TMS、Parcel CI 各自运行最小 `bento-contract`，框架稳定发布要求两边对同一不可变候选返回通过结果，但不接管消费者完整 CI 或业务源码 | `CONFIRMED` |
| `F-17` | 框架由不少于 3 人的独立维护组治理；高风险公共变更需要双维护者和双消费者评审，稳定版本仅由受保护 CI 发布，任何产品或个人不得单方面发布 | `CONFIRMED` |
| `F-18` | `v0.x` 当前 minor 全量支持、前一 minor 关键修复 90 天；`v1` 被替代 minor 关键修复 6 个月，`v2` 发布后最新 `v1` minor 关键修复 12 个月 | `CONFIRMED` |
| `F-19` | `v0.1` 不定义自有可观测性 Port 或公共包；实验性 PostgreSQL/Outbox/Inbox 适配器显式支持 OpenTelemetry API 与 `log/slog`，SDK、Exporter 和环境配置由消费者拥有 | `CONFIRMED` |
| `F-20` | `domain` 不提供 Aggregate/Entity/Value Object 基类或通用领域错误，只提供最小领域事件接口与零值可用的 `EventBuffer`；全部业务模型和不变量由消费者拥有 | `CONFIRMED` |
| `F-21` | `application` 只提供泛型 Handler、Clock、IDGenerator 和 `Transactor`；事务采用 Required 传播及 rollback-only 语义，不提供 Bus、Repository Registry、RequiresNew、保存点或自动重试 | `CONFIRMED` |
| `F-22` | `eventing` 使用显式版本化 JSON 信封、单条 Publisher、按分区有序的 At-Least-Once Outbox 和事务性 Inbox；幂等键为消费者 + 来源 + 事件 ID，不提供 Exactly-Once、开放元数据或通用消息路由 | `CONFIRMED` |
| `F-23` | 事务句柄通过派生 context 隐式传播，业务作用域与授权身份必须显式传参；写事务参与者缺少事务时失败，读取实现显式选择允许连接池的 ReadExecutor 或严格事务 RequireExecutor | `CONFIRMED` |
| `F-24` | `Enqueue` 与 `Publish` 强制校验信封，无效信封不能进入持久化或传输；信封校验规则集属于兼容性契约，收紧规则只能通过新 `SpecVersion` 引入 | `CONFIRMED` |
| `F-25` | Outbox 区分 `Attempt` 与 `Failures` 两个计数器；只有通过 `RecordFailure` 显式记录的可重试发布调用失败（含结果不确定）消耗失败预算，租约过期不消耗预算，领取上限仅作为防崩溃循环兜底 | `CONFIRMED` |
| `F-26` | Inbox 占用采用有界等待语义；锁等待超时返回 `ErrInboxBusy` error 并强制事务回滚，由消息适配器重投，不做无界锁等待 | `CONFIRMED` |
| `F-27` | PostgreSQL 技术表位于专用 schema，默认 `bento` 且可配置；表固定为 `outbox`、`outbox_partition`、`inbox` | `CONFIRMED` |
| `F-28` | 事务句柄使用 `postgres` 包级私有 context key 传播，绑定包含 owner 与 `pgx.Tx`；消费者通过所属 DB 的窄化 Querier/Executor 参与事务，只读路径不暴露 Exec，写路径不取得 Commit/Rollback 能力 | `CONFIRMED` |
| `F-29` | Outbox 使用 `outbox_partition` 分区状态表与 `claimable_at` 索引；完整并发 SQL 已在 PostgreSQL 16.12 通过正向、负向、20 轮竞争和执行计划设计验证 | `CONFIRMED` |
| `F-30` | 框架以 `embed.FS` 发布规范迁移模板；清单只返回防御性元数据副本，按 ID 使用唯一 schema token 受控渲染，checksum 对规范模板计算且发布后不可变化 | `CONFIRMED` |
| `F-31` | PostgreSQL DB/Store 只拥有事务绑定与原子持久化；消费者拥有发布进程生命周期、Publisher 超时、失败预算和退避，`MaxAttempts` 作为 Claim 显式参数由 Store 原子执行 | `CONFIRMED` |
| `F-32` | 三张 PostgreSQL 技术表使用命名 CHECK/唯一约束/索引；Outbox 分区指针通过 `(partition_key, head_enqueued_seq)` 复合外键绑定同分区事件，Inbox 通过延迟约束触发器禁止提交 `PROCESSING` | `CONFIRMED` |
| `F-33` | PostgreSQL 事务显式使用 READ COMMITTED；同分区 Enqueue 先取得事务级 advisory lock，所有实际行锁保持分区行到 Outbox 行；Claim 使用有界 Limit 与 `FOR UPDATE SKIP LOCKED` | `CONFIRMED` |
| `F-34` | `CheckSchema` 在只读 REPEATABLE READ 快照中验证 UTF8 编码和当前二进制依赖的必需表、列、约束、索引、函数与触发器子集；允许兼容扩展且不执行修复 | `CONFIRMED` |
| `F-35` | PostgreSQL 设计证据与实现验收分层：可复现数据库证据解除设计阻断；真实迁移、CheckSchema、Store 和多版本 CI 进入 `I-04`，阻断适配器完成与发布 | `CONFIRMED` |
| `F-36` | `testkit` 只公开不可变固定时钟、并发安全顺序 ID、深拷贝记录式 Publisher、合法信封和工厂驱动真实端口的 Repository/Outbox/Inbox 合同运行器；不提供通用内存 Repository/Store，生产包禁止反向依赖 | `CONFIRMED` |
| `F-37` | 公共兼容性采用逐包 apidiff snapshot、业务无关编译夹具、版本化行为清单和双消费者候选合同四层门禁；snapshot 更新不能替代与上一发布版本比较 | `CONFIRMED` |
| `F-38` | Envelope 校验按信封自身 SpecVersion 分派；`SpecVersionV1` 永久标识 `1.0`，`SpecVersion` 表示当前写入版本，新增版本期间继续支持仍有效的历史版本 | `CONFIRMED` |
| `F-39` | `v0.1` 不公开通用 `ScopedKey`；消费者拥有语义化强类型复合键、构造与有效性校验 | `CONFIRMED` |
| `F-40` | 正式 Go module path 固定为 `go.idp.xyz/idp-bento-go`；模块身份与实际私有 Git 地址解耦，Git 地址及 `go.idp.xyz` 的 vanity metadata 或私有 GOPROXY 路由在发布基础设施阶段绑定 | `CONFIRMED` |

`F-03` 曾记录本地开发目录，已撤销：本地路径是工作区事实而不是架构决定。编号不重复使用。

`F-40` 的外部 Git transport 已于 2026-07-30 最终绑定为 `https://github.com/idp-xyz/idp-bento-go`；仓库从个人命名空间迁移到公司 Organization 时 repository ID 保持不变，且该迁移不改变 ADR-0008 已接受的模块身份解耦决定。

`F-40` 的 `v0.1` 解析方式随后确认使用 HTTPS vanity metadata 直连该 GitHub 仓库，不引入私有 GOPROXY；精确响应与验收见 [`go.idp.xyz` module 解析合同](./module-resolution-contract.md)。DNS、TLS、`v0.1.0-rc.1` 的空缓存真实下载和两侧消费者合同执行已经验证，但两侧正式证明文件与受保护证据作业未完成，因此该外部门禁仍未整体解除。

## 已确认框架范围

首版框架只提供由两个消费者真实用例或明确公司级基础设施需求证明的业务无关能力：

- 最小领域事件原语；聚合根、实体、值对象、标识、领域错误和不变量由消费者定义，不提供框架基类。
- 消费者拥有 Command/Query；框架只提供泛型 Handler、Clock、IDGenerator 和事务端口。
- 时钟、ID 和确定性测试工具。
- 版本化事件信封、Publisher 端口、Outbox、Inbox 和幂等原语。
- Repository 键和事件信封中的显式技术作用域及传播规则；不提供全局租户上下文，也不从 `context.Context` 推断业务范围。
- PostgreSQL/pgx 事务辅助和可选消息基础设施包，不提供业务 Repository。
- 窄化的聚合加载、创建和按版本更新 Port；消费者在此基础上拥有并扩展语义化 Repository Port。
- 模块依赖检查和消费者合同测试支持。

## `v0.1` 公共 API 策略

- 公共核心包固定为 `domain`、`application`、`repository`、`eventing` 和 `testkit`；不使用职责含混的 `contracts`，也不使用容易与 Go 标准库混淆的 `testing`。
- PostgreSQL、Outbox、Inbox 等基础设施包可以交付给消费者使用，但在 `v0.x` 阶段明确标记为实验性，不承诺 `v1` 稳定签名。
- 只被 TMS 或 Parcel 单独需要的能力保留在消费者仓库或框架 `internal`，不得为追求表面完整性进入公共 API。
- 缓存、通用 Saga、业务工作流、Event Sourcing、低代码和代码生成不进入 `v0.1` 公共范围。
- 公共 API 必须同时具备文档、单元测试、兼容性测试和至少一个真实消费者调用；进入稳定 API 清单还需要两个消费者共同验证。
- 删除或修改公共 API 时，`v0.x` 也必须提供迁移说明和下游合同验证结果。

通用 Repository Port 只表达命令侧聚合持久化能力，不包含列表、搜索、分页、任意条件、字段更新、删除、自动租户过滤或事件发布。框架不提供反射、ORM、自动映射或通用 SQL 实现。TMS 与 Parcel 分别拥有查询模型、业务 Repository、租户和授权条件以及显式 SQL。

作用域聚合的读取和条件更新必须使用消费者定义的强类型复合键，例如消费者可以定义同时包含权威范围与聚合 ID 的语义化键。`context.Context` 只承载取消、截止时间、跟踪和请求生命周期信息，不得成为 Repository 隐式取得租户或授权范围的来源。创建操作由聚合自身携带完整且已校验的作用域身份，避免同时传入键和聚合产生双重事实来源；适配器必须把该身份显式写入 SQL。框架不提供通用 `ScopedKey`，因为它不能拥有消费者的键命名、构造规则、零值合法性或有效性校验。

Repository 的公共失败分类保持最小：加载不到目标时为 `NotFound`；插入已经存在的同一聚合身份时为 `AlreadyExists`；条件更新未匹配预期版本时为 `Conflict`。适配器可以附加安全的操作上下文，但必须支持 `errors.Is` 判定。取消和截止时间继续保留 `context.Canceled` 与 `context.DeadlineExceeded`；连接失败、序列化失败等基础设施错误不得伪装成上述分类。领域不变量、业务唯一性以及 HTTP、gRPC 或消息响应映射由 TMS 和 Parcel 自行定义。

Repository 使用聚合级 `Revision` 作为技术并发令牌，而不是业务版本、事件 schema 版本、策略版本、安全版本或资源授权版本。`Revision` 使用 Go `int64`，不得为负；数据库使用 PostgreSQL `BIGINT NOT NULL CHECK (revision >= 1)`。新记录的持久化修订号为 `1`，每次成功条件更新只递增 `1`，并通过 `RETURNING revision` 返回新值。`0` 仅可作为未持久化值，达到 `math.MaxInt64` 时拒绝继续递增并返回基础设施错误。框架不自动重试冲突；消费者必须重新加载并重新执行仍然有效的业务命令。

`domain` 只定义 `EventType`、`EventVersion`、`Event` 和 `EventBuffer`。领域事件具有稳定类型、从 1 开始的结构版本和非零发生时间；事件缓冲器按顺序记录并只在 Unit of Work 成功后确认清空。框架不自动发现、发布或持久化领域事件，也不要求消费者聚合实现统一接口。

`application` 使用一个泛型 `Handler[Request, Response]` 和 `HandlerFunc`，Command/Query 结构由消费者拥有。Clock 与 IDGenerator 由应用层调用并显式把值传入领域模型。Unit of Work 通过 `Transactor.WithinTransaction` 表达：同一 Transactor 嵌套加入现有事务，内层错误或 panic 将事务标记为 rollback-only；不同 Transactor 嵌套返回冲突。框架不提供 Command/Query Bus、Repository Registry、RequiresNew、保存点、分布式事务或自动重试。

`eventing` 使用非泛型、已序列化 JSON 载荷的版本化 Envelope。信封显式包含来源、事件类型/版本、作用域、主题、分区键、发生/记录时间、关联/因果标识和可选 W3C Trace Context，不提供任意 Metadata map。Publisher 每次发布一个信封；明确拒绝与结果不确定具有稳定错误分类。Outbox 以 `Source + EventID` 去重，按 PartitionKey 保持入队顺序，并通过租约、attempt、延迟重试和 `PUBLISHED`/`ABANDONED` 终态提供 At-Least-Once 交付。Inbox 以 `Consumer + Source + EventID` 幂等，必须与本地业务副作用处于同一事务；框架不声称 Exactly-Once，也不提供 Broker、路由、通用重放或 Saga。

公共包依赖保持单向：`domain`、`repository`、`eventing` 各自只依赖标准库；`application` 可以依赖这三个核心契约；`testkit` 可以依赖全部公共核心，但任何生产包不得反向依赖 `testkit`。实验性 `postgres` 适配器可以实现 `application`、`repository` 和 `eventing` 的端口，`postgres/outbox` 与 `postgres/inbox` 只允许依赖其所需的核心契约及父级 `postgres` 包。公共核心不得导入任何适配器。

`testkit` 的公共确定性替身固定为不可变 `FixedClock`、耗尽显式失败的并发安全 `SequenceIDGenerator`、按调用序号注入结果且深拷贝信封的 `RecordingPublisher`，以及一个合法信封起始夹具。Repository、Outbox 与 Inbox 合同通过工厂取得消费者或适配器创建的隔离真实端口；框架不提供通用内存 Repository、Fake Transactor、内存 Outbox/Inbox 或生产降级实现。PostgreSQL 容器、迁移、锁竞争与执行计划夹具留在适配器测试或 `internal`，公共 `testkit` 不导入 `pgx`。精确签名和行为以 [`testkit` 公共合同决策简报](./testkit-contract.md) 为准。

公共 API 兼容性使用固定 Go toolchain 与精确锁定的 apidiff tool dependency 生成逐包 snapshot，并结合业务无关编译夹具、每个 Envelope SpecVersion 的规范行为清单和不可变候选的双消费者合同。公共包和符号使用显式 allowlist；生成当前 snapshot 后仍必须与上一发布版本比较，不能通过同步修改实现与期望文件消除破坏证据。精确制品、版本规则和失败处理以[公共 API 与行为兼容性决策简报](./public-api-compatibility-contract.md)为准。

`v0.1` 不创建自有 `Tracer`、`Meter`、`Logger`、`Observer` Port，也不新增 `observability` 或 `otelkit` 公共包。实验性 PostgreSQL、Outbox 和 Inbox 适配器通过显式构造选项支持 OpenTelemetry API 与 Go `log/slog`；未配置时使用 no-op 行为。消费者分别拥有 SDK、Exporter、Collector、采样、资源属性、TLS、凭据、保留和告警。`idp-bento-go` 不直接依赖 `idp-obs/sdk-go`，但消费者可以在其提供标准 OpenTelemetry provider/exporter 且版本可锁定时选择装配。

框架只拥有自身技术表的迁移资产，例如 Outbox、Inbox 和适配器幂等状态；迁移随框架版本发布，具有稳定标识、规范模板 checksum、最小兼容版本与升级说明。清单不暴露可编辑 SQL，消费者按 ID 通过唯一 schema token 的受控入口渲染；已经发布的迁移文件不可修改，只能追加新迁移。框架不得发布 TMS 或 Parcel 的业务表、业务索引、租户模型或领域数据迁移，也不得提供在应用启动时自动执行迁移的能力。

TMS 与 Parcel 分别选择迁移工具，拥有迁移历史、数据库凭据、执行窗口、技术迁移与业务迁移的排序、生产审批、备份和回退方案。消费者迁移作业直接消费所锁定框架版本提供的只读迁移清单和渲染结果，不复制后再修改框架 SQL。应用启动显式调用只读 `CheckSchema` 验证 UTF8 编码与必需对象子集，不执行修复。框架发布候选必须验证空库安装和上一受支持版本升级；生产升级优先使用扩展兼容的前向迁移，破坏性收缩只能在旧版本退出且消费者明确编排后执行。

## 所有权边界

- 框架拥有技术契约及其兼容性语义。
- TMS 拥有全部 TMS 领域模块、资源装配器、授权动作、SQL 和 Gatekeeper 适配器。
- Parcel 拥有全部 Parcel 领域模块、包裹/网络/关务/结算 SQL 和产品适配器。
- 框架不得导入任何消费者，不得发布消费者业务迁移，也不得提供跨产品数据库访问。
- 共享技术类型不得成为伪装的共享业务模型。

## 发布治理

- TMS 与 Parcel 都不是框架宿主；框架维护组不少于 3 人，具体人员在远端仓库创建前保持 `UNBOUND`。
- 文档和不影响公共行为的内部变更至少需要 1 名非作者维护者批准；公共 API、事件信封、迁移、持久化格式和兼容性策略变更需要 2 名非作者维护者以及 TMS、Parcel 消费者代表批准。
- 合并权限与发布权限分离。默认分支和稳定 tag 受 Ruleset 保护；稳定 tag 只能由满足门禁的 CI 发布身份创建，并由 DSSE 发布授权签名绑定版本、提交与候选证据，维护者个人不得直接发布稳定版本。
- 消费者使用不可变 tag 或版本；禁止浮动分支和生产构建 `replace` 指令。
- 框架普通 PR 只运行框架单元、集成、架构、迁移和公共 API 兼容性测试，不要求访问 TMS 或 Parcel 私有源码。
- 框架发布候选使用不可变版本坐标和 module 校验和；兼容性协调作业触发或等待 TMS、Parcel 各自拥有的最小 `bento-contract` 作业。
- 两个消费者合同都必须针对同一候选版本执行并产生可追溯结果；任何失败、超时或版本坐标不一致都阻止创建稳定 tag，但不阻止继续发布带 `-rc` 的预发布候选用于修复验证。
- `bento-contract` 只覆盖框架 API、序列化、事务、Repository、迁移和适用适配器兼容性，不把消费者完整产品 CI 或无关业务测试变成框架发布门禁。
- TMS、Parcel 的完整 CI 在各自依赖升级 PR 中执行并决定是否采用新框架版本；框架发布成功不自动升级任何消费者。
- 框架发布作业不得保存消费者源码、数据库、业务测试产物或长期访问凭据，只保存消费者、提交、候选版本、module 校验和、合同版本和结果等最小证明。
- 公共 API 新增项必须引用至少两个真实消费者用例，除非它是已经批准的公司级技术原语。
- `v0.x` 的破坏性变化必须提供迁移说明并完成消费者验证。
- `v1.0.0` 要求每个消费者至少有一个通过生产门禁的真实切片，并明确接受稳定 API 清单。
- `v1` 之后的破坏性变化必须进入新的主版本，并提供兼容窗口说明。
- `v0.x` 最新 minor 为 `FULL`，前一 minor 在后继 minor 发布后进入 90 天 `SECURITY`；`v1` 被后继 minor 取代后获得 6 个月 `SECURITY`，未来 `v2.0.0` 发布后最新 `v1` minor 获得 12 个月 `SECURITY`。
- `SECURITY` 只回补高危安全、授权绕过、跨租户泄露、数据损坏、不可恢复迁移和阻断升级的严重兼容问题；新功能和普通缺陷只进入当前全量支持线。
- 每次 minor 或 major 发布必须公布支持矩阵、状态和明确 EOL 日期；已承诺窗口不能因更快发布新版本而提前终止。
- 已知消费者合同失败不能通过紧急例外绕过；CI 故障只能改用消费者拥有的等价合同证明。无法取得完整门禁时只能保留预发布候选。

## 私有化交付约束

- 消费者运行镜像只包含编译后的 Go 二进制，不包含框架或产品源码树。
- 构建镜像、module 凭据和源码检出内容不得进入最终镜像层。
- 框架不得嵌入密钥、环境凭据或客户配置。
- 可复现构建必须锁定精确 Go、依赖和框架版本。

## 关键场景

### TMS 消费者

TMS 使用框架的领域/事件原语和可选 PostgreSQL/Outbox 支持，同时把采购、执行、授权资源事实和 Gatekeeper 集成保留在 TMS 仓库中。

### Parcel 消费者

Parcel 使用相同的稳定技术契约，同时把包裹、路由、节点作业、关务、履约和结算模型保留在 Parcel 仓库中，并保持独立部署和发布。

### 框架升级

新的框架版本必须通过两个消费者合同套件。TMS 可以先于 Parcel 升级，反之亦然；任何消费者都不能被迫与另一个消费者共享发布日期或数据库迁移。

## 风险与缓解

- **框架优先于真实用例进行抽象**：公共 API 必须提供真实消费者证据。
- **业务语义泄漏到共享包**：拒绝产品术语、产品表和产品特定生命周期规则。
- **一个框架版本同时阻塞两个消费者**：使用 SemVer、兼容窗口和独立消费者升级时间。
- **复制源码形成分叉**：发布版本化 module，禁止消费者仓库复制框架源码。
- **可选适配器变成强制耦合**：核心契约保持独立，并在 CI 中验证依赖方向。
- **`v0.1` 公共面过大**：默认保持 internal；只有真实消费者需要且具备合同测试的 API 才公开。
- **通用 Repository 变成隐藏 ORM**：公共契约只保留聚合持久化能力，禁止任意查询和通用实现，消费者拥有全部语义与 SQL。
- **租户作用域被隐藏或遗漏**：作用域聚合的加载和条件更新使用显式强类型复合键，跨租户同 ID 合同测试验证不会串权。
- **错误分类掩盖安全或并发事实**：更新影响 0 行统一按 `Conflict` 处理，不额外探测对象存在性；适配器错误保留原始原因并通过 `errors.Is` 提供稳定分类。
- **包名含混导致职责漂移**：使用职责明确的公共包名并在 CI 校验导入图；HTTP 和具体消息产品适配器保持在消费者仓库或框架 `internal`，可观测能力只进入拥有操作的实验性适配器。
- **迁移职责不清导致自动改库或 SQL 分叉**：框架发布不可变技术迁移资产，消费者拥有执行编排；禁止启动时自动迁移和修改已发布迁移。
- **框架发布被消费者无关故障阻塞或获得过宽权限**：只把消费者拥有的最小合同作业作为门禁，使用不可变候选和最小可追溯结果，不拉取消费者业务源码或完整 CI 状态。
- **单一产品或个人控制共享框架**：使用不少于 3 人的独立维护组、双消费者评审、受保护分支和仅 CI 可执行的签名发布。
- **无限维护旧版本或迫使消费者同步升级**：限制同时支持范围，并用 90 天、6 个月和 12 个月的明确窗口平衡维护成本与消费者独立升级。
- **自建可观测性抽象或泄露敏感高基数数据**：直接使用 OpenTelemetry API 和 `log/slog`，核心不导入 SDK；默认禁止租户、主体、聚合、事件载荷、SQL 参数和凭据进入遥测。
- **测试替身取代真实持久化验证或泄漏到生产**：合同运行器由工厂驱动真实端口，适配器专属并发测试继续执行；框架和消费者 CI 都检查生产依赖图中不存在 `testkit`。

## 未决问题

框架架构与 `v0.1` module 解析方式没有剩余未决选择；真实基础设施值和发布身份继续由 `B-01B`、`B-04` 至 `B-06` 管理。

## 下一步

1. 每次 grilling 只解决一个高影响决定。
2. 本文档只记录已经确认的结论。
3. `R-01` 至 `R-06`、最终实施决策简报和难以逆转的 ADR 已确认，正式 module path 已固定为 `go.idp.xyz/idp-bento-go`。
4. 五个稳定核心包、三个 PostgreSQL 包、本地兼容性制品和 PostgreSQL 16.12 仓库验收已经完成。
5. 下一步部署并验证 `B-01B` 的 vanity 解析，绑定 `B-04`/`B-06` 真实身份与基线，并按已确认的 `B-05` GitHub Actions、WORM、OIDC 和 DSSE 合同建立受保护 CI、不可变 RC 与双消费者候选证明。
