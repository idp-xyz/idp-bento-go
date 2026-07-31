# `idp-bento-go` 实施就绪审计

状态：已确认

Audit date: 2026-07-31

## 结论

框架的产品边界、仓库边界、Repository 策略、迁移所有权、消费者合同、治理、版本支持、可观测性以及 `R-01` 至 `R-06` 的公共设计合同已完成。五个稳定核心包、三个实验性 PostgreSQL 包、迁移资产、`testkit` 合同运行器和本地兼容性制品均已实现。

当前仓库已经达到“开发人员无需补充公共架构判断即可使用和继续加固 `v0.1` 基线”的标准。PostgreSQL 16.12 仓库矩阵和未受保护的基础 GitHub Actions CI 已经通过，实际 Git 仓库和 vanity 解析合同已确认；受保护 CI、线上解析部署、候选治理和双消费者证明仍由外部绑定阻断。这些是发布与组织门禁，不是遗留设计歧义。

## 当前实现状态

| 范围 | 状态 | 证据或剩余工作 |
| --- | --- | --- |
| `domain`、`repository`、`application`、`eventing` | 已实现 | 单元测试、核心编译夹具和逐包 API 快照已建立 |
| `testkit` | 已实现稳定核心 | 确定性替身与三个公共合同运行器已实现；Outbox 覆盖重试、租约、失败/尝试预算和深拷贝，Inbox 覆盖非法参数、终态、并发占用和 Consumer 隔离 |
| `R-06` 仓内门禁 | 已实现 | 八个无绝对路径 `.apidiff` 快照、核心/PostgreSQL 编译夹具、132-case Envelope 行为清单、API/行为负向夹具、独立旧基线比较器和生产依赖图门禁已实现；真实上一发布版本比较要到产生首个后继候选时执行 |
| `postgres`、`postgres/outbox`、`postgres/inbox` | 已实现 | DB/Transactor、迁移、`CheckSchema`、Outbox/Inbox Store、单元测试、编译夹具和三个 API 快照已建立 |
| `I-04` | 仓库矩阵和基础 CI 通过 | PostgreSQL 16.12 本地矩阵已通过；基础 `Framework CI` run `30563832922` 又在 PostgreSQL 16.12 上通过全部 18 个门禁，但未受 Ruleset 保护，不能作为发布级证据 |
| 候选仓内入口 | 已实现并在基础 CI 验证 | `candidatecheck` 固定精确 Go/module、module/格式、快照/行为/依赖、八包 apidiff、静态分析和无缓存 PostgreSQL 全量测试；真实受保护 CI 运行仍依赖 `B-05` |
| 双消费者证明校验 | 已实现内容校验 | 严格 JSON v1 与 `proofcheck` 已覆盖身份、预期 TMS/Parcel 提交、候选/checksum/合同版本、PASS 和时间；证明来源、签名、超时及存储仍依赖 `B-04`/`B-05` |
| 跨仓候选与发布 | 配置已确认，部署未验证 | Git 仓库和 vanity 解析合同已确认；`B-01B` 的线上验收、`B-04` 至 `B-06` 和真实 TMS/Parcel 候选证明仍未完成 |

## 已达到可编码精度的部分

