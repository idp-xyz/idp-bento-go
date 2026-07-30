# 公共 API 与行为兼容性决策简报

状态：已确认

Date: 2026-07-29

## 主题与范围

本简报确认 `R-06`：固定公共包清单、稳定性分级、API 快照格式、编译夹具、行为兼容性清单、版本比较基线、破坏性变化门禁和发布证据。

正式 module path 后续已通过 `F-40` 绑定为 `go.idp.xyz/idp-bento-go`，实际 Git 仓库绑定为 `https://github.com/idp-xyz/idp-bento-go`，`v0.1` 使用 HTTPS vanity metadata 直连该仓库，CI 平台选择 GitHub Actions。本简报不绑定解析服务部署、真实 workflow/App/存储/签名或工具账号；这些仍属于 `B-01B`、`B-04` 与 `B-05`。本文的确认本身不等于已经创建发布候选；当前本地实现进度在文末单独记录。

## 已验证事实

- Go 消费者按 module 版本重新编译，因此首版承诺是源码与行为兼容性，不承诺跨编译器或平台的二进制 ABI。
- 编译夹具只能证明其实际调用过的符号仍可编译，不能发现未覆盖符号、接口实现方破坏或常量值变化。
- 导出声明语义比较可以发现类型、方法和字段变化；当前锁定 apidiff 版本对部分泛型约束收窄存在漏报，因此仓库另加约束指纹。即使声明面完整，也不能发现 `Envelope.Validate` 收紧、JSON 字段变化、`errors.Is` 分类变化或迁移内容重写。
- 框架普通 PR 不能读取 TMS/Parcel 私有源码；真实消费者合同只在不可变候选阶段运行。
- `eventing` 已确认同一个 Envelope SpecVersion 下不得收紧校验规则。只把当前常量改成新版本而停止接受旧版本，会使升级前已写入 Outbox 的合法记录在发布侧变成无效记录。
- `v0.x` patch 不得破坏公共合同；`v0.x` minor 可以有受控破坏性变化；`v1` minor 必须向后兼容。

## 评估选项

### A. 只维护编译夹具

实现简单且最接近消费者调用，但覆盖面取决于人工使用每个符号。没有被夹具调用的接口方法、结构字段或泛型约束可以被静默删除，行为收紧也不会导致编译失败。

### B. 只维护导出 API 文本快照

可以完整覆盖声明面，但普通文本 diff 无法可靠判断 Go 类型兼容性，也无法覆盖常量值、JSON wire、校验接受集合和错误分类。

### C. 类型语义快照 + 编译夹具 + 行为清单 + 双消费者合同

使用固定版本的 Go API 语义比较工具覆盖全部导出声明；用业务无关编译夹具证明典型调用形态；用版本化、机器可比较的行为清单保护常量和 Envelope 规则；最后由两个真实消费者验证候选。

## 决定

采用选项 C。

- 使用 `golang.org/x/exp/cmd/apidiff` 作为 Go 源码 API 语义比较器，通过 `go.mod` 的 tool dependency 管理；`B-02` 已将所属 module 精确锁定为 `v0.0.0-20260727155853-b88d891fe743`，checksum 由 `go.sum` 固定。
- 提交工具生成的逐包 API export snapshot，不提交人工整理的 `go doc` 输出作为机器基线。
- 提交业务无关的核心与实验性适配器编译夹具；编译夹具必须使用全部首版公共符号，但不能包含消费者业务类型。
- 提交 `Envelope` 每个 SpecVersion 的确定性行为清单，覆盖公共常量、wire 字段、校验边界、错误分类和 `ValidEnvelope`。
- 发布候选同时比较前一发布版本、候选提交和已提交快照；任一层不一致都不能通过修改当前期望文件静默掩盖。
- API 新增也属于受治理的公共变更，必须更新清单、文档、测试和消费者证据。
- `internal`、包内测试和未导出符号不属于公共兼容性承诺，但其变化仍须通过框架测试。

