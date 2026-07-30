# ADR-0007：采用四层兼容性与双消费者发布门禁

状态：已接受

日期：2026-07-29

## 背景

Go 编译只能发现被调用符号的部分源码破坏，API snapshot 不能发现 JSON wire、错误分类或 Envelope 校验收紧，单一消费者也不能证明共享框架对另一个产品可用。

## 决定

公共兼容性同时使用逐包 apidiff snapshot、业务无关编译夹具、每个 Envelope SpecVersion 的版本化行为清单和 TMS/Parcel 对同一不可变候选的合同结果。`v0.x` patch 不允许已知破坏；minor 的受控破坏需要完整变化记录和双消费者通过。

## 后果

- 声明面、行为面和真实消费者调用都具有可追溯证据。
- 发布流程需要保存工具版本、module checksum、变化报告和最小消费者证明。
- 任一消费者失败、缺失、超时或 checksum 不一致都阻断稳定 tag。

## 考虑过的替代方案

- 只运行 `go test` 或编译夹具：无法覆盖未调用导出符号与行为兼容性。
- 只维护文本 snapshot：无法可靠判断 Go 语义兼容，也不覆盖 wire 行为。
- 框架 CI 直接读取消费者源码：权限过宽并破坏消费者安全边界。

## 链接

- [公共 API 与行为兼容性合同](../design/public-api-compatibility-contract.md)
- [消费者合同策略](../design/consumer-contracts.md)
- [维护与发布治理](../design/maintenance-governance.md)
