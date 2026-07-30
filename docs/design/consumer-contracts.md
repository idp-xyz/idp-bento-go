# 消费者合同策略

状态：已确认

## 目标

确保每个 `idp-bento-go` 版本都能被 IDP-TMS 与 IDP-Parcel 使用，同时不耦合两者的业务模型、代码仓库或发布计划。

## 已确认约束

- 框架发布必须通过两个消费者的合同测试。
- 消费者锁定精确框架版本并独立升级。
- 合同验证技术兼容性，不验证或共享产品业务行为。
- 产品源码和凭据不得作为框架制品发布。
- 框架普通 PR 不访问消费者仓库；消费者合同只在不可变发布候选阶段成为稳定发布门禁。
- TMS 与 Parcel 分别拥有自己的 `bento-contract` 作业，框架只触发或等待最小结果，不接管消费者完整 CI。
- 合同失败、超时或候选版本不一致时禁止创建稳定 tag；预发布候选可以继续迭代。

## `v0.1` 最小合同矩阵

- 使用最小消费者夹具验证编译期 API 兼容性。
- 验证 `domain` 不提供 Aggregate/Entity/Value Object 基类、消费者业务标识或通用领域错误。
- 验证 `Event` 的类型非空、版本从 `1` 开始且发生时间非零。
- 验证 EventBuffer 零值可用、记录顺序稳定、`Pending` 返回 slice 副本、`Acknowledge` 幂等清空。
- 验证失败事务保留待处理领域事件，成功事务确认清空，聚合重建不产生新事件。
- 验证 HandlerFunc 正确实现 Handler，并保持 context、响应和错误语义。
- 验证 Clock 与 IDGenerator 可以使用确定性实现替换且不依赖全局状态。
- 验证 `FixedClock` 拒绝零时间并保持不可变；`SequenceIDGenerator` 按输入消费、取消不消耗 ID、并发不重复且耗尽返回 `ErrIDSequenceExhausted`。
- 验证外层事务只提交一次，错误、取消和 panic 均回滚，panic 原样传播。
- 验证同一 Transactor 嵌套共享一个事务，内层失败使外层 rollback-only。
- 验证不同 Transactor 嵌套返回 `ErrTransactionConflict`，提交结果不确定返回 `ErrCommitUncertain`。
- 验证 `Transactional` 仅在提交成功后返回 Result，失败时返回零值。
- 验证写事务参与者在没有事务句柄的 context 上返回基础设施错误，不静默使用连接池执行写入。
- 验证 ReadExecutor 在无事务时使用显式注入连接池、在匹配事务时加入事务、在其他 owner 的事务 context 上返回 `ErrTransactionConflict`，且其返回类型与动态包装器都不暴露 `Exec`；RequireExecutor 无事务时返回 `ErrTransactionRequired`，两条路径都不能通过类型断言取得原始 `pgx.Tx`、连接池或 Commit/Rollback 能力。
- 验证提交结果不确定后只有具备稳定 CommandID 和事务性命令处理记录或等价业务幂等证明时才能自动恢复；EventID 与 `ErrAlreadyEnqueued` 不能替代命令幂等。
- 验证领域原语行为和事件信封序列化兼容性。
- 验证 Envelope 必填字段、`SpecVersionV1`/当前 `SpecVersion`、JSON 对象 Payload、UTC 时间与 `Type + Version` 规则；校验按信封自身版本分派，未知版本安全失败。
- 验证 JSON round-trip 不把 Payload 编码为 Base64，`Clone` 深拷贝 Payload，开放式 Metadata map 不属于公共合同。
- 验证 Publisher 单条发布并透明传播 context；明确拒绝和结果不确定错误可通过 `errors.Is` 分类。
- 验证 `RecordingPublisher` 对有效调用按序注入结果，只把成功调用列入 Published，并对输入、Attempts 与 Published 的 Envelope Payload 做深拷贝；取消和无效信封不消耗错误计划。
- 验证 `Enqueue` 与 `Publish` 拒绝未通过校验的信封，返回 `ErrInvalidEnvelope` 且不产生持久化或传输副作用。
- 验证已持久化信封在发布侧校验失败时进入 `ABANDONED` 并携带 `FailureEnvelopeInvalidOnPublish`，不消耗失败预算。
- 验证 Outbox 以 `Source + EventID` 去重，重复入队不覆盖载荷；业务写入与 Enqueue 同事务提交或回滚。
- 验证 Outbox Claim 的 Attempt、`MaxAttempts`、租约过期、旧 token 冲突、重试、发布成功和 `ABANDONED` 终态。
- 验证租约过期只递增 `Attempt`，仅 `RecordFailure` 递增 `Failures`，退避基于递增后的 `Failures`；`RecordFailure` 在预算未耗尽时重排、达到 `MaxFailures` 时原子终结，`Abandon` 不消耗失败预算；达到 `OutboxClaim.MaxAttempts` 的队首由 Store 原子终结且不再返回 Delivery。
- 验证同一 PartitionKey 保持入队顺序且不并发领取，不同 PartitionKey 可以并行。
- 验证 Inbox 以 `Consumer + Source + EventID` 幂等；首次、已处理、已拒绝和并发重复只产生一次本地业务副作用。
- 验证 Inbox 并发占用使用有界等待，后到者在超时后返回 `ErrInboxBusy` 并强制事务回滚，而不是返回 nil error 的普通结果。
- 验证 Inbox 占用、业务副作用和处理终态同事务提交；错误、取消、panic 和数据库明确确认未提交的提交失败全部回滚。提交结果不确定时不得假定已经回滚，重新投递由可能已经提交的 Inbox 终态抑制重复本地副作用。
- 在适用场景验证已经确认的 PostgreSQL `R-04` 事务、Outbox、Inbox 和幂等集成行为；真实实现已通过仓库内 `I-04`，候选仍须在受保护 CI 和两个消费者合同中复验。
- 验证事务参与者在另一个 Transactor 的事务 context 上返回 `ErrTransactionConflict`。
- 验证领取只产出分区队首且已到期的事件；租约过期后 `claimable_at` 使分区重新进入扫描，Publisher 崩溃不会永久冻结分区。
- 验证分区指针在终态转换时正确推进，最后一个事件终结后分区行被删除；人为制造终态/悬空指针后 Claim 能原子修复且不跳过更早非终态事件。
- 验证同一分区并发 Enqueue 的定序与提交可见性，不会先发布后入队的更晚事件。
- 以多轮并发形式验证不产生重复领取，且同一分区不同时持有多个有效租约；移除 `UPDATE` 内谓词复检后该用例必须失败。
- 验证消费者 bootstrap 在 `LeaseFor` 不大于 Publisher 超时时拒绝启动；PostgreSQL Store 不接收或推断 Publisher 超时。
- 验证 Inbox 有界等待在配置时长内返回 `ErrInboxBusy`，外层 Transactor 回滚；成功占用后锁超时已恢复默认值。
- 验证迁移清单的 `ID` 与规范模板 `Checksum` 在框架版本间不变化，`Migrations` 返回防御性元数据副本，框架不暴露可编辑 SQL 或任何迁移执行入口。
- 验证 schema 标识符边界、唯一模板 token、未知迁移与完整性错误；`CheckSchema` 只读验证 UTF8 编码和必需对象子集，并允许经过兼容性测试的扩展对象。
- 验证通用聚合 Repository Port 的加载、创建、乐观版本冲突和事务回滚语义。
- 验证消费者自定义复合查找键能够区分不同租户中的相同聚合 ID。
- 验证 `Loader` 和 `VersionedUpdater` 不从 `context.Context` 隐式取得租户或权威范围，缺少显式作用域键时不能执行持久化操作。
- 验证 `Inserter` 使用聚合自身的完整作用域身份，并拒绝缺失或无效的作用域身份。
- 验证错误租户、其他租户同 ID 与不存在对象具有相同的未找到外部语义，不泄露跨租户对象存在性。
- 验证 Repository 错误使用 `errors.Is` 分类，包装操作上下文后仍可识别 `NotFound`、`AlreadyExists` 和 `Conflict`。
- 验证带预期版本的更新影响 0 行时只返回 `Conflict`，不执行额外存在性查询，也不区分错误租户、对象不存在或版本过期。
- 验证取消、截止时间和基础设施故障不会被错误归类为 Repository 业务结果。
- 验证 `Revision` 仅允许未持久化的 `0` 或持久化后的正数，插入从 `1` 开始，成功更新严格递增 `1`。
- 验证 TMS/Parcel 的事件 schema 版本、策略版本、安全版本和资源授权版本与 `Revision` 相互独立。
- 验证并发更新使用单条条件 SQL；冲突后框架不自动重试，消费者自行决定重新加载和重新授权。
- 验证 `domain`、`repository` 和 `eventing` 只依赖 Go 标准库，`application` 不依赖适配器。
- 验证生产包不能导入 `testkit`，公共核心不能导入 `postgres` 或其子包。
- 验证公共 `testkit` 不依赖 `pgx/v5`、PostgreSQL 容器、OpenTelemetry SDK 或任何消费者包；PostgreSQL 专属夹具保持在适配器测试或 `internal`。
- 验证 `postgres/outbox` 与 `postgres/inbox` 不互相导入，也不包含消费者业务表、业务迁移或事件类型。
- 验证框架技术迁移能够安装到空库，并能够从上一受支持框架版本升级。
- 验证已发布迁移的稳定标识和校验和不可变化，消费者无需复制或编辑框架 SQL。
- 验证应用启动不会执行 DDL，运行账号不需要迁移权限；数据库 schema 不兼容时安全失败并给出只读诊断。
- 验证回滚、重复投递、版本冲突和适配器不可用时的失败行为。
- 对发布候选执行消费者拥有的最小真实切片冒烟合同，不扩大为完整产品端到端测试。
- 验证未配置 OpenTelemetry 时使用 no-op，适配器行为和错误语义不变。
- 验证 Trace Context 通过 Outbox/Inbox 连续传播，但无效上下文不会拒绝业务事件。
- 验证遥测导出失败不会导致事务回滚、事件重试或幂等结果变化。
- 验证默认 span、metric 和日志不包含租户、主体、聚合、事件载荷、SQL 参数、令牌或凭据。

