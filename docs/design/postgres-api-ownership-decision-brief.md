# PostgreSQL 公共 API 与发布编排所有权决策简报

状态：决定已确认；仓库内实现验收已通过；受保护 CI 待完成

Date: 2026-07-29

## 主题与范围

本简报只决定 `R-04` 中 PostgreSQL `DB`、Outbox/Inbox Store 与消费者发布进程之间的公共 API 和所有权。它不固定三张表的完整 DDL、并发 SQL、迁移模板或 PostgreSQL 实验证据。

## 已验证事实

- TMS 必须在同一 PostgreSQL 事务内保存业务结果、授权证据引用和 Outbox 记录。
- Parcel 拥有独立业务数据库与发布边界，并需要处理重复输入、渠道结果待确认和跨上下文事件恢复；共享框架不能接管其运行部署。
- `eventing` 已经定义 `Publisher`、Outbox Writer/Claimer/Finalizer 与 Inbox Starter/Completer 小接口。
- 领取上限用于终结“每次取得租约后都崩溃”的记录；如果只由进程内策略判断，而未进入原子 Claim 合同，崩溃可能永久跳过该上限。

## 评估选项

### A. PostgreSQL Store 同时拥有持久化、Publisher、重试策略和常驻循环

优点是消费者装配最少。代价是数据库适配器会拥有进程生命周期、Broker 超时、并发度和退避策略，无法保持持久化适配器的单一职责，也会迫使 TMS 与 Parcel 使用相同运行策略。

### B. 消费者拥有 Store 以上的全部语义

优点是框架 API 最小。代价是两个消费者都要重新实现租约、失败分类、领取上限和状态转换规则，框架已确认的 At-Least-Once 语义会退化为消费者纪律。

### C. 原子持久化归 Store，运行编排归消费者，原子上限进入 Claim 合同

PostgreSQL Store 只负责事务绑定、原子状态转换、锁和 SQL。TMS、Parcel 各自拥有发布进程的启动停止、并发度、Publisher 超时、失败预算和退避曲线。`OutboxClaim` 显式携带 `MaxAttempts`，Store 在领取事务中保证达到上限的记录不再产生新的 Delivery，并原子推进分区。

## 决定

采用选项 C。

- `postgres.DB` 拥有连接池、事务 owner、schema 名和事务级可观测性装配；构造不执行网络探测或 DDL。
- `postgres/outbox.Store` 实现 Outbox 持久化端口，不调用 Publisher、不计算退避、不启动 goroutine。
- `postgres/inbox.Store` 实现 Inbox 持久化端口，只额外拥有有界锁等待参数。
- 消费者发布进程拥有 worker 生命周期、批次调度、并发度、`PublisherTimeout`、`MaxFailures`、退避与 jitter，并负责在事务中调用 Claim/Finalize。
- `MaxAttempts` 是 Claim 的显式参数，由 PostgreSQL Store 原子执行；它不是 Store 的隐藏全局配置。
- Store 构造函数返回具体 `*Store`，消费者生产代码依赖所需的小接口，不依赖 Store 的全部方法集。

## 主要代价与缓解

- TMS 与 Parcel 都需要少量发布循环代码；通过消费者合同和后续 `testkit` 夹具验证相同状态机，不在 `v0.1` 预建常驻 Processor 抽象。
- 消费者可能配置 `LeaseFor <= PublisherTimeout`；该关系由消费者 bootstrap 校验并进入合同测试，PostgreSQL Store 无法从 `Publisher` 端口推断超时。
- Consumer 可以选择错误的 Finalize 操作；Store 保证转换原子和 token 条件，消费者合同验证错误分类到 `MarkPublished`、`RecordFailure`、`Abandon` 的映射。`RecordFailure` 同时接收 `MaxFailures`，由 Store 原子决定重排或终结，避免失败计数与终态分成两次写入。
- `MaxAttempts` 进入核心 Claim 合同会扩大一个字段，但它消除了隐藏 Store 配置，并使任意适配器都能执行相同防崩溃不变量。

## 后续门禁

`R-04-B` 至 `R-04-E` 已经固定完整 DDL、并发 SQL、迁移/schema 检查并保存 PostgreSQL 16.12 设计证据，真实适配器也已通过仓库内 `I-04`。所有权决定已经进入 [ADR-0004](../adr/0004-framework-migrations-consumer-execution.md)；下一步是受保护 CI 和双消费者候选验证，不把运行策略移入 Store。
