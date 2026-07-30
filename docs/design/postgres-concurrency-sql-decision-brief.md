# PostgreSQL 并发 SQL 与锁顺序决策简报

状态：决定已确认；仓库内实现验收已通过；受保护 CI 待完成

Date: 2026-07-29

## 主题与范围

本简报确认 `R-04-C`：PostgreSQL Outbox 的 Enqueue、Claim、MarkPublished、RecordFailure、Abandon、分区指针修复，以及 Inbox Start、MarkProcessed、MarkRejected 的完整 SQL、事务隔离、锁顺序和错误映射。

本简报不确认迁移模板渲染、checksum、`CheckSchema` 对象集合或可复现实验资产；这些分别属于 `R-04-D` 和 `R-04-E`。默认 schema 使用 `bento` 展示，所有 SQL 必须通过 `R-04-D` 的受控 schema 渲染生成，不得依赖 `search_path`。

## 已确认上游不变量

- 同一 `PartitionKey` 只允许最早非终态事件被领取；不同分区允许并行。
- Enqueue 与消费者业务写入使用同一个 `application.Transactor`，初始 `available_at` 等于 Envelope 的 `RecordedAt`。
- Claim 递增 `attempt`，租约过期不递增 `failures`；只有 RecordFailure 递增 `failures`。
- 终结操作必须同时验证当前状态、当前 lease token、租约有效期和分区队首。
- Inbox 的占用、业务副作用和终态位于同一事务；已提交数据库中不得存在 `PROCESSING`。
- 所有 Store 写操作都要求事务 context；SQL 失败不得静默回退到连接池。

## 压力测试后的决定

### 首次 Enqueue 的序列化

空分区没有 `outbox_partition` 行可供 `FOR UPDATE`。如果两个事务先分配 identity、再尝试创建分区行，较大的序号可能先提交并被发布，随后较小序号才可见，破坏同分区入队顺序。

Enqueue 因此先取得事务级 advisory lock：

```sql
SELECT pg_advisory_xact_lock(
    hashtextextended('idp-bento-go/outbox/' || $1::text, 0)
);
```

`$1` 是已经通过 Envelope 校验的 `partition_key`。64 位哈希碰撞只会让无关分区短暂串行，不会破坏正确性；锁键不写日志、不进入遥测。advisory lock 在事务结束时自动释放，不需要扩展或新增技术表。

### 事务隔离

`postgres.Transactor` 显式使用 PostgreSQL `READ COMMITTED`，不继承客户数据库可能被修改的默认隔离级别。`v0.1` 不暴露事务隔离选项，也不自动重试序列化失败。消费者依赖 Revision 条件更新、显式行锁和本简报的原子 SQL获得一致性。

### 行锁顺序

所有正常路径遵守：

```text
Enqueue advisory lock（仅 Enqueue）
    -> outbox_partition 行锁（存在时）
    -> outbox 行锁/UPDATE
```

Claim 从已经存在的 `outbox_partition` 行开始，不取得 advisory lock。终结操作可以先无锁读取 Outbox 主键以解析分区和序号，但第一个实际行锁必须是 `outbox_partition`，随后才能条件更新 Outbox。无锁解析不构成锁顺序的一部分。

Claim 持有分区行时，Enqueue 可能已经取得 advisory lock 并等待分区行，但 Claim 从不反向等待 advisory lock，因此不会形成环。所有锁都由外层事务提交或回滚释放，Store 不显式解锁。

### Claim 工作上限

`OutboxClaim.Limit` 同时是本次最多检查的到期分区数和最多返回的 Delivery 数。达到领取上限、指针修复或空分区删除会占用一次检查名额，但不产生 Delivery，因此合法结果可以少于 `Limit`；调用方按调度策略再次 Claim。

`Limit` 必须位于 `1..MaxOutboxClaimLimit`，其中 `MaxOutboxClaimLimit = 1000`。该上限限制单次事务持有的分区锁、返回内存和 SQL 往返数量；消费者仍拥有实际批次大小，推荐初值为 `100`。

## SQL 通用规则

