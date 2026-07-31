# `idp-bento-go` 项目速览

> 事实核对日期：2026-07-31，全部数字基于当前 checkout 实测。

## 一句话定位

`idp-bento-go` 是 IDP-TMS 与 IDP-Parcel 共用的、与业务完全无关的 Go DDD + EDA 技术框架。它不是应用，不含任何业务逻辑，只提供端口、值类型、PostgreSQL 适配器和发布兼容性门禁。

## 关键坐标

| 项目 | 值 |
| --- | --- |
| Module path | `go.idp.xyz/idp-bento-go` |
| Git 仓库 | `https://github.com/idp-xyz/idp-bento-go`（私有，Organization `idp-xyz`） |
| 解析方式 | `go.idp.xyz` HTTPS vanity metadata 直连 GitHub，不引入私有 GOPROXY |
| Go 工具链 | 精确 `go1.26.5`（`go.mod` 声明 `go 1.26.0` + `toolchain go1.26.5`） |
| 当前版本 | `v0.1.0-rc.2`（不可变候选，非正式发布；前序候选 `v0.1.0-rc.1` 保留） |
| 许可证 | Boost Software License 1.0 |
| 生产依赖 | `pgx/v5 v5.10.0`、OpenTelemetry API `v1.44.0`、`x/exp`、`x/mod v0.38.0`、`x/tools v0.48.0` |

## 代码规模总览

Go 源码合计 **10 304 行**，其中非测试 6 844 行。框架实现与"发布治理机器"的比例接近一比一：

| 区块 | 源码 | 测试 | 合计 |
| --- | ---: | ---: | ---: |
| 五个稳定核心包 | 1 408 | 437 | 1 845 |
| PostgreSQL 适配器（`postgres` 全树） | 2 592 | 1 429 | 4 021 |
| 发布治理与兼容性（`internal/` 全树） | — | — | 4 183 |
| 消费者编译夹具（`compat/compile`） | 255 | 0 | 255 |

`internal/` 中门禁命令占 2 131 行（源码 1 331 + 测试 800），其余 2 052 行是被命令复用的比较、证明与证据支撑包。

## 一、五个稳定核心包

全部符号只依赖 Go 标准库——`go list -deps` 对这五个包的外部依赖结果为空。核心包几乎只有接口和值类型，聚合、ID、业务键、鉴权全部由消费者拥有。

| 包 | 源码 | 内容 |
| --- | ---: | --- |
| `domain` | 67 行 | `Event` 接口（`Type`/`Version`/`OccurredAt`）、`EventBuffer` 事件缓冲区（`Record`/`Pending`/`Acknowledge`）。非法元数据不返回错误而是直接 panic，共四处：nil 事件、空 type、零 version、零发生时间 |
| `repository` | 39 行 | 泛型端口 `Loader[K,A]`、`Inserter[A]`、`VersionedUpdater[K,A]`，`Revision` 乐观锁，三个哨兵错误 `ErrNotFound`/`ErrAlreadyExists`/`ErrConflict` |
| `application` | 75 行 | 用例入口 `Handler[Request,Response]` 与 `HandlerFunc`、事务边界 `Transactor`/`TxFunc`、泛型包装 `Transactional[Result]`、`Clock` 与 `IDGenerator[ID]` 端口 |
| `eventing` | 366 行 | 集成事件信封 `Envelope`（`SpecVersionV1 = "1.0"`，自带 `Validate`/`Clone`/JSON 编解码）+ Outbox/Inbox 全套端口：`Publisher`、`OutboxWriter`/`OutboxClaimer`/`OutboxFinalizer`、`InboxStarter`/`InboxCompleter`，含全部长度上限常量与错误集 |
| `testkit` | 861 行 | 确定性替身（`FixedClock`、`SequenceIDGenerator`、`RecordingPublisher`、`ValidEnvelope`）+ 三个工厂式合同运行器 `RunRepositoryContract`/`RunOutboxContract`/`RunInboxContract`，用来验证真实端口实现；不提供任何内存实现 |

`testkit` 的 861 行分布为 `contracts.go` 703 行、`publisher.go` 92 行、`deterministic.go` 59 行、`time.go` 7 行。

## 二、PostgreSQL 适配器（`v0.x` 实验性，不承诺稳定）