- 独立仓库、Go module、SemVer、消费者精确版本锁定和双消费者治理边界。
- 公共包与依赖方向：`domain`、`repository`、`eventing`、`application`、`testkit`；实验性 `postgres`、`postgres/outbox`、`postgres/inbox`。
- Repository 小能力接口、强类型作用域键原则、错误分类和 `Revision` 并发语义。
- `domain` 不提供业务模型基类，只提供领域事件元数据与 `EventBuffer` 的精确公共合同。
- `application` 的 Handler、Clock、IDGenerator、Transactor、事务传播、严格模式和失败语义，含提交结果不确定后的恢复路径。
- `eventing` 的版本化 JSON 信封、边界强制校验、Publisher 错误、按分区有序 Outbox、双计数器与重试终态、有界等待事务性 Inbox 和幂等键。
- PostgreSQL 事务使用私有 carrier 与窄化 Querier/Executor；DB/Store 构造签名、只读路径、运行策略所有权、`MaxAttempts` 与 `RecordFailure` 原子边界已经确认。
- PostgreSQL 三张技术表的完整字段、命名约束、索引、分区指针复合外键和 Inbox 提交约束触发器已经确认。
- PostgreSQL READ COMMITTED、Enqueue advisory lock、统一行锁顺序、Claim/Finalize/Inbox 完整 SQL、迁移渲染、schema 检查和错误映射已经确认，并具有 PostgreSQL 16.12 设计证据。
- PostgreSQL 技术迁移由框架拥有资产、消费者拥有执行的责任边界。
- `testkit` 的不可变固定时钟、并发安全顺序 ID、深拷贝记录式 Publisher、合法信封、Repository/Outbox/Inbox 工厂式合同运行器，以及生产依赖隔离门禁。
- 公共包/符号 allowlist、逐包 apidiff snapshot、编译夹具、版本化 Envelope 行为清单、变化记录和双消费者候选门禁；同一 SpecVersion 不得收紧规则。
- 框架 CI、TMS/Parcel `bento-contract` 与稳定发布门禁。
- 维护组、评审、受保护发布、紧急例外和版本支持窗口。
- 不自建可观测性 Port，适配器使用 OpenTelemetry API 与 `log/slog` 的边界。

## 设计门禁

以下设计项目已经完成，开发人员不得在实现中重新选择或扩张公共边界。

| ID | 缺口 | 必须明确的内容 | 状态 |
| --- | --- | --- | --- |
| `R-01` | `domain` 公共合同 | 不提供业务模型基类；使用 `EventType`、`EventVersion`、`Event` 和 `EventBuffer` | `CONFIRMED` |
| `R-02` | `application` 与事务合同 | 泛型 Handler、Clock、IDGenerator、Transactor、Required 传播、rollback-only、严格模式和提交不确定语义 | `CONFIRMED` |
| `R-03` | `eventing` 公共合同 | 事件信封精确字段与强制校验点、事件类型与版本规则、Publisher 错误、Outbox/Inbox 状态机、双计数器、幂等键和重试/放弃语义 | `CONFIRMED` |
| `R-04` | PostgreSQL 适配器合同 | DB/Store、完整 DDL、并发 SQL、迁移/schema 检查与 PostgreSQL 16.12 设计证据均已固定 | `CONFIRMED` |
| `R-05` | `testkit` 公共合同 | 固定时钟/顺序 ID、记录式 Publisher、合法信封、真实端口合同运行器与 public/internal 边界 | `CONFIRMED` |
| `R-06` | 公共 API 兼容性基线 | 公共符号 allowlist、apidiff snapshot、编译夹具、版本化行为清单、版本比较和破坏性变化记录 | `CONFIRMED` |

### `R-04` 证据要求

`R-04-A`（公共 API 与发布编排所有权）已经确认：PostgreSQL Store 只拥有原子持久化，消费者拥有发布进程运行策略，领取防崩溃上限通过 `OutboxClaim.MaxAttempts` 显式传入。

`R-04-B`（完整 DDL）已经确认：三张技术表的字段、状态形状、命名约束和索引固定；`outbox_partition` 使用 `(partition_key, head_enqueued_seq)` 复合外键防止跨分区或悬空指针；Inbox 使用 `DEFERRABLE INITIALLY DEFERRED` constraint trigger 阻止 `PROCESSING` 提交。

`R-04-C`（并发 SQL）已经确认：事务显式使用 READ COMMITTED；同分区 Enqueue 通过事务级 advisory lock 解决空分区竞态；实际行锁固定为分区行到 Outbox 行；Claim 使用有界 Limit 与 `FOR UPDATE SKIP LOCKED`；Finalize 和 Inbox 的完整条件 SQL、影响行数及错误映射已经固定。

`R-04-D`（迁移/schema 检查）已经确认：schema 使用受限小写 ASCII 标识符和唯一模板 token；checksum 对规范模板计算；清单只返回防御性元数据副本并按 ID 渲染；`CheckSchema` 在只读一致性快照中验证 UTF8 编码和当前二进制的必需对象子集。

`R-04-E`（设计证据）已经在 PostgreSQL 16.12 保存环境指纹、DDL/约束负向结果、同分区 Enqueue、20 轮双 Claim worker、Finalize 谓词删除、指针修复、最后队首并发、Inbox busy、catalog 变体和 10 万分区执行计划。真实 Go 实现随后使用仓库测试重跑了对应矩阵。