## 公共包清单

`api/packages.json` 是包级权威清单，首版只允许以下条目：

| 相对 import path | 稳定性 | `v0.1` 处理 |
| --- | --- | --- |
| `domain` | `STABLE-CORE` | 完整 API 与行为门禁 |
| `repository` | `STABLE-CORE` | 完整 API 与行为门禁 |
| `eventing` | `STABLE-CORE` | 完整 API、wire 与行为门禁 |
| `application` | `STABLE-CORE` | 完整 API 与事务行为门禁 |
| `testkit` | `STABLE-CORE` | 完整 API、确定性与生产依赖门禁 |
| `postgres` | `EXPERIMENTAL` | API diff 必须可见；`v0.x` minor 才可受控破坏 |
| `postgres/outbox` | `EXPERIMENTAL` | 同上，并继续执行 `I-04` |
| `postgres/inbox` | `EXPERIMENTAL` | 同上，并继续执行 `I-04` |

新增、删除、重命名包或改变稳定性都必须修改 `api/packages.json`，按公共 API 变更评审。职责含混的 `contracts`、`ports`、`common`、`testing`、`observability` 等包不得因代码生成或临时辅助意外进入清单。

## `v0.1` 公共符号清单

以下是首个实现的导出 allowlist。精确签名由各所有者合同文档定义，代码生成后的 apidiff snapshot 必须与本清单一致；未列出的符号默认不得导出。

### `domain`

- 类型：`EventType`、`EventVersion`、`Event`、`EventBuffer`。
- 方法：`(*EventBuffer).Record`、`(*EventBuffer).Pending`、`(*EventBuffer).Len`、`(*EventBuffer).Acknowledge`。

### `repository`

- 类型：`Revision`、`Loaded`、`Loader`、`Inserter`、`VersionedUpdater`。
- 错误：`ErrNotFound`、`ErrAlreadyExists`、`ErrConflict`。
- `v0.1` 不导出通用 `ScopedKey`。作用域键的命名、构造、零值合法性和字段含义属于消费者；一个只含 `Scope`/`ID` 的通用结构只节省两个字段，却会削弱语义化键与校验边界。

### `application`

- 类型：`Handler`、`HandlerFunc`、`Clock`、`IDGenerator`、`TxFunc`、`Transactor`。
- 方法/函数：`HandlerFunc.Handle`、`Transactional`。
- 错误：`ErrRollbackOnly`、`ErrTransactionConflict`、`ErrCommitUncertain`。

### `eventing`

- 当前与历史版本常量：`SpecVersionV1`、`SpecVersion`、`JSONContentType`。
- 限制常量：`MaxEventIDLength`、`MaxSourceLength`、`MaxEventTypeLength`、`MaxScopeLength`、`MaxSubjectLength`、`MaxPartitionKeyLength`、`MaxCorrelationIDLength`、`MaxCausationIDLength`、`MaxTraceParentLength`、`MaxTraceStateLength`、`MaxFailureCodeLength`、`MaxInboxConsumerLength`、`MaxPayloadBytes`、`MaxOutboxClaimLimit`。
- 失败常量：`FailureEnvelopeInvalidOnPublish`、`FailureClaimLimitExceeded`。
- 基础类型：`EventID`、`EventType`、`EventVersion`、`FailureCode`、`Envelope`。
- Envelope 方法：`Validate`、`Clone`、`MarshalJSON`、`UnmarshalJSON`。
- Publisher：`Publisher`、`PublisherFunc` 与 `PublisherFunc.Publish`。
- Outbox：`OutboxWriter`、`OutboxClaim`、`DeliveryRef`、`Delivery`、`DeliveryFailure`、`OutboxClaimer`、`OutboxFinalizer`。
- Inbox：`InboxConsumer`、`InboxKey`、`InboxStatus`、`InboxProcessing`、`InboxProcessed`、`InboxRejected`、`InboxReceipt`、`InboxStartResult`、`InboxStarter`、`InboxCompleter`。
- 错误：`ErrInvalidEnvelope`、`ErrUnsupportedSpecVersion`、`ErrInvalidRequest`、`ErrAlreadyEnqueued`、`ErrPublishRejected`、`ErrPublishUncertain`、`ErrInboxBusy`、`ErrLeaseLost`、`ErrInvalidTransition`。