- 每个代码块独立编号参数；参数编号不跨代码块延续。
- Store 在进入 SQL 前完成结构校验、UUID 生成、UTC 转换和 PostgreSQL 微秒精度截断。
- 所有 eligibility、租约和终结时间比较使用调用参数中的时间，不在同一路径混用 `now()`、`clock_timestamp()` 或应用 Clock。
- 每次 `UPDATE` 都在自身 `WHERE` 中复检状态、时间、token 和指针条件；上游 `SELECT` 或 CTE 的判断不能替代该复检。
- 影响行数与预期不一致时不得继续推进指针。Claim 可以修复指针并返回较小批次；终结操作返回包装 `eventing.ErrLeaseLost` 的错误。
- 下面的 `head_claimable_at` 均按相同规则计算：`PENDING -> available_at`，`LEASED -> lease_expires_at`。

## Enqueue

Enqueue 在消费者已经开启的业务事务中依次执行 E-01 至 E-04。

### E-01：取得同分区事务锁

```sql
SELECT pg_advisory_xact_lock(
    hashtextextended('idp-bento-go/outbox/' || $1::text, 0)
);
```

- `$1`: `partition_key`

### E-02：锁定已有分区行

```sql
SELECT head_enqueued_seq, claimable_at
FROM bento.outbox_partition
WHERE partition_key = $1
FOR UPDATE;
```

没有返回行是正常的首次入队。若另一个 Claim 正在删除该行，本语句等待其事务结束后重新判断当前版本。

### E-03：写入 Outbox

```sql
INSERT INTO bento.outbox (
    source,
    event_id,
    partition_key,
    status,
    available_at,
    spec_version,
    event_type,
    event_version,
    scope,
    subject,
    occurred_at,
    recorded_at,
    correlation_id,
    causation_id,
    traceparent,
    tracestate,
    content_type,
    payload
)
VALUES (
    $1,
    $2,
    $3,
    'PENDING',
    $10,
    $4,
    $5,
    $6,
    $7,
    $8,
    $9,
    $10,
    $11,
    $12,
    $13,
    $14,
    $15,
    $16::jsonb
)
RETURNING enqueued_seq;
```

参数依次为 Source、EventID、PartitionKey、SpecVersion、Type、Version、Scope、Subject、OccurredAt、RecordedAt、可空 CorrelationID、CausationID、TraceParent、TraceState、ContentType、Payload。可选空字符串写为 SQL `NULL`。

只有约束名为 `outbox_pk` 的 `23505` 映射为 `eventing.ErrAlreadyEnqueued`；其他唯一约束错误保留为基础设施错误。

### E-04：重算并写入真实队首

```sql
WITH head AS (
    SELECT
        partition_key,
        enqueued_seq,
        CASE status
            WHEN 'PENDING' THEN available_at
            WHEN 'LEASED' THEN lease_expires_at
        END AS head_claimable_at
    FROM bento.outbox
    WHERE partition_key = $1
      AND status IN ('PENDING', 'LEASED')
    ORDER BY enqueued_seq
    LIMIT 1
    FOR UPDATE
)
INSERT INTO bento.outbox_partition (
    partition_key,
    head_enqueued_seq,
    claimable_at
)
SELECT partition_key, enqueued_seq, head_claimable_at
FROM head
ON CONFLICT (partition_key) DO UPDATE
SET head_enqueued_seq = EXCLUDED.head_enqueued_seq,
    claimable_at = EXCLUDED.claimable_at
RETURNING head_enqueued_seq, claimable_at;
```

E-03 成功后 E-04 必须返回一行；零行表示数据库不一致并使外层事务回滚。即使分区行意外缺失，E-04 也会选择最早非终态事件，而不是盲目把本次新事件设为队首。

## Claim

Claim 在一个短事务中执行。Store 先一次锁定最多 `Limit` 个候选分区，再按返回顺序逐个处理；所有成功租约在同一次提交中生效。

### C-01：锁定到期分区批次

```sql
SELECT partition_key, head_enqueued_seq, claimable_at
FROM bento.outbox_partition
WHERE claimable_at <= $1
ORDER BY claimable_at, partition_key
LIMIT $2
FOR UPDATE SKIP LOCKED;
```

- `$1`: `claim.Now`
- `$2`: `claim.Limit`

### C-02：锁定真实最早非终态事件

对 C-01 的每个分区执行：