`I-04`（实现验收）的仓库矩阵已经通过：真实迁移与 `CheckSchema`、Outbox/Inbox 公共合同、严格事务、SQLSTATE、20 轮以上竞争、旧租约、指针修复、关键谓词删除守卫和 10 万分区计划均由测试覆盖。`B-05` 未完全绑定前仍无法形成受保护 CI 证明，因此候选与稳定发布门禁保持关闭。

### `R-06` 兼容性要求

`R-06` 已确认四层门禁：逐包 apidiff snapshot 覆盖全部导出声明；业务无关编译夹具证明首版调用形态；每个 Envelope SpecVersion 的规范 JSON manifest 保护常量、wire、校验接受集合和错误分类；不可变候选最终由 TMS、Parcel 真实合同验证。

`v0.1` 新增 `SpecVersionV1 = "1.0"`，`SpecVersion` 作为当前写入版本别名。Validate、Unmarshal、Enqueue 和 Publish 按信封自身版本分派，新增版本不能立即停止接受旧 Outbox 记录。Repository 公共 allowlist 不包含通用 `ScopedKey`，强类型作用域键继续由消费者拥有。

仓内负向验收已经证明删除函数、增加接口方法、收窄泛型约束、改变结构字段、删除公共包、收紧 Envelope 长度/Payload、改变 wire 字段和丢失 `ErrUnsupportedSpecVersion` 分类均会失败。`go run ./internal/cmd/dependencycheck` 已验证当前 7 个生产包，并用真实故意导入 `testkit` 的测试夹具证明失败路径；`compatcheck` 要求旧基线与候选为两个不同目录且不提供更新入口。

## 外部绑定

这些项目不需要重新讨论框架边界，但必须在对应阶段绑定真实值。

| ID | 项目 | 何时必须绑定 | 当前状态 |
| --- | --- | --- | --- |
| `B-01A` | 正式 Go module path | 创建 `go.mod` 前 | `BOUND`：`go.idp.xyz/idp-bento-go`；不再阻断本地代码脚手架 |
| `B-01B` | 实际 Git 地址及 `go.idp.xyz` 的 vanity metadata 或私有 GOPROXY 路由 | 首次跨仓获取候选前 | `CONFIGURATION BOUND / DEPLOYMENT UNVERIFIED`：Git 仓库为 `https://github.com/idp-xyz/idp-bento-go`；`v0.1` 使用 HTTPS vanity metadata 直连该仓库，不引入私有 GOPROXY；2026-07-30 实测 `go.idp.xyz` 为 `NXDOMAIN`，TLS/元数据和空缓存真实 RC 下载尚不可执行 |
| `B-02` | 精确 Go、`pgx/v5`、OpenTelemetry、apidiff 与 SemVer 解析版本 | 创建代码基线与依赖锁时 | `BOUND`：Go `1.26.5`、pgx `v5.10.0`、OpenTelemetry API `v1.44.0`、`x/exp v0.0.0-20260727155853-b88d891fe743`、`x/mod v0.38.0`；2026-07-29 由官方端点核验，`x/mod` 原已由工具链间接锁定，现由证明校验直接使用 |
| `B-03` | PostgreSQL 验证主版本范围 | 首个适配器集成测试前 | `BOUND`：`v0.1` 初始支持 major 16；CI 使用最新稳定 16.x，当前证据为 16.12 |
| `B-04` | 维护者、消费者代表、安全批准者账号 | 开放共享合并与发布候选前 | `UNBOUND IDENTITIES`：公司 Organization `idp-xyz` 已绑定，但真实维护者、消费者代表和安全批准者账号尚未绑定 |
| `B-05` | CI 平台、跨仓触发与合同证明存储 | 首个跨仓发布候选前 | `BASIC CI DEPLOYED / RELEASE DEPLOYMENT UNVERIFIED`：基础 `.github/workflows/ci.yml` 已固定 Action SHA、Go 1.26.5 与 PostgreSQL 16.12 digest，并在提交 `c780c14212aa9ebd367fb9b0379e7d9562360f5a` 的 run `30563832922` 通过；当前明确保持 GitHub Free，私有仓库 Ruleset 不执行，所以该 run 不是受保护发布证据。`workflow_dispatch`、职责分离 GitHub App、WORM/OIDC/KMS/DSSE、Environment、消费者协调和演练仍未部署；Team 或等价受保护平台只在共享合并或首个 RC 前强制选择 |
| `B-06` | 首发日期、支持矩阵日期和消费者基线提交 | 首个稳定版本前 | `UNBOUND` |

