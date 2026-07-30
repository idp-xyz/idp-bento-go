# ADR-0006：testkit 以确定性替身和真实端口合同为边界

状态：已接受

日期：2026-07-29

## 背景

两个消费者需要复用时钟、ID、发布观察和端口合同测试，但通用内存 Repository、Outbox 或 Inbox 无法忠实表达消费者语义、SQL 事务和 PostgreSQL 并发行为，容易制造虚假通过。

## 决定

`testkit` 只公开不可变固定时钟、并发安全顺序 ID、深拷贝记录式 Publisher、合法信封，以及由工厂驱动真实 Repository/Outbox/Inbox 端口的合同运行器。PostgreSQL 容器、迁移和锁竞争夹具保持在适配器测试或 `internal`。

## 后果

- 两个消费者共享同一可移植合同，又保留自己的业务类型和数据库生命周期。
- 合同测试装配成本高于通用内存假实现，但能发现真实事务和作用域缺陷。
- 生产依赖图必须阻止导入 `testkit`。

## 考虑过的替代方案

- 提供通用内存 Store：会掩盖 SQL、锁、事务和作用域错误。
- 公开 PostgreSQL 测试基础设施：会让稳定核心依赖具体适配器和环境管理。
- 由消费者各自复制合同：语义会分叉，候选结果不可比较。

## 链接

- [`testkit` 公共合同](../design/testkit-contract.md)
- [消费者合同策略](../design/consumer-contracts.md)
