# PostgreSQL `R-04-E` 可复现设计证据

状态：设计证据已确认；仓库内实现验收已通过；受保护 CI 证明待完成

Date: 2026-07-29

## 主题与结论

本文保存 `R-04-E` 的 PostgreSQL 设计阶段证据：规范 DDL、schema 渲染、约束负向路径、Outbox 并发锁序、Claim 并行、Finalize 条件谓词、Inbox 有界等待、catalog 负向变体和 10 万分区执行计划。

本轮结论：

- `R-04` 的架构与数据库设计已经达到可直接编码状态，不再保持 `DESIGN-BLOCKER`。
- `F-29` 的分区状态表、`claimable_at` 索引和并发 SQL 有 PostgreSQL 16.12 可复现证据，可以改为 `CONFIRMED`。
- 在本设计证据形成时，尚不存在的 Go 迁移资产、`CheckSchema` 和 Store 实现不能被描述为已经测试；它们当时进入 `I-04` 实现验收门禁。2026-07-30 的真实实现结果见下方追记。
- 本轮没有创建 `.go`、`.sql`、`go.mod`、Git 仓库或正式迁移文件；可执行 SQL 的规范来源仍是三个 `R-04` 决策简报。

## 2026-07-30 实现验收追记

设计阶段与实现阶段继续保持分层。2026-07-30 创建真实 Go 实现和迁移资产后，仓库内 `I-04` 使用以下环境重跑：

| 项目 | 值 |
| --- | --- |
| PostgreSQL | `PostgreSQL 16.12 on x86_64-pc-linux-musl` |
| 镜像 | `postgres:16.12-alpine` |
| 镜像摘要 | `postgres@sha256:97ff59a4e30e08d1c11bdcd9455e7832368c0572b576c9092cde2df4ae5552a3` |
| 平台 | `amd64/linux` |
| `server_encoding` | `UTF8` |
| `default_transaction_isolation` | `read committed` |
| 迁移模板 checksum | `sha256:71eaecb1597bf0b2048ad80328dd03dea9aa8a8cc4f8eda571907bc9f35b4397` |

可复跑入口为：

```powershell
$env:IDP_BENTO_POSTGRES_DSN = '<postgres-16-dsn>'
go test ./postgres -run 'TestPostgres16(Contracts|I04)' -count=1 -v
go test ./...
go vet ./...
go run ./internal/cmd/apisnapshot -require-all
```

仓库证据对应关系：

- `postgres/integration_test.go` 安装真实迁移、执行 `CheckSchema`，并以 `testkit` 公共运行器验证五组 Outbox 与五组 Inbox 合同。
- `postgres/i04_test.go` 验证严格事务参与、DDL/SQLSTATE 负向、兼容扩展、schema 漂移、三类各 20 轮竞争、旧租约三终结拒绝、分区指针修复、Inbox 锁超时恢复和 10 万分区计划。
- `postgres/outbox/sql_test.go` 固定 Claim、MaxAttempts 与三个 Finalizer 的关键条件谓词；删除状态、时间、token、队首或计数边界会使测试失败。
- `compat/compile/postgres` 使用三个 PostgreSQL 包的全部首版公共构造和方法；`api/snapshots/postgres*.apidiff` 保存三个实验性包的无位置 API 基线。
- 官方 apidiff 对八个公共包逐一比较当前实现均报告 `no changes`，`apisnapshot -require-all` 报告 snapshots clean。

本次本地矩阵全部通过，并在执行中发现并修复了 `CheckSchema` trigger catalog 查询使用 PostgreSQL 保留字 `constraint` 作为别名的问题。首版不存在上一迁移版本，升级测试记录为 `not applicable`；下一项迁移必须增加从上一受支持版本升级的用例。

该追记不是受保护 CI 证明。`B-05` 的 GitHub Actions、WORM、OIDC 和 DSSE 配置已经确认，但真实 workflow、身份、S3/KMS 标识和演练尚未绑定，因此 CI run ID、候选 checksum 与 TMS/Parcel 同一候选结果仍不存在，并继续阻断 RC 和稳定发布。

## 为什么拆分设计证据与实现验收

原门禁要求在开始实现前保存实际迁移、`CheckSchema` 和 Store 的合同测试，但这些对象只有实现后才存在。这会形成循环依赖。

评估过三个选项：