`go.mod`、稳定核心和 PostgreSQL 本地基线已经创建，Git 仓库地址和 vanity 解析合同已经确认；当前没有剩余架构决定阻断代码加固。`B-01B` 剩余的线上部署验收及 `B-04` 至 `B-06` 分别阻断跨仓获取、共享合并、受保护 CI、发布候选或稳定发布。

## 明确不阻断 `v0.1` 的后置选择

- 实际 Kafka、Pulsar、NATS 或其他消息产品。
- HTTP Router 和消费者传输层框架。
- TMS、Parcel 各自采用的迁移执行工具。
- OpenTelemetry SDK、Exporter、Collector 后端和 `idp-obs/sdk-go` 是否由消费者装配。
- 商业支持 SLA、Dashboard、告警平台和客户环境部署方式。

## 审计修正

- 删除“可观测性不进入 `v0.1`”的陈旧表述；当前决定是可观测能力进入实验性适配器，但不新增自有 Port 或公共包。
- 将消费者合同从“候选层级”改为已确认的 `v0.1` 最小合同矩阵。
- 维护者真实账号不再作为本地设计与合同实现前置条件，只在开放共享合并与发布候选前强制绑定。
- 撤销 `F-03`：本地开发目录是工作区事实而不是架构决定；编号不重复使用。
- `R-03` 在首次标记 `CONFIRMED` 后经复审补入四项缺口：信封校验的强制执行点、Outbox 双计数器、分区串行代价说明、Inbox 有界等待与 `ErrInboxBusy` 强制回滚。补入后重新确认。
- 补入 `R-02` 的事务参与者严格模式与隐式传播论证，消除"租户禁止隐式而事务允许隐式"看似矛盾的表述。
- 删除“Revision 冲突可以证明命令已经提交”和“生产侧提交结果不确定可以由 Inbox 解决”的错误推论；自动恢复必须具备稳定 CommandID 与事务性命令处理记录或等价业务幂等证明。
- `R-05` 拒绝通用内存 Repository/Outbox/Inbox 和 Fake Transactor；公共测试工具固定为确定性替身与工厂驱动真实端口的合同运行器，PostgreSQL 专属夹具保持在适配器测试或 `internal`。
- `R-06` 拒绝单层编译/文本快照，固定 apidiff、编译、行为和真实消费者四层门禁；snapshot 更新不得替代上一版本比较。
- 公共符号审计删除了未形成精确合同的通用 `ScopedKey`，并补入历史 Envelope SpecVersion 并存规则，消除升级后旧 Outbox 记录被判无效的风险。
- 最终实施决策简报已经确认，仓库设计状态改为 `Confirmed`；未完成项继续按 `UNBOUND` 或 `I-04` 管理，不能被确认状态掩盖。
- 将原 `B-01` 拆分为 `B-01A` 模块身份与 `B-01B` 获取基础设施；前者已绑定，后者不得继续误阻断本地脚手架。
- 2026-07-30 先将 `B-01B` 的实际 Git 仓库绑定为个人命名空间下的 `https://github.com/idp-repo/idp-bento-go`，随后创建公司 Organization `idp-xyz` 并迁移为 `https://github.com/idp-xyz/idp-bento-go`；`v0.1` 使用 HTTPS vanity metadata 直连最终地址且不引入私有 GOPROXY。DNS/TLS/线上元数据和真实 RC 空缓存下载仍待验证。
- 2026-07-30 确认 `B-05` 使用 GitHub Actions 和 `workflow_dispatch`，GitHub Artifact 只作证明传输，权威证据必须进入 WORM；同时补强 `proofcheck`，强制证明提交等于 `B-06` 预期 TMS/Parcel 提交。真实平台标识与存储仍未绑定。
- 2026-07-30 进一步确认 S3-compatible Object Lock `COMPLIANCE` 三年保留、GitHub OIDC、KMS ECDSA P-256 与 DSSE 发布授权，并新增 `evidencecheck` 验证 manifest、原始证明摘要、GitHub 来源和时间窗；`B-05` 配置选择完成，真实部署仍未验证。
- 2026-07-30 公开线上核验确认框架 repository ID `1315735832`、公开可见性和默认分支 `main`；当前 owner ID `310467973` 的类型为个人 `User`，与治理文档原称“GitHub 组织已绑定”冲突，因此组织所有权改为部署阻断项。workflow、Ruleset、Environment 与 tag 均为空，分支保护因未认证只能标记为未验证；`go.idp.xyz` 同时确认为 `NXDOMAIN`，所以 `B-01B` 与 `B-05` 状态不升级。
- 2026-07-30 登录后的 GitHub 设置页进一步确认当前账号没有 Organization，且个人账号不能转换为 Organization；唯一受支持的治理迁移路径是新建公司 Organization 后转移 `idp-bento-go`。目标名称和新 owner ID 在实际创建前保持 `UNBOUND`。
- 2026-07-30 创建免费 Organization `idp-xyz` 并完成仓库转移；repository ID 保持 `1315735832`，owner ID 更新为 `311024230`，旧 GitHub URL 自动重定向。迁移后 workflow、Ruleset、Environment、tag 均为 0，默认分支保护明确未配置，因此组织所有权已绑定但发布控制面仍未部署。
- 2026-07-30 将仓库切换为私有，保留远端 Boost Software License 1.0 初始提交，并把通过本地门禁的 113 个源码与文档文件推送到 `main`；远端提交更新为 `6da1e17e74eeecc9ec120d646642f2f303f4ccbd`。同日使用一次性 PostgreSQL 16.9 容器通过 `candidatecheck -initial-v0-1` 的全部 18 个门禁。Ruleset 页面明确提示免费 Organization 不执行私有仓库规则；Team 升级停在账单资料和付款之前，因此不得把默认分支描述为已保护。
- 2026-07-31 部署基础 `Framework CI`，固定第三方 Action 完整 SHA、Go 1.26.5 与 PostgreSQL 16.12 manifest digest。首次 run `30560728007` 暴露 Windows 生成的 `testkit` export data 与 Linux runner 不一致；提交 `c780c14212aa9ebd367fb9b0379e7d9562360f5a` 将 snapshot 生成和当前包加载统一到 `linux/amd64`、`CGO_ENABLED=0`，run `30563832922` 随后通过全部 18 个门禁。用户确认保持 GitHub Free、不购买 Team；该 CI 未受 Ruleset 保护，不能授权 RC 或稳定发布。

