# PostgreSQL 迁移渲染与 schema 检查决策简报

状态：决定已确认；仓库内实现验收已通过；受保护 CI 待完成

Date: 2026-07-29

## 主题与范围

本简报确认 `R-04-D`：PostgreSQL 技术迁移的 schema 标识符规则、唯一模板 token、规范化 checksum、只读迁移清单、受控渲染，以及 `DB.CheckSchema` 的一致性快照、对象集合和错误语义。

本简报不创建迁移文件、不选择 TMS/Parcel 的迁移工具，也不提供迁移执行器或历史表。`R-04-E` 已验证候选模板算法与 PostgreSQL 行为；完整迁移资产及真实 API 测试属于 `I-04` 实现验收。

## 已验证事实

- PostgreSQL 标识符上限为 63 字节；超长标识符可能被服务端截断，不能依赖数据库事后发现。
- 自定义 schema 必须同时出现在表、函数、触发器函数体和错误诊断中；任意字符串替换会形成 SQL 注入和漏替换风险。
- 客户 schema 不同不应改变同一框架迁移的身份，因此 checksum 必须绑定规范模板而不是渲染结果。
- 迁移历史由消费者拥有，历史中出现某个 ID 不能证明表、约束、索引、函数和触发器仍与框架兼容。
- 对 schema 做完全对象相等检查会拒绝未来兼容的 expand 迁移，阻断新旧二进制过渡；启动检查应验证当前二进制依赖的必需对象子集。
- `CheckSchema` 是只读启动门禁，不是迁移执行器、权限审计器或任意数据库篡改检测器。

## 评估选项

### schema 标识符

1. 接受任意 PostgreSQL quoted identifier。表达能力最大，但大小写、Unicode、引号转义和不同迁移工具的规范化行为都会进入公共合同。
2. 只接受小写 ASCII 标识符，仍始终按 PostgreSQL identifier 引用。覆盖 `bento`、`customer_bento` 等私有化命名，同时保持渲染、日志和 catalog 查询确定。
3. 只允许固定 `bento`。最简单，但无法满足已有客户命名和权限约束。

采用选项 2。

### 迁移资产访问

1. 在 `Migration` 中公开 SQL 模板，由消费者自行替换 schema。容易误执行未渲染模板，也无法保护唯一渲染入口。
2. 清单只公开不可变元数据，消费者按迁移 ID 调用 `RenderMigration` 获取 SQL。框架始终从内置资产渲染，不信任调用方传回的可修改结构。
3. 框架直接执行迁移。能够隐藏模板，但夺走消费者的权限、历史、审批和生产编排所有权。

采用选项 2。

### schema 兼容性来源

1. 只读取消费者迁移历史。成本低，但历史和真实对象可能漂移。
2. 要求 catalog 与当前版本完全相等。检测最严格，但无法容纳兼容扩展和滚动升级。
3. 在同一只读一致性快照中验证当前二进制依赖的必需对象及定义，允许额外对象。既能拒绝缺失或被修改的关键结构，也保留 expand/contract 空间。

采用选项 3。

## 已确认决定

### schema 名称

schema 输入必须同时满足：

- 长度为 1 至 63 个 ASCII 字节。
- 匹配正则 `^[a-z_][a-z0-9_]{0,62}$`。
- 不以 `pg_` 开头。
- 不等于 `information_schema` 或 `public`。
- 不包含前后空白；框架不 trim、不转小写，也不做 Unicode 规范化。

`DefaultSchema` 继续为 `bento`。`WithSchema` 和 `RenderMigration` 必须调用同一个包内校验函数；`NewDB` 遇到非法 schema 时返回包装 `ErrInvalidConfig` 的错误。即使输入只含安全字符，渲染和 catalog 查询仍必须把 schema 作为一个 PostgreSQL identifier 引用，不能拼接裸字符串或依赖 `search_path`。

拒绝 `public` 用于保护已经确认的独立技术 schema 边界。客户环境若禁止创建 schema，必须由数据库管理员预置一个符合规则的专用 schema；框架仍不执行 `CREATE SCHEMA`。

### 唯一模板 token

迁移模板唯一允许的参数 token 为：

```text
{{IDP_BENTO_SCHEMA}}
```

规则如下：