`SpecVersionV1` 固定为 `"1.0"`；`SpecVersion` 是当前新建信封使用的版本别名，`v0.1` 等于 `SpecVersionV1`。未来新版本必须新增独立常量并保留仍受支持的历史常量，不能通过改写 `SpecVersionV1` 表达升级。

### `testkit`

- `ErrIDSequenceExhausted`。
- `FixedClock`、`NewFixedClock`、`FixedClock.Now`。
- `SequenceIDGenerator`、`NewSequenceIDGenerator`、`SequenceIDGenerator.NewID`。
- `PublishAttempt`、`RecordingPublisher`、`NewRecordingPublisher`、`RecordingPublisher.Publish`、`RecordingPublisher.Attempts`、`RecordingPublisher.Published`。
- `ValidEnvelope`。
- `RepositoryContractScenario`、`RepositoryContractFactory`、`RunRepositoryContract`。
- `OutboxContractScenario`、`OutboxContractFactory`、`RunOutboxContract`。
- `InboxContractScenario`、`InboxContractFactory`、`RunInboxContract`。

### 实验性 PostgreSQL 包

- `postgres`：`DefaultSchema`、`Querier`、`Executor`、`ErrTransactionRequired`、`ErrInvalidConfig`、`ErrSchemaIncompatible`、`ErrMigrationNotFound`、`ErrMigrationIntegrity`、`DB`、封闭 `Option`、`WithSchema`、三个可观测性 `With*`、`NewDB`、`Schema`、`Transactor`、`ReadExecutor`、`RequireExecutor`、`CheckSchema`、`Migration`、`Migrations`、`RenderMigration`。
- `postgres/outbox`：`Store`、封闭 `Option`、`WithTracerProvider`、`WithMeterProvider`、`WithLogger`、`NewStore`、`Enqueue`、`Claim`、`MarkPublished`、`RecordFailure`、`Abandon`。
- `postgres/inbox`：`DefaultLockTimeout`、`Store`、封闭 `Option`、`WithLockTimeout`、`WithTracerProvider`、`WithMeterProvider`、`WithLogger`、`NewStore`、`Start`、`MarkProcessed`、`MarkRejected`。

实验性不是“不可见”。这些符号同样进入 snapshot 和变更报告，只是在 `v0.x` minor 的兼容承诺较弱；patch 仍不得静默破坏。

## 兼容性制品布局

代码基线建立后创建：

```text
api/
  packages.json
  snapshots/
    domain.apidiff
    repository.apidiff
    eventing.apidiff
    application.apidiff
    testkit.apidiff
    postgres.apidiff
    postgres-outbox.apidiff
    postgres-inbox.apidiff
  behavior/
    envelope-current.json
    envelope-v1.json
  changes/
    <from>--<to>.json
    <from>--<to>.apidiff.txt
compat/
  compile/
    core/
    postgres/
internal/
  compatibility/
  cmd/
    apisnapshot/
    candidatecheck/
    compatcheck/
    compatmanifest/
    dependencycheck/
```

