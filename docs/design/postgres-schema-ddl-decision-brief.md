# PostgreSQL 技术表 DDL 决策简报

状态：决定已确认；仓库内实现验收已通过；受保护 CI 待完成

Date: 2026-07-29

## 主题与范围

本简报确认 `R-04-B`：`outbox`、`outbox_partition`、`inbox` 三张 PostgreSQL 技术表的字段、约束、索引与 Inbox 提交不变量。默认 schema 使用 `bento` 展示。

本简报不确认同分区并发 Enqueue、Claim、Finalize 与指针修复 SQL，不确认 schema 模板渲染、checksum、`CheckSchema` 对象集合或 PostgreSQL 实验证据；这些分别属于 `R-04-C`、`R-04-D` 和 `R-04-E`。

## 已验证事实

- Outbox 信封与业务写入必须在同一个 PostgreSQL 事务内原子提交。
- `outbox_partition` 必须指向同一分区中的真实 Outbox 行，但数据库外键无法单独证明该行是最早非终态事件。
- Inbox 的 `PROCESSING` 是事务内占用态。普通 `CHECK` 无法同时允许先插入 `PROCESSING`、随后在同一事务终结，并阻止遗漏终结后提交。
- PostgreSQL `timestamptz` 只保存微秒精度；`jsonb` 会规范化 JSON，不能用其存储大小重复判断原始 JSON 的 1 MiB 上限。
- 私有化消费者可能要求自定义 schema 名称；规范 DDL 必须完全限定对象名且不依赖 `search_path`。

## 评估选项

### Outbox 分区指针

1. 重复保存 `head_source`、`head_event_id` 和 `head_enqueued_seq`。可直接按事件主键定位，但三个字段可能互相矛盾，需要额外触发器维护。
2. 只保存 `head_source`、`head_event_id`。能够外键到 Outbox，却不能直接表达和比较队列顺序。
3. 只保存 `partition_key` 与 `head_enqueued_seq`，并通过复合外键引用 Outbox 的同名列。字段最少，数据库可保证指针不跨分区，队首顺序仍由 Store SQL 与并发测试证明。

采用选项 3。

### Inbox 提交不变量

1. 只依赖 Store 调用纪律。遗漏 `MarkProcessed`/`MarkRejected` 会留下已提交的 `PROCESSING` 行并永久抑制重投。
2. 使用普通行级 `CHECK`。它会拒绝合法的事务内中间态，无法表达“只允许在提交前存在”。
3. 使用 `DEFERRABLE INITIALLY DEFERRED` constraint trigger，在事务提交时重新查询该键的最终行；最终状态仍为 `PROCESSING` 时以 SQLSTATE `23514` 阻止提交。

采用选项 3。触发器监听 `INSERT` 和 `UPDATE`，既防止首次占用遗漏终结，也防止终态被非法改回 `PROCESSING`。触发器必须重新查询当前行，不能检查事件触发时的 `NEW.status`，否则一次合法的 `INSERT PROCESSING -> UPDATE PROCESSED` 仍会在提交时被误拒绝。

## 规范 DDL

以下 SQL 是 `R-04-B` 的规范语义来源。它使用默认 schema `bento`；`R-04-D` 负责把 schema 变成经过验证和正确引用的模板参数。迁移作业必须先创建 schema，框架迁移本身不执行 `CREATE SCHEMA`、`GRANT` 或 `REVOKE`。