```sql
SELECT
    enqueued_seq,
    status,
    available_at,
    lease_expires_at,
    attempt,
    CASE status
        WHEN 'PENDING' THEN available_at
        WHEN 'LEASED' THEN lease_expires_at
    END AS head_claimable_at
FROM bento.outbox
WHERE partition_key = $1
  AND status IN ('PENDING', 'LEASED')
ORDER BY enqueued_seq
LIMIT 1
FOR UPDATE;
```

零行时执行 C-03。返回序号与 C-01 指针不一致时执行 C-04，本轮不为该分区返回 Delivery。序号一致时，先把漂移的 `claimable_at` 修正为 `head_claimable_at`；若真实时间晚于 `claim.Now`，本轮不领取。

### C-03：删除空分区

```sql
DELETE FROM bento.outbox_partition
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

必须影响一行，否则返回基础设施一致性错误。

### C-04：修复错误指针

```sql
UPDATE bento.outbox_partition
SET head_enqueued_seq = $3,
    claimable_at = $4
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

- `$1`: 分区
- `$2`: C-01 中的旧指针
- `$3`: C-02 找到的真实队首序号
- `$4`: 真实队首的 `head_claimable_at`

必须影响一行。修复本身不返回 Delivery，避免一次调用同时掩盖不一致并产生外部副作用。

### C-05：达到领取上限时终结队首

当 C-02 的 `attempt >= claim.MaxAttempts` 且真实队首已到期时执行：

```sql
UPDATE bento.outbox AS o
SET status = 'ABANDONED',
    lease_token = NULL,
    lease_expires_at = NULL,
    abandoned_at = $3,
    failure_code = 'outbox.claim_limit_exceeded'
WHERE o.partition_key = $1
  AND o.enqueued_seq = $2
  AND o.attempt >= $4
  AND (
      (o.status = 'PENDING' AND o.available_at <= $3)
      OR
      (o.status = 'LEASED' AND o.lease_expires_at <= $3)
  )
RETURNING o.enqueued_seq;
```

- `$1`: 分区
- `$2`: 队首序号
- `$3`: `claim.Now`
- `$4`: `claim.MaxAttempts`

成功后执行 C-07；本次检查不产生 Delivery。影响零行时执行一次 C-02/C-04 修复并跳过该分区，不得放宽谓词重试。

### C-06：领取队首

Store 为每个候选预生成新的 UUID token，并计算绝对 `lease_expires_at = claim.Now + claim.LeaseFor`：

```sql
UPDATE bento.outbox AS o
SET status = 'LEASED',
    lease_token = $3,
    lease_expires_at = $4,
    attempt = o.attempt + 1
WHERE o.partition_key = $1
  AND o.enqueued_seq = $2
  AND o.attempt < $5
  AND (
      (o.status = 'PENDING' AND o.available_at <= $6)
      OR
      (o.status = 'LEASED' AND o.lease_expires_at <= $6)
  )
RETURNING
    o.source,
    o.event_id,
    o.lease_token,
    o.attempt,
    o.failures,
    o.spec_version,
    o.event_type,
    o.event_version,
    o.scope,
    o.subject,
    o.partition_key,
    o.occurred_at,
    o.recorded_at,
    o.correlation_id,
    o.causation_id,
    o.traceparent,
    o.tracestate,
    o.content_type,
    o.payload;
```

- `$1`: 分区
- `$2`: 队首序号
- `$3`: 新 UUID token
- `$4`: 绝对租约到期时间
- `$5`: `claim.MaxAttempts`
- `$6`: `claim.Now`

成功后执行：

```sql
UPDATE bento.outbox_partition
SET claimable_at = $3
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

必须影响一行。C-06 自身影响零行时重新执行 C-02 并修复指针，本轮不返回 Delivery。可领取谓词必须保留在 `UPDATE` 自身，防止后续重构把 READ COMMITTED 复检退化为上游快照判断。

### C-07：终态后推进分区

先锁定新的真实队首：

```sql
SELECT
    enqueued_seq,
    CASE status
        WHEN 'PENDING' THEN available_at
        WHEN 'LEASED' THEN lease_expires_at
    END AS head_claimable_at
FROM bento.outbox
WHERE partition_key = $1
  AND status IN ('PENDING', 'LEASED')