- `packages.json` 使用 UTF-8、LF、稳定字段顺序，记录格式版本、相对包路径和稳定性，不记录本机绝对路径、生成时间或 CI run ID。
- `snapshots/*.apidiff` 是 apidiff 可直接读取的二进制 export data，不是文本。它们由 `internal/cmd/apisnapshot` 使用空 `token.FileSet` 生成，禁止手工编辑。
- 官方 `go tool apidiff -w` 会把 checkout 与 Go toolchain 的绝对源码路径写入逐包 export data，即使设置 `GOFLAGS=-trimpath` 也不会消除；因此不得用它直接更新提交基线。固定版本的官方 apidiff 仍负责读取基线并执行类型语义比较。
- `compat/compile/core` 使用每个稳定核心符号并验证关键接口赋值；`compat/compile/postgres` 只做构造与类型级编译，不连接数据库。
- `internal/cmd/compatmanifest` 运行公开 API 的确定性 case matrix，输出规范 JSON；生成器和结果都进入评审。
- `internal/cmd/compatcheck` 只读比较独立的不可变旧 `api/` 基线与候选制品，不提供 `-update`，并拒绝旧基线与候选指向同一目录。
- `internal/cmd/candidatecheck` 组合候选阶段全部仓内门禁，要求真实 PostgreSQL DSN、禁用 Go 测试缓存，并强制显式选择初始 `v0.1` 或独立旧基线模式；它不创建或发布 RC。
- `internal/cmd/dependencycheck` 从不含测试变体的实际 Go 导入图检查生产包、核心、`testkit` 和两个 PostgreSQL Store 的依赖方向。
- `changes` 只在发布比较存在可见变化时创建。`.apidiff.txt` 保存固定工具生成的完整报告，JSON 保存分类、替代方案、迁移文档路径和报告 SHA-256。

任何生成命令必须先写临时文件并比较，再由显式更新命令替换基线。普通 `go test` 不得在工作区静默改写 snapshot。

### 洁净检查与显式更新

普通 PR 使用以下命令检查已提交制品，不改写文件：

```powershell
go run ./internal/cmd/apisnapshot
go run ./internal/cmd/compatmanifest -mode version -out api/behavior/envelope-v1.json
go run ./internal/cmd/compatmanifest -mode current -source api/behavior/envelope-v1.json -out api/behavior/envelope-current.json
go run ./internal/cmd/dependencycheck
```

公共 API 或行为经过评审后，维护者显式添加 `-update` 更新对应制品，并审查 apidiff 与 JSON 差异。PostgreSQL 三个公共包实现前，`apisnapshot` 会明确报告它们为 skipped；当前三个包、对应 snapshot 与 `compat/compile/postgres` 已经存在。稳定候选必须运行 `go run ./internal/cmd/apisnapshot -require-all`，任何缺失包都失败，且 PostgreSQL 编译夹具不能由核心夹具替代。

生成器每次先在目标目录写临时文件并做字节比较；连续两次相同输入必须报告 clean。API 快照不得包含 checkout、用户目录或 toolchain 绝对路径。行为清单的大型合法 wire case 使用字节数与 `sha256:` 摘要保存确定性证据，避免在 JSON 基线中复制 1 MiB Payload。

存在上一不可变版本后，候选额外执行：

```powershell
go run ./internal/cmd/compatcheck -baseline <上一不可变版本的 api 目录> -candidate api
```

`compatcheck` 对 packages manifest、逐包 API snapshot、所有历史 `envelope-v*.json` 和 `envelope-current.json` 分层比较；任何兼容或不兼容公共变化都先失败并进入发布治理。候选侧更新当前 snapshot 或 manifest 不会改写独立旧基线，因此不能消除历史差异。

## API 语义比较

- apidiff 对 `api/packages.json` 中每个包比较上一基线与候选，覆盖导出类型、别名、常量/变量声明类型、函数、方法、结构字段和接口方法。
- 当前锁定的 apidiff 版本不会报告 `~int | ~string` 收窄为 `~int`；该限制由负向夹具确认。`compatcheck` 因此额外比较所有导出泛型函数和命名类型的约束指纹，约束变化一律先按不兼容失败，不能单独依赖 apidiff 文本报告。
- 包删除、稳定性降级或导入路径变化由 packages manifest 额外判断，不能依赖单包工具自行发现。
- 增加接口方法、收紧泛型约束、删除方法、改变字段类型、改变函数参数/返回值或把值接收器改为只剩指针接收器均按不兼容处理。
- apidiff 报告的“兼容新增”仍需公共 API 评审；机器兼容不等于抽象已经有两个消费者证据。
- API snapshot 不保护运行语义、错误身份、常量具体值、JSON tag/wire 或数据库迁移；这些分别由行为清单、合同测试和迁移 checksum 门禁补充。