```sql
CREATE TABLE bento.outbox (
    source            text        NOT NULL,
    event_id          text        NOT NULL,
    partition_key     text        NOT NULL,
    enqueued_seq      bigint      GENERATED ALWAYS AS IDENTITY,
    status            text        NOT NULL,
    available_at      timestamptz NOT NULL,
    lease_token       uuid,
    lease_expires_at  timestamptz,
    attempt           bigint      NOT NULL DEFAULT 0,
    failures          bigint      NOT NULL DEFAULT 0,
    published_at      timestamptz,
    abandoned_at      timestamptz,
    last_failed_at    timestamptz,
    failure_code      text,

    spec_version      text        NOT NULL,
    event_type        text        NOT NULL,
    event_version     bigint      NOT NULL,
    scope             text        NOT NULL,
    subject           text        NOT NULL,
    occurred_at       timestamptz NOT NULL,
    recorded_at       timestamptz NOT NULL,
    correlation_id    text,
    causation_id      text,
    traceparent       text,
    tracestate        text,
    content_type      text        NOT NULL,
    payload           jsonb       NOT NULL,

    CONSTRAINT outbox_pk
        PRIMARY KEY (source, event_id),
    CONSTRAINT outbox_partition_seq_uq
        UNIQUE (partition_key, enqueued_seq),
    CONSTRAINT outbox_source_chk
        CHECK (btrim(source) <> '' AND octet_length(source) <= 255),
    CONSTRAINT outbox_event_id_chk
        CHECK (btrim(event_id) <> '' AND octet_length(event_id) <= 128),
    CONSTRAINT outbox_partition_key_chk
        CHECK (btrim(partition_key) <> '' AND octet_length(partition_key) <= 512),
    CONSTRAINT outbox_enqueued_seq_chk
        CHECK (enqueued_seq > 0),
    CONSTRAINT outbox_status_chk
        CHECK (status IN ('PENDING', 'LEASED', 'PUBLISHED', 'ABANDONED')),
    CONSTRAINT outbox_attempt_chk
        CHECK (attempt BETWEEN 0 AND 4294967295),
    CONSTRAINT outbox_failures_chk
        CHECK (failures BETWEEN 0 AND 4294967295 AND failures <= attempt),
    CONSTRAINT outbox_nonpending_attempt_chk
        CHECK (status = 'PENDING' OR attempt > 0),
    CONSTRAINT outbox_failure_code_chk
        CHECK (
            failure_code IS NULL
            OR (
                octet_length(failure_code) <= 128
                AND failure_code ~ '^[a-z0-9][a-z0-9._-]*$'
            )
        ),
    CONSTRAINT outbox_failure_history_chk
        CHECK (
            (
                failures = 0
                AND last_failed_at IS NULL
                AND (
                    (status = 'ABANDONED' AND failure_code IS NOT NULL)
                    OR (status <> 'ABANDONED' AND failure_code IS NULL)
                )
            )
            OR (
                failures > 0
                AND last_failed_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT outbox_state_shape_chk
        CHECK (
            (
                status = 'PENDING'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'LEASED'
                AND lease_token IS NOT NULL
                AND lease_expires_at IS NOT NULL
                AND published_at IS NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'PUBLISHED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NOT NULL
                AND abandoned_at IS NULL
            )
            OR (
                status = 'ABANDONED'
                AND lease_token IS NULL
                AND lease_expires_at IS NULL
                AND published_at IS NULL
                AND abandoned_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT outbox_spec_version_chk
        CHECK (spec_version = '1.0'),
    CONSTRAINT outbox_event_type_chk
        CHECK (btrim(event_type) <> '' AND octet_length(event_type) <= 255),
    CONSTRAINT outbox_event_version_chk
        CHECK (event_version BETWEEN 1 AND 4294967295),
    CONSTRAINT outbox_scope_chk
        CHECK (btrim(scope) <> '' AND octet_length(scope) <= 255),
    CONSTRAINT outbox_subject_chk
        CHECK (btrim(subject) <> '' AND octet_length(subject) <= 512),
    CONSTRAINT outbox_correlation_id_chk
        CHECK (
            correlation_id IS NULL
            OR (btrim(correlation_id) <> '' AND octet_length(correlation_id) <= 128)
        ),
    CONSTRAINT outbox_causation_id_chk
        CHECK (
            causation_id IS NULL
            OR (btrim(causation_id) <> '' AND octet_length(causation_id) <= 128)
        ),
    CONSTRAINT outbox_traceparent_chk
        CHECK (
            traceparent IS NULL
            OR (btrim(traceparent) <> '' AND octet_length(traceparent) <= 512)
        ),
    CONSTRAINT outbox_tracestate_chk
        CHECK (
            tracestate IS NULL
            OR (btrim(tracestate) <> '' AND octet_length(tracestate) <= 512)
        ),
    CONSTRAINT outbox_content_type_chk
        CHECK (content_type = 'application/json'),
    CONSTRAINT outbox_payload_object_chk
        CHECK (jsonb_typeof(payload) = 'object')
);

CREATE INDEX outbox_active_recorded_idx
    ON bento.outbox (recorded_at, enqueued_seq)
    WHERE status IN ('PENDING', 'LEASED');

CREATE TABLE bento.outbox_partition (
    partition_key     text        NOT NULL,
    head_enqueued_seq bigint      NOT NULL,
    claimable_at      timestamptz NOT NULL,

    CONSTRAINT outbox_partition_pk
        PRIMARY KEY (partition_key),
    CONSTRAINT outbox_partition_key_chk
        CHECK (btrim(partition_key) <> '' AND octet_length(partition_key) <= 512),
    CONSTRAINT outbox_partition_head_seq_chk
        CHECK (head_enqueued_seq > 0),
    CONSTRAINT outbox_partition_head_fk
        FOREIGN KEY (partition_key, head_enqueued_seq)
        REFERENCES bento.outbox (partition_key, enqueued_seq)
        ON UPDATE RESTRICT
        ON DELETE RESTRICT
);

CREATE INDEX outbox_partition_claimable_idx
    ON bento.outbox_partition (claimable_at, partition_key);

CREATE TABLE bento.inbox (
    consumer      text        NOT NULL,
    source        text        NOT NULL,
    event_id      text        NOT NULL,
    status        text        NOT NULL,
    received_at   timestamptz NOT NULL,
    settled_at    timestamptz,
    failure_code  text,

    CONSTRAINT inbox_pk
        PRIMARY KEY (consumer, source, event_id),
    CONSTRAINT inbox_consumer_chk
        CHECK (btrim(consumer) <> '' AND octet_length(consumer) <= 255),
    CONSTRAINT inbox_source_chk
        CHECK (btrim(source) <> '' AND octet_length(source) <= 255),
    CONSTRAINT inbox_event_id_chk
        CHECK (btrim(event_id) <> '' AND octet_length(event_id) <= 128),
    CONSTRAINT inbox_status_chk
        CHECK (status IN ('PROCESSING', 'PROCESSED', 'REJECTED')),
    CONSTRAINT inbox_failure_code_chk
        CHECK (
            failure_code IS NULL
            OR (
                octet_length(failure_code) <= 128
                AND failure_code ~ '^[a-z0-9][a-z0-9._-]*$'
            )
        ),
    CONSTRAINT inbox_state_shape_chk
        CHECK (
            (
                status = 'PROCESSING'
                AND settled_at IS NULL
                AND failure_code IS NULL
            )
            OR (
                status = 'PROCESSED'
                AND settled_at IS NOT NULL
                AND failure_code IS NULL
            )
            OR (
                status = 'REJECTED'
                AND settled_at IS NOT NULL
                AND failure_code IS NOT NULL
            )
        ),
    CONSTRAINT inbox_settled_time_chk
        CHECK (settled_at IS NULL OR settled_at >= received_at)
);

CREATE FUNCTION bento.assert_inbox_settled_before_commit()
RETURNS trigger
LANGUAGE plpgsql
AS $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM bento.inbox AS i
        WHERE i.consumer = NEW.consumer
          AND i.source = NEW.source
          AND i.event_id = NEW.event_id
          AND i.status = 'PROCESSING'
    ) THEN
        RAISE EXCEPTION USING
            ERRCODE = '23514',
            SCHEMA = 'bento',
            TABLE = 'inbox',
            CONSTRAINT = 'inbox_settled_before_commit',
            MESSAGE = 'inbox entry must reach a terminal state before commit';
    END IF;

    RETURN NULL;
END;
$$;

CREATE CONSTRAINT TRIGGER inbox_settled_before_commit
AFTER INSERT OR UPDATE ON bento.inbox
DEFERRABLE INITIALLY DEFERRED
FOR EACH ROW
EXECUTE FUNCTION bento.assert_inbox_settled_before_commit();
```