| 包 | 源码 | 内容 |
| --- | ---: | --- |
| `postgres` | 1 092 行 | `DB`（封装 pgxpool）、`Executor`/`Querier` 抽象；`transaction.go` 用派生 context 隐式传播事务句柄，写操作缺句柄直接返回 `ErrTransactionRequired`；`schema.go` 提供只读 `CheckSchema` 漂移检测；`migrations.go` 用 `embed.FS` + `{{IDP_BENTO_SCHEMA}}` token 按 ID 受控渲染不可变迁移 |
| `postgres/outbox` | 1 029 行 | 共 9 个文件，其中 `enqueue.go`/`claim.go`/`finalize.go`/`partition.go` 是四个操作文件，另有 `store.go`、`sql.go`、`validation.go` 及测试。事务级 advisory lock、READ COMMITTED、同分区顺序保证、租约机制，并把"领取次数"与"失败次数"拆成独立计数器 |
| `postgres/inbox` | 471 行 | 并发占用超时返回 `ErrInboxBusy`；靠 `DEFERRABLE INITIALLY DEFERRED` 约束触发器禁止 `PROCESSING` 中间态提交 |

迁移资产只有一份 `postgres/migrations/000001_eventing_store.sql`，建三张表 `outbox`、`outbox_partition`、`inbox`，约束相当严格：状态形状 CHECK、指向同分区头指针的复合外键、`failure_code` 正则、提交前断言触发器。框架只提供迁移资产，执行、权限与编排归消费者，应用启动不自动改库。

## 三、发布治理与兼容性门禁

`internal/cmd/` 下 8 个自研门禁命令：

| 命令 | 源码 | 含测试 | 职责 |
| --- | ---: | ---: | --- |
| `candidatecheck` | 249 | 455 | 候选总入口，串起 18 个门禁：工具链必须 `go1.26.5`、module 身份、`mod tidy -diff`、gofmt、快照、行为清单、依赖图、8 个包的 apidiff、`go vet`、`-count=1` 全量测试。强制真实 PostgreSQL DSN，不允许跳过 |
| `compatmanifest` | 528 | 617 | Envelope v1 行为清单，`api/behavior/envelope-v1.json` 实测 132 个 case |
| `apisnapshot` | 250 | 361 | 生成逐包 `.apidiff` 快照 |
| `evidencecheck` | 81 | 244 | 校验 WORM 发布证据清单、原始证明摘要与时间窗 |
| `compatcheck` | 67 | 140 | 用独立不可变旧基线与当前候选比较 |
| `proofcheck` | 60 | 165 | 校验 TMS 与 Parcel 对同一候选 checksum 的合同证明 |
| `apicheck` | 53 | 106 | 校验快照与当前公共 API 一致 |
| `dependencycheck` | 43 | 43 | 阻止生产依赖图导入 `testkit`、核心反向依赖 PostgreSQL、Outbox 与 Inbox 互相依赖 |

`apisnapshot`、`apicheck`、`compatcheck` 共用 `internal/compatibility`，由它把加载环境固定为 `GOOS=linux`、`GOARCH=amd64`、`CGO_ENABLED=0`，避免快照随生成机器漂移。

`api/snapshots/` 覆盖 8 个公共包：`domain`、`repository`、`application`、`eventing`、`testkit`、`postgres`、`postgres-outbox`、`postgres-inbox`。快照比较强制使用独立不可变旧基线，禁止用候选自身 `-update` 洗掉破坏记录。

CI（`.github/workflows/ci.yml`）只有一步：跑 `candidatecheck -initial-v0-1`，PostgreSQL service 固定为 `postgres:16.12-alpine@sha256:97ff59a4…`，Go 固定 `1.26.5`，第三方 Action 全部 SHA 锁定。

## 四、文档与基础设施

- `docs/design/` 24 份设计合同，`docs/adr/` 9 条已接受 ADR（独立仓库、核心/适配器分离、context 事务传播、迁移所有权、Outbox/Inbox 一致性、testkit 边界、四层兼容性门禁、稳定 module path、WORM + OIDC 发布证据）。
- `infra/vanity/` 是 `go.idp.xyz` vanity metadata 的 nginx 部署（Dockerfile、compose、宿主配置、加固脚本），让 module identity 与 GitHub 仓库解耦。
- `compat/compile/{core,postgres}` 是消费者视角的编译夹具。

## 五、当前状态