ORDER BY enqueued_seq
LIMIT 1
FOR UPDATE;
```

返回一行时：

```sql
UPDATE bento.outbox_partition
SET head_enqueued_seq = $3,
    claimable_at = $4
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

零行时：

```sql
DELETE FROM bento.outbox_partition
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

`$2` 是刚进入终态的旧队首。更新或删除必须影响一行，否则整个事务回滚。

## Outbox 终结操作

MarkPublished、RecordFailure 和 Abandon 先执行 F-01/F-02，再执行各自条件更新。调用方提供的终结时间同时作为租约有效性判断时间；时间必须非零并已按 UTC/微秒精度规范化。

### F-01：无锁解析目标分区

```sql
SELECT partition_key, enqueued_seq
FROM bento.outbox
WHERE source = $1
  AND event_id = $2;
```

零行返回包装 `eventing.ErrLeaseLost` 的错误。该查询不得带 `FOR UPDATE`，否则会先锁 Outbox 并与其他路径形成反向锁序。

### F-02：锁定并验证当前分区队首

```sql
SELECT head_enqueued_seq
FROM bento.outbox_partition
WHERE partition_key = $1
FOR UPDATE;
```

零行或 `head_enqueued_seq` 不等于 F-01 的目标序号时返回 `ErrLeaseLost`。

### F-03：MarkPublished

```sql
UPDATE bento.outbox AS o
SET status = 'PUBLISHED',
    lease_token = NULL,
    lease_expires_at = NULL,
    published_at = $6
WHERE o.source = $1
  AND o.event_id = $2
  AND o.partition_key = $3
  AND o.enqueued_seq = $4
  AND o.status = 'LEASED'
  AND o.lease_token = $5
  AND o.lease_expires_at > $6
  AND EXISTS (
      SELECT 1
      FROM bento.outbox_partition AS p
      WHERE p.partition_key = o.partition_key
        AND p.head_enqueued_seq = o.enqueued_seq
  )
RETURNING o.partition_key, o.enqueued_seq;
```

影响一行后执行 C-07；零行返回 `ErrLeaseLost`。

### F-04：RecordFailure

```sql
UPDATE bento.outbox AS o
SET failures = o.failures + 1,
    last_failed_at = $6,
    failure_code = $8,
    lease_token = NULL,
    lease_expires_at = NULL,
    status = CASE
        WHEN o.failures + 1 >= $9 THEN 'ABANDONED'
        ELSE 'PENDING'
    END,
    available_at = CASE
        WHEN o.failures + 1 >= $9 THEN o.available_at
        ELSE $7
    END,
    abandoned_at = CASE
        WHEN o.failures + 1 >= $9 THEN $6
        ELSE NULL
    END
WHERE o.source = $1
  AND o.event_id = $2
  AND o.partition_key = $3
  AND o.enqueued_seq = $4
  AND o.status = 'LEASED'
  AND o.lease_token = $5
  AND o.lease_expires_at > $6
  AND EXISTS (
      SELECT 1
      FROM bento.outbox_partition AS p
      WHERE p.partition_key = o.partition_key
        AND p.head_enqueued_seq = o.enqueued_seq
  )
RETURNING o.status, o.failures;
```

- `$6`: `FailedAt`
- `$7`: `RetryAt`
- `$8`: FailureCode
- `$9`: `MaxFailures`

Store 在 SQL 前验证 `RetryAt >= FailedAt`、FailureCode 和正数 MaxFailures。返回 `PENDING` 时执行：

```sql
UPDATE bento.outbox_partition
SET claimable_at = $3
WHERE partition_key = $1
  AND head_enqueued_seq = $2;
```

`$3` 是 RetryAt。返回 `ABANDONED` 时执行 C-07。任一预期更新影响零行都使事务回滚；F-04 主更新零行返回 `ErrLeaseLost`。

### F-05：Abandon

```sql
UPDATE bento.outbox AS o
SET status = 'ABANDONED',
    lease_token = NULL,
    lease_expires_at = NULL,
    abandoned_at = $6,
    failure_code = $7
