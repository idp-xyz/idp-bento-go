# ADR-0003：通过 context 传播事务并执行严格模式

状态：已接受

日期：2026-07-29

## 背景

消费者业务 Repository 与框架 Outbox 必须在同一 `pgx.Tx` 中原子提交，但核心 Repository Port 不能依赖 pgx，也不能让框架接管消费者 Repository 的构造方式。事务外静默回退连接池会破坏原子性。

## 决定

`application.Transactor` 通过派生 `context.Context` 传播事务句柄。`postgres` 使用私有 key、owner 和窄化 Querier/Executor 解析事务；写参与者缺少匹配事务时立即失败，读取实现显式选择 ReadExecutor 或 RequireExecutor。业务作用域和授权身份继续显式传参，不进入 context。

## 后果

- 消费者 Repository 与框架 Outbox 可以共享事务而不污染核心端口签名。
- 事务参与关系不是编译期参数，需要严格错误和合同测试补足。
- 不同 Transactor 的 context 冲突必须安全失败，禁止静默回退。

## 考虑过的替代方案

- 把事务作为 Repository 参数：需要自建泄漏实现细节的 Tx 抽象并拉长全部端口签名。
- UoW 返回已绑定 Repository：会侵入消费者的依赖注入和 Repository 构造。
- 缺少事务时回退连接池：会产生业务写入与 Outbox 非原子提交窗口。

## 链接

- [application 与事务合同](../design/application-transaction-contract.md)
- [PostgreSQL 适配器合同](../design/postgres-adapter-contract.md)