## `testkit` 合同装配

- 框架 PR CI 使用业务无关夹具验证 `FixedClock`、`SequenceIDGenerator`、`RecordingPublisher`、`ValidEnvelope` 和合同运行器本身，不访问产品仓库。
- TMS、Parcel 各自在 `_test.go` 或专用合同测试包中，用自身强类型作用域键、聚合、显式 SQL Repository 和数据库生命周期实例化 `RunRepositoryContract`；业务类型和测试数据不进入框架。
- 适用 PostgreSQL 能力的消费者使用真实 `Transactor`、Outbox Store 和 Inbox Store 实例化 `RunOutboxContract`、`RunInboxContract`。公共运行器验证可移植状态机，`I-04` 继续验证 DDL、SQLSTATE、锁竞争、执行计划和多版本数据库。
- 每个合同子测试都通过工厂取得隔离 scenario，资源由工厂使用 `testing.T.Cleanup` 释放；公共运行器不调用 `t.Parallel`，不拥有数据库凭据或 schema 清理策略。
- 不允许以通用内存 Repository、Fake Transactor 或内存 Outbox/Inbox 的通过结果替代真实消费者/适配器合同。
- 消费者生产依赖图必须排除 `testkit`；只有测试文件与专用合同命令允许导入。该负向门禁的失败会阻断候选合同结果。