- token 可以出现多次，但每次只能位于 PostgreSQL 标识符位置。
- 模板不得包含其他 `{{IDP_BENTO_*}}` token；不得为表名、列名、角色、tablespace 或任意 SQL 片段增加参数。
- 渲染时把每个 token 替换为经过校验并正确双引号引用的单一 schema identifier。
- Inbox 触发器函数的异常诊断使用 `TG_TABLE_SCHEMA`，不在字符串字面量中复用 identifier token。
- 渲染后仍存在已知 token 或出现未知框架 token 时返回迁移完整性错误，不返回部分 SQL。
- 模板和运行 SQL 全部使用完全限定对象名，不执行 `SET search_path`。

这种限制使 schema 是唯一变量，避免迁移资产退化为通用 SQL 模板引擎。

### 规范化 checksum

每个迁移 checksum 对规范模板计算，算法固定为：

1. 输入必须是有效 UTF-8。
2. 移除开头至多一个 UTF-8 BOM。
3. 把 `CRLF` 和孤立 `CR` 统一为 `LF`。
4. 移除文件末尾的全部 `LF`，再追加且只追加一个 `LF`。
5. 不修改其他字节；内部空格、空行、注释、语句顺序和 token 都参与校验。
6. 对结果计算 SHA-256，并编码为 `sha256:` 加 64 位小写十六进制。

`RenderMigration` 从同一规范模板产生结果，因此不同操作系统得到相同换行和末尾换行。checksum 不对 schema 特定的渲染结果计算；TMS、Parcel 的迁移历史必须记录框架提供的模板 checksum、迁移 ID、框架 module 版本和实际 schema。迁移工具可以另记渲染 SQL 的内容哈希，但它不能替代框架 checksum。

已经发布的 ID、规范模板或 checksum 永不修改。任何 SQL 修正都追加新迁移。manifest 保存固定 checksum；合同测试重新计算并比较，防止修改模板时自动更新 checksum 掩盖历史重写。

### 迁移清单与渲染 API

`postgres` 包的公共 API 固定为：

```go
package postgres

var (
	ErrMigrationNotFound  = errors.New("postgres: migration not found")
	ErrMigrationIntegrity = errors.New("postgres: migration integrity failure")
)

type Migration struct {
	ID                  string
	Checksum            string
	MinFrameworkVersion string
}

func Migrations() []Migration
func RenderMigration(migrationID string, schema string) (string, error)
```

首个迁移 ID 固定为 `000001_eventing_store`，`MinFrameworkVersion` 固定为 `v0.1.0`。它包含 `outbox`、`outbox_partition`、`inbox`、Inbox 约束触发器函数和触发器；实际 checksum 在规范模板成为实现资产时生成并锁定。

清单和渲染遵循：

- ID 使用六位十进制前缀、下划线和小写 snake_case 描述，按字节字典序即执行顺序；编号不得复用。
- `Migrations` 按 ID 严格递增，每次分配并返回新的 slice；调用方修改 slice 或元素不影响后续调用。
- `Migration` 不暴露 SQL 或模板字段，避免消费者跳过渲染入口或把修改后的结构传回框架。
- `RenderMigration` 只按 ID 从包内 manifest 查找资产。未知 ID 返回包装 `ErrMigrationNotFound` 的错误。
- 渲染前重新计算规范模板 checksum 并与 manifest 比较；不匹配、非法 UTF-8、非法 token 或 token 缺失返回包装 `ErrMigrationIntegrity` 的错误。
- 非法 schema 返回包装 `ErrInvalidConfig` 的错误。任何错误路径都不得返回部分 SQL。
- 框架不提供 `Up`、`Down`、`Migrate`、历史表或连接参数；消费者迁移作业按清单顺序渲染并执行。

这取代早期候选的 `Migration.SQL` 和 `RenderMigration(Migration, schema)` 形态。迁移模板是框架内部不可变资产，不是调用方可编辑的数据对象。

### `CheckSchema` 执行边界

`DB.CheckSchema(ctx)` 必须：

- 仅在启动门禁或显式诊断命令中调用，不自动进入 `NewDB` 或业务请求路径。
- 若 context 已包含任意框架事务绑定，返回包装 `application.ErrTransactionConflict` 的错误。
- 从所属 pool 开启独立的 PostgreSQL `REPEATABLE READ READ ONLY` 事务，在一个 catalog 快照中完成全部检查，随后回滚该只读事务。
- 不执行 DDL、DML、`LOCK TABLE`、权限变更、schema 修复或迁移历史写入。
- catalog 查询、权限不足、连接失败、取消和超时保留原基础设施错误；只有确认对象缺失或定义不兼容时才包装 `ErrSchemaIncompatible`。
- 收集同一快照内的全部兼容性问题，按对象类别和名称稳定排序后返回；错误文本可以包含 schema、对象类别、对象名和不匹配属性，但不得包含连接串、SQL 参数或凭据。