WHERE o.source = $1
  AND o.event_id = $2
  AND o.partition_key = $3
  AND o.enqueued_seq = $4
  AND o.status = 'LEASED'
  AND o.lease_token = $5
  AND o.lease_expires_at > $6
  AND EXISTS (
      SELECT 1
      FROM bento.outbox_partition AS p
      WHERE p.partition_key = o.partition_key
        AND p.head_enqueued_seq = o.enqueued_seq
  )
RETURNING o.partition_key, o.enqueued_seq;
```

影响一行后执行 C-07；零行返回 `ErrLeaseLost`。Abandon 不递增 `failures`，但保留此前的 `last_failed_at`。

## Inbox

Inbox Store 的 `LockTimeout` 必须是 `1ms` 以上的整毫秒值；默认 `50ms`。Store 生成十进制毫秒字符串，不接受调用方提供任意 PostgreSQL 配置文本。

### I-01：Start 设置局部锁超时

```sql
SELECT set_config('lock_timeout', $1, true);
```

`$1` 由 Store 生成为例如 `50ms`。`true` 表示只作用于当前事务。

### I-02：尝试占用

```sql
INSERT INTO bento.inbox (
    consumer,
    source,
    event_id,
    status,
    received_at
)
VALUES ($1, $2, $3, 'PROCESSING', $4)
ON CONFLICT (consumer, source, event_id) DO NOTHING
RETURNING status;
```

I-02 成功后立即执行：

```sql
SET LOCAL lock_timeout = DEFAULT;
```

返回 `PROCESSING` 时 Start 返回 `Acquired=true`、有效 Receipt 与 `InboxProcessing`。零行时执行 I-03。I-02 的 `55P03` 映射为 `eventing.ErrInboxBusy`，当前事务已经 aborted，不能尝试重置配置或继续查询，必须由外层 Transactor 回滚。context 取消与截止时间保持原分类。

### I-03：读取已提交终态

```sql
SELECT status
FROM bento.inbox
WHERE consumer = $1
  AND source = $2
  AND event_id = $3;
```

只能返回 `PROCESSED` 或 `REJECTED`，并映射为 `Acquired=false` 与零值 Receipt。零行或 `PROCESSING` 表示数据库协议不一致并返回基础设施错误。

### I-04：MarkProcessed

```sql
UPDATE bento.inbox
SET status = 'PROCESSED',
    settled_at = $4
WHERE consumer = $1
  AND source = $2
  AND event_id = $3
  AND status = 'PROCESSING'
  AND received_at <= $4
RETURNING status;
```

### I-05：MarkRejected

```sql
UPDATE bento.inbox
SET status = 'REJECTED',
    settled_at = $4,
    failure_code = $5
WHERE consumer = $1
  AND source = $2
  AND event_id = $3
  AND status = 'PROCESSING'
  AND received_at <= $4
RETURNING status;
```

I-04/I-05 影响零行时执行：

```sql
SELECT status, received_at
FROM bento.inbox
WHERE consumer = $1
  AND source = $2
  AND event_id = $3;