精确 API 与行为矩阵见 [`testkit` 公共合同决策简报](./testkit-contract.md)。

## 公共 API 兼容性合同

- 框架对公共包清单中的每个包生成 apidiff snapshot，并把稳定核心与实验性适配器分级；未登记导出和 snapshot 不可复现都会使 PR 失败。
- 核心与 PostgreSQL 编译夹具使用全部首版公共符号。完整导出面由 snapshot 保证，关键运行语义由框架和消费者合同保证。
- 每个受支持 Envelope SpecVersion 使用独立行为清单。仍受支持版本的旧合法 case 变为非法、wire 字段变化或稳定错误分类丢失时，候选失败。
- 不可变候选必须与上一发布版本比较；同时修改实现和当前 snapshot 不能抹去 apidiff 报告。
- `v0.x` minor 的受控破坏需要完整变化记录、迁移说明和两个消费者针对同一候选的通过结果；patch 不得包含已知破坏。
- TMS、Parcel 返回的合同证明记录 public manifest/合同套件版本，使结果能与候选公共面对应。

精确公共符号清单、制品布局与失败规则见[公共 API 与行为兼容性决策简报](./public-api-compatibility-contract.md)。

## CI 责任划分

| 流水线 | 所有者 | 必须执行 | 不负责 |
| --- | --- | --- | --- |
| 框架 PR CI | `idp-bento-go` | 单元测试、PostgreSQL 集成、架构规则、迁移、公共 API 和内置消费者夹具 | 访问产品源码、运行完整产品测试 |
| TMS `bento-contract` | IDP-TMS | 针对精确候选版本验证编译、核心契约、适用适配器和最小真实切片夹具 | Parcel 行为、框架发布操作 |
| Parcel `bento-contract` | IDP-Parcel | 针对精确候选版本验证编译、核心契约、适用适配器和最小真实切片夹具 | TMS 行为、框架发布操作 |
| 框架候选协调 | `idp-bento-go` | 校验两个合同结果针对同一候选并决定是否允许稳定 tag | 保存消费者源码、替代消费者判断 |
| 消费者升级 PR CI | 各消费者 | 对已发布精确版本运行完整产品 CI 并决定何时采用 | 反向决定框架发布时间 |

