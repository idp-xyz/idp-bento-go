# ADR-0005：固定 PostgreSQL Outbox/Inbox 一致性模型

状态：已接受

日期：2026-07-29

## 背景

同分区事件顺序、租约恢复、领取竞争、失败预算和 Inbox 并发占用存在数据库级竞态。只定义抽象状态机而不固定 DDL、隔离级别和锁顺序，无法证明真实 PostgreSQL 实现保持原子性和活性。

## 决定

使用 `outbox`、`outbox_partition`、`inbox` 三张技术表；分区指针由同分区复合外键保护，Inbox 使用延迟约束触发器禁止提交 `PROCESSING`。事务固定 READ COMMITTED，同分区 Enqueue 先取得事务级 advisory lock，实际行锁固定为分区行到 Outbox 行，Claim 使用有界 Limit 与 `FOR UPDATE SKIP LOCKED`。

## 后果

- 同一分区严格有序，不同分区可以并行，崩溃后可恢复领取。
- PostgreSQL 实现必须执行 `I-04` 的多轮竞争、负向约束和执行计划验收。
- 分区状态表、锁约定和修复路径增加实现复杂度，但避免隐式竞态。

## 考虑过的替代方案

- 只使用一张 Outbox 表：难以同时保证队首顺序、并行领取与可索引扫描。
- 依赖数据库默认隔离级别和临时锁顺序：环境漂移或重构会引入死锁与乱序。
- 业务写入后再单独写 Outbox：失去事务性 Outbox 的原子性保证。

## 链接

- [PostgreSQL 技术表 DDL](../design/postgres-schema-ddl-decision-brief.md)
- [PostgreSQL 并发 SQL](../design/postgres-concurrency-sql-decision-brief.md)
- [`R-04-E` 设计证据](../design/postgres-r04e-reproducibility-evidence.md)