## 字段和精度规则

- 信封字段逐列保存；Go `Envelope.ID` 映射到 `event_id`，`Type` 映射到 `event_type`，`Version` 映射到 `event_version`。
- 所有字符串必须是有效 UTF-8；全部 `Max*Length` 按 UTF-8 字节数解释，Go 边界使用 `utf8.ValidString` 与 `len([]byte(value))`，PostgreSQL 使用 `octet_length`。Unicode 空白和控制字符的完整规则仍由 `Envelope.Validate` 强制；DDL 提供结构性纵深防御，不另造一套可能漂移的 Unicode 规则。
- PostgreSQL 数据库 `server_encoding` 必须为 `UTF8`。迁移不改变数据库编码，`R-04-D` 的 `CheckSchema` 必须只读检查并在不匹配时返回 `ErrSchemaIncompatible`。
- 可选字符串使用 SQL `NULL` 表示 Go 空字符串；Store 写入和扫描必须保持该映射。
- `attempt`、`failures`、`event_version` 使用 `bigint`，但约束在 Go `uint32` 范围内；`failures <= attempt`。
- `lease_token` 使用原生 `uuid`，由 Go Store 生成，不要求 `uuid-ossp` 或 `pgcrypto` 扩展。
- Store 写入时间前统一转为 UTC，并在集成测试中按 PostgreSQL 微秒精度比较；不得要求纳秒级往返相等。
- 数据库只检查 `payload` 为 JSON object。1 MiB 上限由写入前的 `Envelope.Validate` 按原始 JSON 字节数执行，不使用 `pg_column_size(jsonb)` 替代。

