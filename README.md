# idp-bento-go

状态：已确认

`idp-bento-go` 是由 IDP-TMS 与 IDP-Parcel 共同复用、与业务无关的 Go DDD+EDA 技术框架。

框架是独立技术组件，不作为任何产品的宿主，不拥有产品领域模型，不共享产品业务数据库，也不协调产品发布周期。

## 当前实现进度

- 已实现并测试 `domain`、`repository`、`application`、`eventing` 和 `testkit` 稳定核心。
- 已实现 `postgres`、`postgres/outbox`、`postgres/inbox`、不可变迁移、只读 `CheckSchema`、PostgreSQL 编译夹具和真实端口集成测试。
- 已建立八个公共包的无本机绝对路径 `.apidiff` 快照、两个编译夹具，以及 132-case 的 Envelope v1 行为清单。
- 仓库内 `I-04` 矩阵已在 PostgreSQL 16.12 上通过；2026-07-31，基础 GitHub Actions `Framework CI` 又在 PostgreSQL 16.12 上通过 `candidatecheck -initial-v0-1` 的全部 18 个门禁（run `30563832922`）。该 workflow 未受 Ruleset 保护，只是开发反馈，不是 RC 或稳定发布授权。
- 私有远端仓库已经接收完整源码，`main` 当前指向 `c780c14212aa9ebd367fb9b0379e7d9562360f5a`。当前明确保持 GitHub Free，不购买 Team；免费 Organization 不执行私有仓库 Ruleset，因此受保护默认分支、解析基础设施、维护者身份、RC 和 TMS/Parcel 同一候选合同尚未完成，当前仍不是稳定版本发布完成。Team 或等价受保护平台只需在开放共享合并或首个 RC 前选择。

## 已确认基线