首个 `v0.1.0` 没有上一发布版本。实现必须先与本文公共符号 allowlist 对齐，生成初始 snapshots，经框架维护者和两个消费者代表确认后作为首个发布基线。不得把当前实现中偶然导出的符号自动视为既定 API。

## Envelope 行为兼容性

### 多 SpecVersion 规则

- `Envelope.Validate` 按 `Envelope.SpecVersion` 分派到对应版本规则，而不是只与当前 `SpecVersion` 常量做字符串相等比较。
- `v0.1` 只支持 `SpecVersionV1`；未知版本同时包装 `ErrInvalidEnvelope` 与 `ErrUnsupportedSpecVersion`。
- 未来增加 V2 时新增 `SpecVersionV2` 并把 `SpecVersion` 指向 V2；V1 规则和 manifest 保持不变，V1 Outbox 记录、重投消息和消费者仍按 V1 校验。
- `UnmarshalJSON` 接受所有当前受支持版本并按各自规则验证；Enqueue 与 Publish 使用信封自身版本校验。
- 历史版本只有在公布 EOL、完成消费者与在途/持久化记录迁移，并按允许破坏的版本级别发布后才能停止支持。历史 manifest 永久保留，不改写为新规则。

### `envelope-v1.json`

行为清单至少包含：

- 格式版本、`SpecVersionV1`、`JSONContentType`、全部 `Max*` 值和固定 FailureCode 值。
- 每个 JSON 字段名、必填/可空、空值省略、未知字段接受规则和 UTC RFC3339Nano 输出。
- 所有字符串的空白、UTF-8、控制字符、精确字节上限和超一字节 case；至少包含 ASCII 与多字节 UTF-8 边界。
- Payload 的空、对象、数组、标量、null、非法 JSON、精确 `MaxPayloadBytes` 和超限 case。
- 零/非零事件版本和时间、支持/未知 SpecVersion、四个可空传播字段。
- Marshal/Unmarshal round-trip、Payload 非 Base64 和 Clone 深拷贝。
- 每个 case 的稳定 ID、输入构造摘要、`valid`、`errors.Is(ErrInvalidEnvelope)`、`errors.Is(ErrUnsupportedSpecVersion)` 和规范 wire 结果（适用时）。

输出按 case ID 排序且不包含时间戳、随机值、内存地址或本机路径。生成器必须证明连续两次运行字节相同。

`envelope-current.json` 单独记录当前 `SpecVersion`、对应版本 manifest 的 SHA-256，以及 `testkit.ValidEnvelope` 的版本、合法性和独立 Payload 行为。未来把当前版本切换到 V2 时，只更新 current manifest 并新增 `envelope-v2.json`；不得修改 `envelope-v1.json`。这样当前测试起点可以演进，同时历史版本接受集合保持不可变。

比较规则：

- 对仍受支持的同一 SpecVersion，旧基线 `valid=true` 而候选为 false 是禁止的规则收紧，任何 patch、minor 或 major 都不能把它伪装成同一版本修复。
- 删除旧 case、删除历史版本 manifest、改变既有 wire 字段名/含义、把旧合法 Payload 改成非法或丢失稳定错误分类均视为破坏。
- `valid=false -> true` 是规则放宽，源码兼容但仍需协议评审、更新行为清单和双消费者合同。
- 新的更严格规则进入新的 SpecVersion manifest；升级期间旧 manifest 继续运行。
- `envelope-current.json` 只有在新增目标版本 manifest、更新 `SpecVersion` 当前别名并提供变化记录时才允许改变；单独改变 current fixture 或别名使候选失败。

## 编译与行为夹具