功能面已完成：`R-01` 至 `R-06` 全部落地，PostgreSQL 16.12 上 `I-04` 矩阵通过，`af68525` 上的基础 CI run `30616180483` 通过全部 18 个门禁。

跨仓获取链路已经打通并可复现：

| 验收项 | 结果 |
| --- | --- |
| DNS / TLS / metadata | `go.idp.xyz A 8.133.22.7`，Let's Encrypt ECDSA 证书有效期至 2026-10-29，根路径与真实子包返回唯一精确 `go-import` |
| 不可变候选 | `v0.1.0-rc.1` → `af68525`；`v0.1.0-rc.2` → `56322dc`，后者以 `-baseline` 模式通过 19 个门禁且公共 API 与前者一致 |
| 空缓存真实下载 | rc.1 两次独立执行返回相同 `Sum` `h1:lx/snQkwUQW5pWPOgNI4AsZrSxxi0YYqHfOemsgEuLQ=`；rc.2 返回 `Sum` `h1:W9T1KsbXwuVs3lHTNwSUcEBY0Ids00hUH7I9KuBtCEo=`，两者 `GoModSum` 同为 `h1:RFR6ylLNIIA7e4PPGVCzojYiH6DB8eHd2s5vI9XcUgY=` |
| TMS 消费者合同 | `f8eb9c3` 建立 `TBC-01`~`TBC-05`，`7661603` 补齐 `TBC-06`~`TBC-08`，`37d3209` 改锁 rc.2；八个用例在 PostgreSQL 16.14 上以 `-count=1` 全部通过 |
| Parcel 消费者合同 | `960a516` 建立合同，`1ce7c01` 改锁 rc.2；`PBC-01`、`PBC-05`、`PBC-06`、`PBC-08` 同样在 PostgreSQL 16.14 上全部通过 |

两个消费者使用各自的编号方案而非同一方案留空：`PBC-01`/`PBC-05` 对应 `TBC-01`/`TBC-02`，`PBC-06` 合并了 `TBC-06`+`TBC-07`，`PBC-08` 合并了 `TBC-05`+`TBC-08`。真实覆盖差异只有 `TBC-03`（EventBuffer 所有权）与 `TBC-04`（RecordingPublisher 确定性），都是核心包语义而非适配器行为，是否需要两侧都覆盖尚未确认。

跳过路径受门禁保护：两侧 `internal/platform/pgtest` 在 `CI` 已设置而 DSN 缺失时直接 `Fatal`，只有本地开发降级为 `t.Skip`。因此受保护 CI 中不存在"跳过却报 PASS"的静默通过路径，但本地绿色结果同样不能当作消费证明。

| 双消费者证明 | TMS `1942bb1`、Parcel `9c73c42` 产出真实证明文件，`proofcheck` 对 rc.2 返回通过；证明来自本地运行，不满足受信任工作流来源要求 |

剩余阻断：

- 证明必须来自 TMS/Parcel 各自的受信任工作流而非本地运行，并写入不可变审计存储。这一条与下面的平台问题同源。
- 仓库仍在 GitHub Free，免费 Organization 不执行私有仓库 Ruleset，因此受保护分支、受保护候选 workflow、Environment、WORM 证据与 DSSE 发布授权（`B-05`）均未部署，现有 CI run 不能作为发布级证据。
- 维护者、消费者代表与安全批准者身份（`B-04`）和首发日期、支持矩阵、消费者基线提交（`B-06`）未绑定。

因此 `v0.1.0-rc.1` 只能被描述为不可变候选，不得当作已发布的 `v0.x`。

## 六、消费者使用边界

- 只通过正式 module path `go.idp.xyz/idp-bento-go` 和不可变 SemVer 获取，不复制源码、不使用浮动分支、生产环境不使用 `replace`。
- 空缓存获取环境用 `GOPRIVATE=go.idp.xyz` + `GOPROXY=direct` + `GIT_TERMINAL_PROMPT=0`；禁止用 `GOSUMDB=off` 全局关闭 checksum database 代替 `GOPRIVATE`，那会一并放弃公共依赖和 `golang.org/toolchain` 的校验。
- 迁移 SQL 由消费者自行执行与编排，框架不在应用启动时改库。
- `postgres*` 属于 `v0.x` 实验层，可能在 minor 之间破坏；只有五个核心包受兼容性合同保护。