## 推荐完成顺序

1. 已完成：创建 `go.mod`、锁定 `B-02`、实现五个稳定核心包及核心兼容性制品。
2. 已完成：实现 `postgres`、`postgres/outbox`、`postgres/inbox`、不可变迁移、PostgreSQL 编译夹具与三个 API 快照。
3. 已完成仓库矩阵：在 PostgreSQL 16.12 上执行 `I-04`；受保护 CI 复跑仍依赖 `B-05`。
4. 已完成仓内候选入口：`candidatecheck` 强制真实 PostgreSQL DSN、无缓存测试和显式初始/旧基线模式。
5. 已完成私有远端源码基线：保留初始许可证历史，将完整源码推送到 `idp-xyz/idp-bento-go` 的 `main`，并核验远端提交。
6. 已完成基础 CI：未受保护的 GitHub Actions 在精确 Go/PostgreSQL 基线上通过全部 18 个门禁。
7. 下一阶段：保持 GitHub Free，先部署并验收 `B-01B` vanity 解析，绑定 `B-04`/`B-06` 身份与消费者基线，并准备 TMS、Parcel 合同入口；开放共享合并或创建首个 RC 前，再选择 GitHub Team 或满足同一保护合同的等价平台，建立 Ruleset、Environment、WORM 和发布身份。

## 下一步

`v0.1` 实现基线、PostgreSQL 16.12 仓库验收、私有 Git 远端源码基线、基础 GitHub Actions CI 和 vanity 解析合同已经完成。下一推荐阶段是部署 `go.idp.xyz` 解析基础设施并绑定治理身份与消费者基线；受保护平台的付费或迁移选择推迟到开放共享合并或首个 RC 之前，不影响当前继续开发。