- 核心编译夹具只使用业务无关局部类型，实现 `domain.Event`、Repository 泛型实例和 Handler/Publisher 端口，并编译所有 `testkit` scenario 字段与 runner 调用。
- PostgreSQL 编译夹具验证封闭 Option 只能由框架构造、Querier 不暴露 Exec、Executor 不暴露 Commit/Rollback，以及 Store 满足对应 eventing 小接口。
- 编译夹具不得使用反射枚举 API；完整性由 snapshots 和 packages manifest 保证。
- 公共错误的文字不是稳定合同；夹具使用 `errors.Is`。如果稳定 sentinel 被替换或包装关系改变，行为测试必须失败。
- 事务、Repository、Outbox/Inbox、迁移和可观测性行为继续执行已有单元/集成/消费者合同；R-06 不把全部行为压缩成一个 API 文件。

## 版本比较与发布门禁

### 普通 PR

1. 生成候选 packages manifest、逐包 snapshot 和行为清单，与提交文件做洁净比较。
2. 对默认分支基线运行 apidiff，输出完整兼容与不兼容报告。
3. 运行核心/实验适配器编译夹具、框架测试、架构依赖门禁和适用 `I-04`。
4. 无公共变化时不得出现 snapshot churn；有变化时必须附公共用例、文档与测试，并按治理规则评审。

### `v0.x` patch

- 与同 minor 的上一稳定 patch 比较。
- 稳定核心和实验性适配器都不得存在已知不兼容 API、现有 SpecVersion 行为收紧、wire 破坏或已发布迁移变化。
- 兼容新增原则上进入 minor；只有修复现有合同且不扩大公共面时进入 patch。

### `v0.x` minor

- 与上一稳定 minor 的最新版本比较。
- 允许受控 API 破坏，但必须保存完整 apidiff 报告、`api/changes/<from>--<to>.json`、迁移说明、替代调用和双消费者对同一候选的通过结果。
- 未在生成报告中出现的额外不兼容变化、缺失迁移路径或任一消费者失败都阻断稳定 tag。
- 同一 Envelope SpecVersion 的规则收紧不适用该例外，必须新增版本。

### `v1+`

- patch/minor 必须保持稳定核心向后兼容；破坏性 API、wire 或持久化变化只能进入下一 major。
- 废弃符号使用标准 Go doc `Deprecated:` 标记，至少保留一个稳定 minor 且不少于 6 个月；删除仍须 major。
- major 仍需完整变化报告、迁移说明、支持窗口和双消费者候选证明，不能只以版本号豁免审查。

### 实验性适配器

- `EXPERIMENTAL` 只允许在 `v0.x` minor 受控破坏，不允许在 patch 静默变化。
- 每次变化都生成 diff 并由适用消费者验证；`I-04`、迁移不变性和 schema 兼容性门禁不因实验标签降低。
- `v1.0.0` 前必须明确每个实验包是进入稳定清单、继续以清晰政策发布，还是移出主 module 公共面；不得默认继承含混状态。

## 变化记录格式

`api/changes/<from>--<to>.json` 至少包含：

- `format_version`、`from`、`to`、候选 module checksum。
- apidiff 工具版本、Go toolchain 版本和 `.apidiff.txt` 的 SHA-256。
- 每项变化的稳定 ID、包、符号、`additive`/`incompatible`/`behavioral` 分类和原因。
- 替代 API 或明确的无替代说明、迁移文档相对路径、受影响的稳定/实验分类。
- Envelope SpecVersion、迁移资产或消费者合同是否受影响。

审批人和 CI 身份由受保护平台记录，不复制到可任意编辑的 JSON 作为授权证明。JSON 只提供机器可核对的变化内容；治理系统核验双维护者、双消费者和适用安全批准。

## 双消费者合同