1. 在正式实现前先创建一次性迁移和测试代码。会产生第二套待丢弃实现，并违反当前只完善文档的范围。
2. 取消 PostgreSQL 证据门禁。无法保护并发、迁移和 catalog 合同。
3. 现在保存独立于 Go 实现的数据库设计证据；实现后用同一矩阵验证真实公共 API 和资产。

采用选项 3。设计证据证明已确认 SQL 和 DDL 在目标数据库上的行为；实现验收证明代码忠实实现这些合同。两者不能互相替代。

## 环境指纹

| 项目 | 值 |
| --- | --- |
| 容器名称 | `idp-bento-r04e-20260729` |
| PostgreSQL | `16.12 (Debian 16.12-1.pgdg13+1)` |
| 平台 | `x86_64-pc-linux-gnu` |
| 镜像配置摘要 | `sha256:e4842c8a99ca99339e1693e6fe5fe62c7becb31991f066f989047dfb2fbf47af` |
| `server_encoding` | `UTF8` |
| `default_transaction_isolation` | `read committed` |
| `max_connections` | `100` |
| `shared_buffers` | `128MB` |
| `block_size` | `8192` |
| 性能数据集 | 100,000 条 Outbox、100,000 个活跃分区，每分区 1 条事件 |

时间数据只用于证明本机执行方向，不是性能 SLO，也不能直接外推到客户硬件和生产数据分布。

## 规范来源

- [PostgreSQL 技术表 DDL 决策简报](./postgres-schema-ddl-decision-brief.md) 是表、列、约束、索引、函数和触发器的语义来源。
- [PostgreSQL 并发 SQL 与锁顺序决策简报](./postgres-concurrency-sql-decision-brief.md) 是 Enqueue、Claim、Finalize、Inbox 和指针修复语句的语义来源。
- [PostgreSQL 迁移渲染与 schema 检查决策简报](./postgres-migration-schema-decision-brief.md) 是 schema token、checksum 和 catalog manifest 的语义来源。

DDL 通过结构化读取第一个 `sql` 围栏取得。测试 schema 使用两个步骤渲染：先把 `SCHEMA = 'bento'` 改为 `SCHEMA = TG_TABLE_SCHEMA`，再把 identifier 位置的 `bento` 替换为目标 schema。该过程只服务无正式迁移文件阶段的设计验证；消费者仍必须使用未来的 `RenderMigration`，不得复制此逻辑。

核心提取方式为：

```powershell
$doc = Get-Content -Raw -Encoding UTF8 `
    'docs/design/postgres-schema-ddl-decision-brief.md'