```

行仍为 `PROCESSING` 且终结时间早于 `received_at` 时返回 `eventing.ErrInvalidRequest`；其他零行、缺失或终态情况返回 `eventing.ErrInvalidTransition`。MarkRejected 在 SQL 前验证 FailureCode。延迟约束触发器继续作为提交时最后防线。

## 错误映射

| 条件 | 稳定错误 |
| --- | --- |
| Envelope 或调用参数无效 | `eventing.ErrInvalidEnvelope` / `eventing.ErrInvalidRequest` |
| `outbox_pk` 的 `23505` | `eventing.ErrAlreadyEnqueued` |
| 终结目标缺失、非队首、状态/token 不匹配或租约已失效 | `eventing.ErrLeaseLost` |
| Inbox 并发占用的 `55P03` | `eventing.ErrInboxBusy`，强制回滚 |
| Inbox 完成操作不是从当前事务的 `PROCESSING` 转换 | `eventing.ErrInvalidTransition` |
| 延迟约束触发器的 `23514` | 基础设施协议错误，强制回滚 |
| 指针更新/删除影响行数异常、非预期唯一/check/FK 错误 | 保留原因的基础设施错误 |
| context 取消或截止 | 保持 `context.Canceled` / `context.DeadlineExceeded` 可判定 |

## 必须验证的并发场景

- 两个同分区 Enqueue 事务在一个事务提交前不得为另一个分配更小/更早可发布的序号；回滚只形成允许的 identity 空洞。
- 不同分区 Enqueue 不因 advisory lock 互相等待；人为构造哈希碰撞时只降低并发，不破坏数据。
- 两个 Claim worker 不得领取同一分区；不同分区可以由不同 worker 并行领取。
- 租约过期后的新 Claim 生成新 token，旧 token 的三个终结操作均返回 `ErrLeaseLost`。
- Claim 达到 MaxAttempts 时原子 Abandon 并推进队首，不消耗返回 Delivery 数但占用一次分区检查名额。
- Enqueue 与最后一个队首终结并发时，提交后要么指针指向新事件，要么 Enqueue 在分区删除后重建指针；不得留下活跃事件而没有分区行。
- 指针指向终态或较晚事件时，Claim 修复为最早非终态事件且本轮不发布。
- RecordFailure 的预算未耗尽/恰好耗尽、MarkPublished、Abandon 都在同一事务正确推进或保留分区。
- Inbox 同键未提交占用在 LockTimeout 内返回 `55P03`/`ErrInboxBusy`；占用者提交终态后，后续 Start 返回正常重复结果。
- 删除任一 `UPDATE` 自身的状态、时间、token 或队首谓词后，对应负向并发测试必须失败。

## 本轮验证

2026-07-29 使用一次性 PostgreSQL 16.12 容器执行了本简报的顺序和并发路径：

- 同分区第二个 Enqueue 在 advisory lock 上等待约 `0.79s`；即使其 `RecordedAt` 更早，仍取得更大的 `enqueued_seq`，队首保持第一个事务。
- Enqueue、Claim、MarkPublished、RecordFailure 重排、失败预算耗尽、MaxAttempts、Abandon 和终态指针推进均满足 DDL 状态约束。
- 错误指针从较晚事件修复为最早非终态事件，修复调用不产生 Delivery。
- 两个 Claim 会话通过 `FOR UPDATE SKIP LOCKED` 分别领取不同分区，同一分区未产生重复租约。
- 最后队首终结与新 Enqueue 并发时，Enqueue 等待分区行锁；旧事务删除分区后，新事务重建指针，提交后不存在活跃事件失联。
- Inbox 同键并发占用在 `100ms` 内以 SQLSTATE `55P03` 失败；占用者提交 `PROCESSED` 后，下一次 Start 返回正常终态重复结果。

这些检查是最初的一次性观察；后续 `R-04-E` 已保存环境指纹、20 轮双 worker、谓词删除、10 万分区执行计划和其他正负向设计证据。真实 Store 仍须通过 `I-04` 实现验收。

## 主要代价与缓解

- 同分区 Enqueue 会持有 advisory lock 直到业务事务结束；这是把提交可见性顺序转换为稳定 `enqueued_seq` 顺序的必要成本。PartitionKey 应按聚合或足够细的逻辑流选择，不能默认使用整个租户。
- Claim 返回数量可能小于 Limit；消费者调度循环必须把空/小批次视为正常结果，不能把它解释为永久无工作。
- 每个候选分区需要少量 SQL 往返；`R-04-E` 的 10 万分区计划已确认当前索引方向。实现只能在 `I-04` 证明语义和 mutation tests 不变后合并语句。
- 终结操作先做一次无锁定位读取；它避免反向锁序，最终正确性仍由锁分区后的条件 UPDATE 保证。
- advisory lock 哈希函数不作为持久化格式，PostgreSQL 升级后哈希值变化不会影响历史数据；滚动升级期间所有会话运行在同一数据库引擎上。

## 后续门禁

`R-04-C` 完成。`R-04-E` 已把正向、负向、竞态和执行计划结果保存为可复现设计证据，`F-29` 改为 `CONFIRMED`；真实 Store 随后在 PostgreSQL 16.12 重跑三类各 20 轮竞争、指针修复、旧租约和 10 万分区计划并通过。

`R-04-D`、`R-04-E`、`R-05 testkit` 与 `R-06` 公共兼容性基线已在独立文档中完成，并发与锁顺序决定已经进入 [ADR-0005](../adr/0005-postgres-outbox-inbox-consistency.md)；真实 Store 仍须执行 `I-04`。