- 框架普通 PR 使用内置业务无关编译夹具，不访问消费者仓库。
- 不可变 RC 发布后，TMS 与 Parcel 分别针对精确候选版本和 checksum 编译自身真实调用，运行 `R-05` 合同、适用 PostgreSQL 集成和最小真实切片。
- 消费者结果必须记录合同套件版本，使框架能证明它们覆盖候选的 public manifest 版本。
- 两份最小结果使用严格 JSON v1，并由 `proofcheck` 校验消费者身份、`B-06` 预期完整提交、预发布 SemVer、同一 module checksum、合同套件版本、`PASS` 和完成时间；精确格式见[双消费者候选合同证明格式](./consumer-proof-format.md)。
- apidiff 通过但任一消费者失败仍阻断稳定发布；消费者通过不能豁免 API/行为门禁失败。
- 框架只保存最小结果，不保存消费者源码、业务夹具或数据库快照。

## 失败处理

- snapshot 未更新或生成不洁净：PR 失败，开发者显式执行更新命令并评审差异。
- snapshot 被更新但 apidiff 报告缺失：PR 失败，不能用更新基线消除历史比较。
- 工具或 Go 版本漂移：PR 失败；升级工具链必须是独立变更，重新生成全部 snapshot 并证明没有伪造 API 变化。
- 上一版本或 module checksum 无法取得：稳定候选失败，不允许跳过比较；预发布候选可以继续用于修复环境。
- 行为 manifest 不确定或连续运行不同：PR 失败，先移除时间、随机数、map 非稳定顺序或本机路径。
- 发现未登记公共包/符号：PR 失败；要么改为未导出/internal，要么按公共 API 新增流程登记。

## 明确拒绝

- 只依赖 `go test` 编译当前仓库、只依赖消费者冒烟或只看人工 `git diff`。
- 把 `go doc`、README 或文档目录当作唯一机器 API snapshot。
- 自动接受所有 additive API，或以“实验性”为由跳过 patch 兼容性检查。
- 允许 PR 同时修改实现和当前 snapshot 后不再与上一发布版本比较。
- 把 error 文本、JSON 对象键顺序、绝对路径或生成时间纳入稳定合同。
- 在同一 Envelope SpecVersion 下收紧规则，或升级当前版本时立即停止接受旧 Outbox 记录。
- 让框架 CI取得 TMS/Parcel 私有源码或长期凭据以运行普通 PR 检查。

## 完成判据

`R-06` 的实现验收要求：

- 首版代码只导出本文 allowlist，初始 snapshots 可重复生成并与固定工具版本一致。
- API 负向夹具证明删除函数、增加接口方法、改变泛型约束、改变结构字段和删除包都会失败。
- 行为负向夹具证明减小长度、禁止旧合法 Payload、改变 JSON 字段或删除 `ErrUnsupportedSpecVersion` 分类都会失败。
- snapshot 更新不能消除与上一版本的 apidiff；受控破坏必须产生完整 changes 记录。
- 核心与 PostgreSQL 编译夹具通过，生产依赖图不包含 `testkit`。
- TMS、Parcel 对同一不可变候选和 checksum 返回通过结果。

八个公共包的逐包快照、核心/PostgreSQL 编译夹具、Envelope 行为清单、API/行为负向夹具、生产依赖图门禁和独立旧基线比较器已经实现并通过仓内检查。首个 `v0.1.0` 没有上一发布版本；首次候选审查、后继版本的真实旧基线比较、变化记录审批和双消费者结果仍须在对应候选阶段产生。

## 后续门禁

`R-06` 设计、本地基线和仓内负向门禁已实现，PostgreSQL 仓库内 `I-04` 也已通过。跨仓 CI、首次不可变候选审查和双消费者候选证明尚未完成，因此当前仍不能宣称 `v0.1` 可发布。

候选阶段统一执行入口与失败规则见[候选前置门禁运行手册](./release-candidate-runbook.md)。

本决定已经进入 [ADR-0007](../adr/0007-compatibility-and-consumer-gates.md)。最终实施决策简报已经确认，兼容性制品随公共实现同步建立。