$match = [regex]::Match(
    $doc,
    '(?ms)^```sql\r?\n(.*?)^```[ \t]*\r?$'
)
$ddl = $match.Groups[1].Value.Replace(
    "SCHEMA = 'bento'",
    'SCHEMA = TG_TABLE_SCHEMA'
) -replace '\bbento\b', 'r04e_render'
```

复现者必须先核对服务端版本和镜像摘要，使用 `psql -X -v ON_ERROR_STOP=1`，并为并发场景使用独立数据库连接。

## DDL 与迁移形状证据

规范 DDL 在预置 schema 后完整执行，得到：

| 对象 | 数量或属性 |
| --- | --- |
| 永久普通表 | `3` |
| 命名约束 | `37` |
| 索引，含约束 backing index | `6` |
| 触发器函数 | `1` |
| 非内部触发器 | `inbox_settled_before_commit` |
| 触发器属性 | `DEFERRABLE=true`、`INITIALLY DEFERRED=true` |

正负向结果：

| 场景 | 结果 |
| --- | --- |
| `INSERT PROCESSING -> UPDATE PROCESSED -> COMMIT` | 成功，最终只有 `PROCESSED` |
| 提交遗留 `PROCESSING` | `23514`，约束 `inbox_settled_before_commit` |
| `TG_TABLE_SCHEMA` 诊断 | `SCHEMA NAME: r04e_render` |
| 跨分区队首指针 | `23503`，约束 `outbox_partition_head_fk` |
| 258 字节多字节 `event_type` | `23514`，约束 `outbox_event_type_chk` |
| JSON array Payload | `23514`，约束 `outbox_payload_object_chk` |

## schema 与 checksum 证据

schema 边界夹具结果：

- 接受：`bento`、`customer_bento`、63 字节边界。
- 拒绝：空值、数字开头、大写、连字符、Unicode、64 字节、`pg_` 前缀、`public`、`information_schema`。

使用规范 DDL 构造的候选模板为 8,791 个规范字节。该值和以下 checksum 只是算法夹具，不是尚未创建的首个正式迁移 checksum：

| 输入变体 | SHA-256 |
| --- | --- |
| LF | `sha256:71eaecb1597bf0b2048ad80328dd03dea9aa8a8cc4f8eda571907bc9f35b4397` |
| CRLF 加额外末尾换行 | 与 LF 相同 |
| UTF-8 BOM | 与 LF 相同 |
| 内部 SQL 空格变化 | `sha256:c2c7c1d4d05951a688e8821d11ee9cdc7c414a475b82dc03953f58aa39df8158` |

这证明 BOM、传输换行和末尾换行差异被规范化，内部内容变化仍会改变 checksum。正式迁移创建时必须重新生成并锁定 manifest checksum，不能复制这里的算法夹具值作为发布值。

## 并发调度与结果

### 同分区 Enqueue

事务调度：

| 时点 | 会话 A | 会话 B | 会话 C |
| --- | --- | --- | --- |
| T0 | 取得 `same` advisory lock，插入 A |  |  |
| T0 + 250ms | 持锁并等待提交 | 请求 `same` advisory lock | 取得 `other` advisory lock并提交 C |
| T0 + 1.2s | 提交 | 取得锁，插入 RecordedAt 更早的 B 并提交 | 已完成 |

结果：

- 不同分区 C 用时约 `142ms`。
- 同分区 B 用时约 `1106ms`，确认等待 A 的事务级 advisory lock。
- A 的 `enqueued_seq=1`，B 的 `enqueued_seq=3`；B 的 `RecordedAt` 更早仍不改变序号。
- `same` 队首保持 A。

### 双 Claim worker

两个会话都按 `(claimable_at, partition_key)` 使用 `FOR UPDATE SKIP LOCKED` 锁一个分区，再按 `outbox_partition -> outbox` 锁序写入不同 lease。

单轮结果：

- Worker A 领取 `same`，token 为 `11111111-1111-4111-8111-111111111111`。
- Worker B 在 A 未提交时领取 `other`，token 为 `22222222-2222-4222-8222-222222222222`。
- Worker B 用时约 `141ms`，没有等待 A 的分区行。

重复压力结果：

- 轮数：`20`。
- 重复领取或租约数量错误：`0`。
- 第二 worker 最大用时：`247ms`。

### Finalize、重排与指针

| 场景 | 结果 |
| --- | --- |
| MarkPublished 当前队首 | A 进入 `PUBLISHED`，指针推进到 B |
| 使用旧 token 终结 B | 条件更新影响 `0` 行，B 保持 `PENDING` |
| RecordFailure | 状态回到 `PENDING`，`Attempt=1`、`Failures=1`，分区时间与 `available_at` 相等 |
| 指针人为指向后续事件 | 修复到最早非终态序号，本轮不产生 Delivery |
| `Attempt=MaxAttempts` | 原子进入 `ABANDONED`，空分区行删除 |
| 最后队首终结与新 Enqueue 并发 | 新 Enqueue 等待约 `1226ms`，旧事件 `PUBLISHED`，新事件 `PENDING` 且成为新指针 |

谓词删除变体：

- 完整状态/token/时间条件对错误目标影响 `0` 行；删除这些条件后影响 `1` 行并错误进入 `PUBLISHED`。
- 完整队首条件对非队首租约影响 `0` 行；删除队首条件后影响 `1` 行并错误进入 `PUBLISHED`。

这些变体证明谓词是正确性条件，不是可以为性能移除的重复检查。

### Inbox 有界等待

会话 A 插入同一 InboxKey 的 `PROCESSING`，在同一事务更新为 `PROCESSED`，保持事务约 1.2 秒。会话 B 设置事务局部 `lock_timeout=100ms` 后尝试同键 `INSERT ... ON CONFLICT DO NOTHING`。

结果：

- 会话 B 退出码为 `3`，SQLSTATE 为 `55P03`。
- 包含进程启动与 `psql` 开销的外部计时约 `262ms`。
- A 提交后只存在一个 `PROCESSED` 行；B 的失败事务没有留下 `PROCESSING`。

`100ms` 是数据库锁等待配置，不把本机外部进程总耗时定义为 API SLA。

## catalog 负向变体

所有变体都在事务中制造并回滚：

| 变体 | catalog 信号 |
| --- | --- |
| 删除 `outbox_active_recorded_idx` | `to_regclass` 返回 NULL |
| 把同名 `outbox_spec_version_chk` 改成 `CHECK (true)` | `pg_get_constraintdef` 返回 `CHECK (true)` |
| 禁用 `inbox_settled_before_commit` | `tgenabled='D'` |
| 回滚后复查 | 索引存在、约束 validated、触发器 `tgenabled='O'` |

只读检查事务确认运行于 `repeatable read` 且 `transaction_read_only=on`。真实 `CheckSchema` 实现后必须把这些变体变成 API 级负向合同测试，并通过 `errors.Is(err, ErrSchemaIncompatible)` 断言。

## 执行计划证据

数据准备耗时仅作环境记录：

- 插入 100,000 条 Outbox：约 `585.787ms`。
- 插入 100,000 个分区：约 `679.387ms`。
- Outbox `ANALYZE`：约 `78.494ms`。
- 分区 `ANALYZE`：约 `15.828ms`。

Claim 分区扫描，`Limit=100`：

```text
Index Scan using outbox_partition_claimable_idx
  Filter: (claimable_at <= clock_timestamp())