## 候选验证流程

1. 框架受保护 CI 使用 `candidatecheck` 和真实 PostgreSQL 16 执行仓内候选前置门禁；首个 `v0.1.0-rc.1` 显式使用 `-initial-v0-1`，后续候选必须提供独立不可变旧 `api/` 基线。
2. 仓内门禁通过后生成不可变预发布版本，例如 `v0.1.0-rc.1`，并记录 module 校验和。
3. TMS、Parcel 各自在自己的安全边界内获取该候选，不使用本地 `replace` 或浮动分支。
4. 两边从 `B-06` 批准的精确消费者提交运行版本化的 `bento-contract` 作业，只返回最小可追溯证明：消费者标识、消费者提交、候选版本、module 校验和、合同套件版本、结果和完成时间；严格 JSON v1 见[双消费者候选合同证明格式](./consumer-proof-format.md)。
5. 框架候选协调作业使用 `proofcheck` 核对两个结果及预期消费者提交。失败、超时、缺失、提交/候选/checksum/合同版本不一致或证明来源不可验证均阻止稳定 tag。
6. 稳定版本发布后，消费者分别通过正常依赖升级 PR 和完整产品 CI 决定采用时间；两者不需要同步上线。

仓内执行顺序、输入和失败规则见[候选前置门禁运行手册](./release-candidate-runbook.md)。`candidatecheck` 成功不等于已创建 RC，也不替代本节的双消费者证明；`proofcheck`/`evidencecheck` 只校验内容，不能替代[GitHub Actions 候选与发布合同](./github-actions-release-contract.md)和[WORM 发布证据与签名合同](./worm-release-evidence-contract.md)对来源、锁定、签名、超时和批准人的验证。

## 安全与隔离

- 消费者 CI 只获得读取候选 Go module 所需的最小权限；框架 CI 不获得消费者数据库、部署环境或业务仓库的长期读写权限。
- 如果 CI 平台需要跨仓触发，触发凭据只能具有指定工作流的触发与状态读取权限，并应使用短期凭据。
- 框架发布制品和日志不得包含消费者源码、测试数据、数据库快照、密钥或完整业务日志。
- 合同证明不是产品测试报告；它只证明指定消费者提交与指定框架候选之间的技术兼容性。

## 阻断规则

- 框架自身必需测试失败：阻断候选和稳定发布。
- 任一消费者最小合同失败、超时、缺失或版本不一致：允许继续产生修复用预发布候选，但阻断稳定发布。
- 消费者完整产品 CI 中与框架无关的失败：不阻断框架发布，由消费者在依赖升级 PR 中处理。
- 不允许静默跳过消费者合同后创建稳定 tag；任何紧急例外必须进入发布治理决策并留下可审计记录。
- 已知合同失败不能豁免；CI 平台故障时只能由相应消费者运行等价合同并提供可验证证明。
- `FULL` 与 `SECURITY` 状态的维护线都必须通过对应消费者基线的最小合同；`EOL` 版本不再进入发布矩阵。

## 未决问题

- GitHub Actions、`workflow_dispatch`、三年 Object Lock Compliance、GitHub OIDC、KMS ECDSA P-256 与 DSSE 已确认；真实消费者仓库/workflow ID、GitHub App、S3/KMS/OIDC 标识、Action SHA 和批准身份仍待外部绑定。

## 下一步

`R-01` 至 `R-06` 本地实现和 PostgreSQL 16.12 仓库验收已经完成。下一步创建并验证受保护 GitHub Actions、App 和 WORM 存储，再由 TMS、Parcel 对同一不可变候选执行本文件定义的最小合同。