## 约束边界

- 复合外键保证分区指针引用同一 `partition_key` 下的真实 Outbox 行，但不证明其为最早非终态行。该不变量必须由 `R-04-C` 的统一锁顺序、条件 SQL 和 `R-04-E` 的并发/负向测试证明。
- `outbox_partition` 只保留活跃分区；最后一个非终态事件终结时由 Store 删除分区行。DDL 不级联删除 Outbox 历史。
- `outbox_active_recorded_idx` 服务待发布数量和最老记录延迟观测；`outbox_partition_claimable_idx` 服务到期分区扫描；`outbox_partition_seq_uq` 同时服务复合外键和分区内顺序扫描。
- constraint trigger 抛出的 `23514` 是基础设施错误，表示调用方违反 Inbox 事务协议；不得映射为 `ErrInboxBusy`。`ErrInboxBusy` 只对应并发占用的 `55P03`。
- 状态 CHECK 只证明当前行的字段组合合法，不记录也不证明历史转换路径。Outbox 的终态不可逆、Inbox 的 `PROCESSED`/`REJECTED` 不可互换，必须由 `R-04-C` 中带当前状态和 lease token/幂等键条件的 Store SQL 强制，并由负向测试保护。
- 本阶段不定义技术表保留期或删除 API。终态清理必须在后续运维设计中保持审计、重放和 Inbox 幂等窗口，不得由应用启动自动执行。

## 本轮验证

2026-07-29 使用本机一次性 PostgreSQL 16 容器执行了本简报的完整 SQL，并验证：

- schema 外的预置 `CREATE SCHEMA bento` 完成后，三张表、两个索引、函数和约束触发器全部创建成功。
- 跨分区队首引用以 `23503` 和 `outbox_partition_head_fk` 被拒绝。
- `INSERT PROCESSING -> UPDATE PROCESSED -> COMMIT` 成功；遗漏终结和终态回退到 `PROCESSING` 均以 `23514` 被拒绝且未留下提交行。
- 非法 Outbox 状态形状、超过 UTF-8 字节上限的多字节事件类型和非 object JSON Payload 均由对应命名 CHECK 拒绝。
- 测试数据库 `server_encoding` 为 `UTF8`。

该检查是最初的一次性观察；后续 `R-04-E` 已在 PostgreSQL 16.12 保存环境指纹、负向约束、并发日志、catalog 变体和 `EXPLAIN` 设计证据。真实迁移和 Store 仍须通过 `I-04` 实现验收。

## 主要代价与缓解

- 约束和延迟触发器增加写入成本；三张表的正确性优先于微小写入开销。`R-04-E` 已保存 10 万分区的索引方向证据，实际容量仍由 `I-04` 与消费者环境验证。
- `jsonb` 不保留原始 JSON 字节表示；公共合同本就不承诺对象键顺序或字节级规范化，幂等只使用稳定键。
- 全局 identity 会产生序列空洞，事务回滚也不会回收值；顺序只比较大小，不要求连续。
- 默认 schema 被写入展示 SQL；后续 `R-04-D` 已确认以唯一受控 token 渲染 schema，函数异常诊断改用 `TG_TABLE_SCHEMA`，不在字符串字面量中复用 identifier token。

## 后续门禁

`R-04-B` 完成，并将 `F-27` 改为 `CONFIRMED`。后续 `R-04-C` 至 `R-04-E` 已完成，真实迁移和 DDL 负向约束也已通过 PostgreSQL 16.12 仓库验收；受保护 CI 复跑仍依赖 `B-05`。

`R-04-C` 至 `R-04-E`、`R-05 testkit` 与 `R-06` 公共兼容性基线已在独立文档中完成，技术 schema 决定已经进入 [ADR-0005](../adr/0005-postgres-outbox-inbox-consistency.md)；真实迁移与 Store 通过 `I-04` 验收。