`server_encoding` 必须为 `UTF8`。检查不绑定数据库 owner、schema owner、角色名、OID、统计信息、表空间、物理大小或当前序列值，这些属于部署环境而不是持久化合同。

### 必需关系与列

目标 schema 必须存在。以下三个关系必须是永久普通表，且未启用或强制 Row-Level Security：

- `outbox`
- `outbox_partition`
- `inbox`

必需列以 `R-04-B` 规范 DDL 为语义来源。catalog manifest 必须核对名称、PostgreSQL 类型、typmod、是否允许 NULL、identity/generated 属性和默认表达式：

| 表 | 必需列 |
| --- | --- |
| `outbox` | `source text NOT NULL`、`event_id text NOT NULL`、`partition_key text NOT NULL`、`enqueued_seq bigint GENERATED ALWAYS AS IDENTITY`、`status text NOT NULL`、`available_at timestamptz NOT NULL`、`lease_token uuid`、`lease_expires_at timestamptz`、`attempt bigint NOT NULL DEFAULT 0`、`failures bigint NOT NULL DEFAULT 0`、`published_at timestamptz`、`abandoned_at timestamptz`、`last_failed_at timestamptz`、`failure_code text`、`spec_version text NOT NULL`、`event_type text NOT NULL`、`event_version bigint NOT NULL`、`scope text NOT NULL`、`subject text NOT NULL`、`occurred_at timestamptz NOT NULL`、`recorded_at timestamptz NOT NULL`、`correlation_id text`、`causation_id text`、`traceparent text`、`tracestate text`、`content_type text NOT NULL`、`payload jsonb NOT NULL` |
| `outbox_partition` | `partition_key text NOT NULL`、`head_enqueued_seq bigint NOT NULL`、`claimable_at timestamptz NOT NULL` |
| `inbox` | `consumer text NOT NULL`、`source text NOT NULL`、`event_id text NOT NULL`、`status text NOT NULL`、`received_at timestamptz NOT NULL`、`settled_at timestamptz`、`failure_code text` |

列的物理顺序不是运行合同，不进入检查。`enqueued_seq` 的 identity backing sequence 必须存在、归属该列、数据类型为 `bigint`、递增量为 `1`、最小值为 `1` 且不循环；当前值和 cache 大小不检查。

### 必需约束与索引

所有必需约束必须存在于正确表、已验证且定义匹配规范 DDL。主键、唯一键和外键核对类型、列及顺序；外键还核对目标表/列、更新删除动作和 deferrable 属性；CHECK 使用按受支持 PostgreSQL major 固定并经过空白规范化的 `pg_get_constraintdef` 签名。`v0.1` 初始只支持 major 16，实际实现须在 `I-04` 锁定签名。

必需约束名称为：

| 表 | 约束 |
| --- | --- |
| `outbox` | `outbox_pk`、`outbox_partition_seq_uq`、`outbox_source_chk`、`outbox_event_id_chk`、`outbox_partition_key_chk`、`outbox_enqueued_seq_chk`、`outbox_status_chk`、`outbox_attempt_chk`、`outbox_failures_chk`、`outbox_nonpending_attempt_chk`、`outbox_failure_code_chk`、`outbox_failure_history_chk`、`outbox_state_shape_chk`、`outbox_spec_version_chk`、`outbox_event_type_chk`、`outbox_event_version_chk`、`outbox_scope_chk`、`outbox_subject_chk`、`outbox_correlation_id_chk`、`outbox_causation_id_chk`、`outbox_traceparent_chk`、`outbox_tracestate_chk`、`outbox_content_type_chk`、`outbox_payload_object_chk` |
| `outbox_partition` | `outbox_partition_pk`、`outbox_partition_key_chk`、`outbox_partition_head_seq_chk`、`outbox_partition_head_fk` |
| `inbox` | `inbox_pk`、`inbox_consumer_chk`、`inbox_source_chk`、`inbox_event_id_chk`、`inbox_status_chk`、`inbox_failure_code_chk`、`inbox_state_shape_chk`、`inbox_settled_time_chk` |

必需索引必须有效、ready、使用预期访问方法，并匹配唯一性、键列顺序、包含列和谓词：

