# 架构决策记录

最终实施决策简报已确认。以下 ADR 均已接受：

- [ADR-0001：建设独立的共享 Go 框架](./0001-standalone-shared-framework.md)
- [ADR-0002：分离标准库核心与基础设施适配器](./0002-core-adapter-boundary.md)
- [ADR-0003：通过 context 传播事务并执行严格模式](./0003-context-transaction-propagation.md)
- [ADR-0004：框架拥有技术迁移，消费者拥有执行](./0004-framework-migrations-consumer-execution.md)
- [ADR-0005：固定 PostgreSQL Outbox/Inbox 一致性模型](./0005-postgres-outbox-inbox-consistency.md)
- [ADR-0006：testkit 以确定性替身和真实端口合同为边界](./0006-testkit-real-port-contracts.md)
- [ADR-0007：采用四层兼容性与双消费者发布门禁](./0007-compatibility-and-consumer-gates.md)
- [ADR-0008：使用稳定 module path 解耦 Git transport](./0008-stable-module-path.md)
- [ADR-0009：使用合规模式 WORM 与 OIDC 签名发布证据](./0009-worm-oidc-release-evidence.md)