- 独立 Git 仓库、Go module、CI、版本和发布生命周期。
- 正式 Go module path 固定为 `go.idp.xyz/idp-bento-go`；实际 Git 仓库已绑定为 [idp-xyz/idp-bento-go](https://github.com/idp-xyz/idp-bento-go)，`v0.1` 使用 HTTPS vanity metadata 直连该仓库，不引入私有 GOPROXY。
- 工具链与基础依赖基线：Go `1.26.5`、`pgx/v5 v5.10.0`、OpenTelemetry API `v1.44.0`、`x/exp v0.0.0-20260727155853-b88d891fe743`；均在 2026-07-29 通过官方端点核验。
- 发布模式：从 `v0.x` 开始使用 SemVer；消费者锁定精确版本。
- 首批消费者：IDP-TMS 与 IDP-Parcel，各自保持独立业务、数据、运行和发布边界。
- 发布候选必须通过框架测试及 TMS、Parcel 消费者合同测试。
- 只有两个消费者的真实切片都验证稳定核心 API 后，才允许发布 `v1.0.0`。
- `v0.1` 稳定核心包为 `domain`、`application`、`repository`、`eventing` 和 `testkit`；`postgres`、`postgres/outbox`、`postgres/inbox` 在 `v0.x` 阶段属于实验性适配器。
- 框架拥有技术表的不可变迁移资产，消费者拥有迁移执行、权限、历史、业务迁移和生产编排；应用启动时不自动改库。
- 框架稳定发布需要 TMS、Parcel 在各自 CI 中通过同一候选版本的最小消费者合同；框架不接管两个产品的完整 CI 或业务源码。
- 框架由不少于 3 人的独立维护组治理；高风险公共变更需要双维护者和双消费者评审，稳定版本只能由受保护 CI 发布。
- 版本支持窗口：`v0.x` 前一 minor 关键修复 90 天；`v1` 被替代 minor 关键修复 6 个月；未来 `v2` 发布后最新 `v1` minor 关键修复 12 个月。
- `v0.1` 不自建可观测性 Port；实验性适配器使用 OpenTelemetry API 和 `log/slog`，消费者拥有 SDK、Exporter、Collector、采样和凭据。
- 事务句柄通过派生 context 隐式传播，业务作用域与授权身份必须显式传参；写事务参与者在缺少事务句柄时返回错误，读取能力显式选择 ReadExecutor 或 RequireExecutor。
- 集成事件信封在写入 Outbox 和发布两处强制校验；校验规则收紧属于破坏性变更，只能通过新 `SpecVersion` 引入。
- Outbox 以独立计数器区分领取次数与显式记录的可重试发布调用失败（含结果不确定），租约过期不消耗失败预算；Inbox 并发占用超时返回 `ErrInboxBusy` 并强制回滚。
- `eventing` 使用版本化 JSON 信封、单条 Publisher、At-Least-Once Outbox 与事务性 Inbox；同一分区保持顺序，不声称 Exactly-Once。
- PostgreSQL 三张技术表的完整 DDL 已确认：分区指针使用同分区复合外键，Inbox 使用延迟约束触发器禁止提交 `PROCESSING` 中间态。
- PostgreSQL 并发 SQL 已确认：READ COMMITTED、Enqueue 事务级 advisory lock、分区行到 Outbox 行的统一锁顺序，以及有界 Claim/Finalize/Inbox 条件更新。
- `testkit` 使用不可变固定时钟、并发安全的顺序 ID、深拷贝记录式 Publisher，以及工厂驱动真实实现的 Repository/Outbox/Inbox 合同运行器；不提供通用内存 Repository 或 PostgreSQL 公共测试基础设施。
- 公共兼容性使用 apidiff snapshot、编译夹具、版本化 Envelope 行为清单和双消费者候选合同；更新当前 snapshot 不能替代与上一发布版本比较。

## 非目标

- 产品业务实体、工作流、角色、权限或数据库表。
- TMS 与 Parcel 共用运行时、数据库或部署单元。
- 逐项复制 Python `idp-bento` API 的 Go 版本。
- 通用 ORM、低代码平台、工作流引擎或 Event Sourcing 平台。
- 没有真实消费者或公司级基础设施需求证明的框架抽象。

## `R-04` 当前状态

- PostgreSQL 技术表使用专用 schema 与 `outbox`、`outbox_partition`、`inbox` 三张表，完整字段、约束和索引已经确认。
- 分区状态表使用可重新进入扫描的 `claimable_at`；并发 Enqueue、领取、终结和指针修复的完整 SQL 已确认。
- 迁移资产通过 `embed.FS`、防御性元数据清单、唯一 schema token 和按 ID 受控渲染 API 发布；checksum 对规范模板计算。
- PostgreSQL DB/Store、完整 DDL、并发 SQL、迁移渲染和 schema 检查已经实现；仓库内 `I-04` 在 PostgreSQL 16.12 上覆盖真实迁移、合同运行器、20 轮竞争、负向约束、schema 漂移和 10 万分区执行计划并通过。

## `R-05` 当前状态

- `FixedClock`、`SequenceIDGenerator`、`RecordingPublisher` 和 `ValidEnvelope` 的精确 API、取消、耗尽、并发与深拷贝语义已经确认。
- Repository、Outbox、Inbox 通过公共工厂式合同运行器验证真实端口；合同运行器不导入 `pgx`，也不提供内存 Store。
- PostgreSQL 容器、迁移、锁竞争、SQLSTATE 和执行计划夹具保持在实验性适配器测试或 `internal`；`go run ./internal/cmd/dependencycheck` 已对非测试生产依赖图阻止导入 `testkit`、核心反向依赖 PostgreSQL 和 Outbox/Inbox 相互依赖。
- `R-05` 稳定核心已实现；Outbox 与 Inbox 合同运行器已经对 PostgreSQL 真实端口执行并通过。

## `R-06` 当前状态

- 首版稳定核心与实验性适配器的公共包、符号 allowlist 和稳定性分级已经固定；`repository` 不提供通用 `ScopedKey`。
- Go 声明面使用精确锁定的 apidiff tool dependency 和无位置逐包 `.apidiff` snapshot，行为面使用业务无关编译夹具与规范 JSON manifest。
- `SpecVersionV1` 永久表示 `1.0`，`SpecVersion` 表示当前写入版本；校验按信封自身版本分派，升级不能让旧 Outbox 记录突然失效。
- `R-06` 仓内门禁已覆盖八个公共包、核心/PostgreSQL 编译夹具、Envelope 行为基线、API/行为负向夹具和生产依赖图；`compatcheck` 强制使用独立不可变旧基线，并拒绝用候选 `-update` 洗掉历史破坏。首个版本尚无上一发布版本，真实上一版本比较、变化记录审批和跨仓候选门禁仍待首次 RC 阶段执行。
- `go run ./internal/cmd/candidatecheck -initial-v0-1` 是首个候选的平台无关仓内入口；它要求真实 PostgreSQL DSN，并以 `-count=1` 运行全部测试。后继候选必须改用 `-baseline <不可变前序 api 目录>`。
- `go run ./internal/cmd/proofcheck ...` 严格核对 TMS、Parcel 是否从 `B-06` 预期提交对同一候选版本、module checksum 和合同套件返回 `PASS`；它不验证平台来源或签名，也不能在 `B-04`/`B-05` 未完全绑定时授权发布。
- `go run ./internal/cmd/evidencecheck ...` 严格核对 release evidence manifest、两份原始证明、GitHub 来源字段、内容摘要、API 比较身份和协调时间窗；Object Lock、OIDC、KMS 与 DSSE 仍由真实平台验收。

## 文档

- [框架基线](./docs/design/framework-baseline.md)
- [`domain` 包公共合同](./docs/design/domain-contract.md)
- [`application` 与事务公共合同](./docs/design/application-transaction-contract.md)
- [`eventing`、Outbox 与 Inbox 公共合同](./docs/design/eventing-contract.md)
- [PostgreSQL 适配器公共合同](./docs/design/postgres-adapter-contract.md)
- [PostgreSQL 公共 API 与发布编排所有权决策简报](./docs/design/postgres-api-ownership-decision-brief.md)
- [PostgreSQL 技术表 DDL 决策简报](./docs/design/postgres-schema-ddl-decision-brief.md)
- [PostgreSQL 并发 SQL 与锁顺序决策简报](./docs/design/postgres-concurrency-sql-decision-brief.md)
- [PostgreSQL 迁移渲染与 schema 检查决策简报](./docs/design/postgres-migration-schema-decision-brief.md)
- [PostgreSQL `R-04-E` 可复现设计证据](./docs/design/postgres-r04e-reproducibility-evidence.md)
- [`testkit` 公共合同决策简报](./docs/design/testkit-contract.md)
- [公共 API 与行为兼容性决策简报](./docs/design/public-api-compatibility-contract.md)
- [候选前置门禁运行手册](./docs/design/release-candidate-runbook.md)
- [`go.idp.xyz` module 解析合同](./docs/design/module-resolution-contract.md)
- [GitHub Actions 候选与发布合同](./docs/design/github-actions-release-contract.md)
- [WORM 发布证据与签名合同](./docs/design/worm-release-evidence-contract.md)
- [双消费者候选合同证明格式](./docs/design/consumer-proof-format.md)
- [包边界](./docs/design/package-boundaries.md)
- [消费者合同策略](./docs/design/consumer-contracts.md)
- [维护与发布治理](./docs/design/maintenance-governance.md)
- [版本兼容性与支持策略](./docs/design/version-support-policy.md)
- [可观测性边界](./docs/design/observability.md)
- [实施就绪审计](./docs/design/implementation-readiness-audit.md)
- [`v0.1` 最终实施决策简报](./docs/design/v0.1-implementation-decision-brief.md)
- [ADR 索引](./docs/adr/README.md)

`R-01` 至 `R-06` 的公共设计合同和本地代码基线均已实现，PostgreSQL 16.12 仓库内验收已通过。下一阶段是部署并验证 `B-01B` 的 vanity 解析、绑定 `B-04` 至 `B-06`，建立受保护 CI 与不可变 RC，再收集 TMS、Parcel 对同一候选 checksum 的合同证明。