Execution Time: 0.114 ms
Buffers: shared hit=203
```

单分区真实队首定位：

```text
Index Scan using outbox_partition_seq_uq
  Index Cond: (partition_key = 'partition-050000'::text)
  Filter: (status = ANY ('{PENDING,LEASED}'::text[]))
Execution Time: 0.020 ms
Buffers: shared hit=5
```

两条计划分别命中 `R-04-B` 确认的分区扫描索引和分区序号唯一索引，没有退化为顺序扫描。实现阶段必须保存机器可读的完整计划输出，并只断言关键访问路径，不把本机毫秒数作为脆弱测试。

## `I-04` 实现验收门禁

以下项目只能在创建真实实现后完成，任何文档结论都不能提前替代：

- 实际 `000001_eventing_store` 模板、manifest 和发布 checksum 的完整性测试。
- `Migrations()` 防御性复制、按 ID 渲染、非法 token/schema/UTF-8 和未知 ID 错误测试。
- 空库安装和从上一受支持框架版本升级；首版没有前序版本时记录 `not applicable`，不能伪造升级结果。
- 真实 `DB.CheckSchema` 对规范 schema、本文 catalog 负向变体、基础设施错误和已有事务 context 的合同测试。
- 真实 Store 对 Enqueue、Claim、Finalize、Inbox、错误映射和影响行数的测试。
- 20 轮以上并发测试及谓词删除 mutation tests。
- PostgreSQL 16 最新受支持补丁的 CI；扩展到其他 major 前必须在每个 major 重跑全部矩阵。
- 最终 CI 保存 PostgreSQL 版本、镜像摘要、数据库参数、随机种子、完整日志和关键执行计划。

以上条目在设计证据形成时均未完成。2026-07-30 追记已经完成仓库内实现矩阵；受保护 CI 与候选证明仍未完成，因此可以声明本地 PostgreSQL 适配器实现与仓库验收完成，但不得声明发布级 `I-04`、RC 或稳定版本完成。

## 支持范围与限制

- `v0.1` 初始 PostgreSQL 支持范围固定为 major `16`，CI 使用当时最新稳定 `16.x` 补丁；当前设计证据版本为 `16.12`。
- 本轮只验证 PostgreSQL 16.12，不推断 17、18 或未来 major 的 catalog 文本和锁行为。
- 10 万个单事件分区验证索引方向，不覆盖热点分区、宽 Payload、长历史或生产 I/O 饱和。
- advisory lock 哈希碰撞只在文档中完成语义分析，尚未制造已知碰撞夹具；实现测试应补充碰撞只降低并发、不破坏正确性的场景。
- 外部进程计时包含 Docker 和 `psql` 开销，不作为数据库内部耗时或生产容量结论。

## 后续门禁

`R-04-E` 的设计证据完成，`R-04`、`R-05 testkit` 与 `R-06` 公共兼容性基线均已确认；正式 module path 已绑定，最终实施决策简报已经生成。

`I-04` 仓库内实现矩阵已完成；`B-05` 未完全绑定使受保护 CI 证明保持未完成并阻断 RC 与发布。设计证据与真实 Go 实现验收仍分别记录，不互相替代。