- `outbox_pk`：唯一 B-tree，`(source, event_id)`，无谓词。
- `outbox_partition_seq_uq`：唯一 B-tree，`(partition_key, enqueued_seq)`，无谓词。
- `outbox_active_recorded_idx`：非唯一 B-tree，`(recorded_at, enqueued_seq)`，谓词为 `status IN ('PENDING', 'LEASED')`。
- `outbox_partition_pk`：唯一 B-tree，`(partition_key)`，无谓词。
- `outbox_partition_claimable_idx`：非唯一 B-tree，`(claimable_at, partition_key)`，无谓词。
- `inbox_pk`：唯一 B-tree，`(consumer, source, event_id)`，无谓词。

索引和 CHECK 的文本签名只在经过 `pg_get_expr`/`pg_get_indexdef` 规范化后比较；不得比较 OID、自动生成的 identity sequence 名称或无关显示空白。

### 必需函数与触发器

目标 schema 必须包含零参数函数 `assert_inbox_settled_before_commit()`：

- 返回 `trigger`，语言为 `plpgsql`，`SECURITY INVOKER`，非 leakproof。
- volatility 为 `VOLATILE`，parallel 属性为 `UNSAFE`，且没有函数级 `SET` 配置。
- 规范化函数体与迁移 manifest 中锁定的 SHA-256 一致；函数体使用完全限定的 Inbox 表并通过 `TG_TABLE_SCHEMA` 设置异常诊断 schema。

`inbox` 必须包含 `inbox_settled_before_commit` constraint trigger：

- `AFTER INSERT OR UPDATE`、`FOR EACH ROW`。
- `DEFERRABLE INITIALLY DEFERRED`。
- 已启用，并调用目标 schema 中上述零参数函数。

函数 owner 和 OID 不检查。额外函数、触发器、索引、约束和向后兼容列不会因为“额外存在”而失败；`CheckSchema` 只证明当前二进制的必需子集。框架发布测试必须另外证明任何新增对象不会破坏仍受支持的旧二进制。

## 最小测试合同

- schema 校验接受 `bento`、`customer_bento` 和 63 字节边界，拒绝空值、首字符数字、大写、连字符、Unicode、64 字节、`pg_` 前缀、`public` 和 `information_schema`。
- token 只在 identifier 位置完整替换；非法 schema、未知 ID、checksum 漂移、未知 token、缺少 token 和无效 UTF-8 均不返回部分 SQL。
- 同一模板在 LF、CRLF、CR、可选 BOM 和末尾换行差异下产生相同 checksum；内部空格、注释或语句变化产生不同 checksum。
- `Migrations` 顺序稳定、无重复 ID，每次返回防御性复制；调用方修改结果不影响后续调用。
- 空库安装使用消费者预置 schema 和渲染 SQL 成功；模板 SQL 不能在未渲染时执行。
- `CheckSchema` 对规范空库返回 nil，全程不产生 DDL/DML 或持久化副作用。
- 缺失或改变任一必需表、列、约束、索引、函数、触发器、identity 属性以及非 UTF8 数据库均返回可通过 `errors.Is` 识别的 `ErrSchemaIncompatible`。
- 取消、超时、权限不足和连接失败不被误分类为 `ErrSchemaIncompatible`。
- 增加兼容表、可空列或索引不会因额外对象失败；每个受支持 PostgreSQL major 的 catalog 签名由集成测试固定。
- 传入已有事务 context 时返回 `application.ErrTransactionConflict`；检查在一个只读 REPEATABLE READ 快照内完成。

## 主要代价与缓解

- 小写 ASCII schema 规则拒绝部分合法 PostgreSQL 名称；换取确定渲染、跨工具兼容和更小的注入面，私有化客户仍可使用命名空间前缀。
- catalog 签名对 PostgreSQL major 有兼容成本；`v0.1` 只确认 major 16，扩展其他 major 前必须重跑 `I-04`，不假设跨 major 文本输出相同。
- 必需子集检查不能识别所有额外对象的副作用；框架 schema 不允许消费者私改，向前兼容由不可变迁移和旧二进制升级测试证明。
- 启动检查需要若干 catalog 查询；它只在启动门禁执行并使用一致性快照，不进入业务热路径。

## 后续门禁

`R-04-D` 完成。`F-30` 更新为 `CONFIRMED`，新增 `F-34` 记录 `CheckSchema` 必需对象子集与只读一致性快照。真实迁移、checksum、空库安装、规范/漂移 schema 检查已通过 PostgreSQL 16.12 仓库验收；受保护 CI 复跑仍依赖 `B-05`。

`R-05 testkit` 与 `R-06` 公共兼容性基线已确认，迁移所有权决定已经进入 [ADR-0004](../adr/0004-framework-migrations-consumer-execution.md)；真实迁移和 `CheckSchema` 进入 `I-04` 实现验收。
