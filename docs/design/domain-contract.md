# `domain` 包公共合同

状态：已确认

## 目标

为 TMS 与 Parcel 提供最小、业务无关的进程内领域事件合同，同时保持聚合、实体、值对象、标识、业务错误和不变量完全由消费者拥有。

## 已确认决定

- 不提供 `AggregateRoot`、`Entity`、`ValueObject` 基类、嵌入结构或标记接口。
- 不要求消费者聚合实现统一 ID、Revision、Tenant、事件访问或持久化接口。
- 不提供通用领域错误、验证框架、状态机、Specification 或业务规则 DSL。
- `domain` 只公开领域事件元数据类型、事件接口和零值可用的事件缓冲器。
- 领域事件是进程内已经发生的业务事实；集成事件、事件信封、序列化、Outbox 和发布属于 `eventing` 与应用层。

## 首版公共 API

```go
package domain

import "time"

type EventType string

type EventVersion uint32

type Event interface {
	Type() EventType
	Version() EventVersion
	OccurredAt() time.Time
}

type EventBuffer struct {
	// unexported state
}

func (b *EventBuffer) Record(event Event)

func (b *EventBuffer) Pending() []Event

func (b *EventBuffer) Len() int

func (b *EventBuffer) Acknowledge()
```

上述符号构成 `v0.1` 的 `domain` 公共面。不得为了表面完整性增加基础聚合、实体、值对象或领域服务类型。

## Event 合同

- `Type()` 返回消费者拥有的稳定事件类型。框架只要求非空，不规定 TMS、Parcel 的命名空间或业务词汇。
- `Version()` 从 `1` 开始，只表示该领域事件结构的版本，不得复用 Repository `Revision`、策略版本、安全版本或资源授权版本。
- `OccurredAt()` 是事实发生时间，必须非零。消费者使用显式时钟取得时间并在构造事件时写入；事件不得在序列化时临时读取系统时钟。
- 领域事件不包含框架生成的事件 ID、租户、聚合引用、Trace Context、发布状态或重试次数。这些技术信息在映射为集成事件时添加。
- 事件应使用不可变值语义；推荐使用具有值接收器的结构体。框架不深拷贝事件载荷，消费者不得在记录后修改其可观察内容。
- 领域事件只表达已经发生的事实，名称使用过去式业务语言；具体语言属于消费者，不进入框架仓库。

## EventBuffer 合同

- 零值可以直接使用，不需要构造函数或初始化。
- `Record` 按调用顺序追加事件，不自动发布、不序列化、不写 Outbox。
- `Record` 对 nil 事件、空 `EventType`、零 `EventVersion` 或零 `OccurredAt` 触发 panic。这些属于开发契约错误，不是可恢复的业务拒绝；消费者必须先检查业务规则，再构造并记录有效事件。
- 选择 panic 而非返回 error 的权衡：返回 error 会迫使每个记录事件的领域方法都增加错误返回值，把开发期契约违规的处理成本转嫁给全部业务代码路径；而这些条件在正常开发流程中由编译期结构和代码评审排除，运行期出现即表示代码缺陷。代价是框架会在消费者的调用栈中 panic，因此触发条件必须保持极小且稳定，不得扩展为业务校验。
- 零 `OccurredAt` 是最容易被测试触发的一项。使用固定时钟夹具时必须提供非零时间，否则会在领域代码深处触发契约 panic。
- `Pending` 返回保持记录顺序的浅拷贝。修改返回 slice 不得改变缓冲器；事件本身的不可变性由消费者合同保证。
- `Len` 返回当前未确认事件数，不分配事件 slice。
- `Acknowledge` 清空全部当前待处理事件，可以重复调用。
- 只有包含业务写入和适用 Outbox 写入的 Unit of Work 成功提交后才能调用 `Acknowledge`。事务失败、取消或 panic 时必须保留待处理事件。
- 从持久化状态重建聚合时不得把历史状态再次记录为新领域事件；重建后的缓冲器为空。
- `EventBuffer` 不保证并发安全。一个聚合实例在一个命令执行和事务边界内只能由一个 goroutine 修改；`Pending` 到提交完成之间不得并发追加事件。

## 消费者拥有的模型

TMS 与 Parcel 分别定义并维护：

- 聚合根、实体和值对象结构。
- TenantID、聚合 ID、业务键和构造规则。
- 聚合生命周期、状态转换、职责分离和业务不变量。
- 领域错误及其应用层/传输层映射。
- 领域事件具体类型、业务字段和命名空间。
- 聚合如何持有 `EventBuffer`：可以嵌入，也可以使用私有命名字段；框架不要求统一布局。

## 明确拒绝的公共 API

- `BaseAggregate`、`BaseEntity`、通用 `ID()`、`TenantID()` 或 `Revision()` 接口。
- 反射式事件扫描、自动事件命名、自动时间戳或自动序列化。
- 自动从聚合提取并发布事件的 Repository Hook。
- 通用 `DomainError`、错误码注册表或 HTTP/gRPC 映射。
- Event Sourcing、事件重放、Snapshot 或通用状态机。

## 最小测试合同

- EventBuffer 零值可以记录事件。
- 多个事件保持原始顺序。
- `Pending` 返回 slice 副本，修改 slice 不影响缓冲器。
- `Acknowledge` 清空事件且重复调用安全。
- 非法事件元数据触发契约 panic。
- 失败事务不调用 `Acknowledge`；成功事务只调用一次并清空全部已提交事件。
- 重新加载聚合不会生成新的待处理事件。
- 框架代码不依赖任何 TMS 或 Parcel 领域类型。

## 与后续合同的关系

- `application` 必须定义何时读取 `Pending`、如何把领域事件映射为集成事件，以及成功提交后如何调用 `Acknowledge`。
- `eventing` 必须定义集成事件信封、事件 ID、租户/聚合引用、Trace Context 和版本化序列化。
- `testkit` 提供固定时钟，但不公开重复测试框架具体 `EventBuffer` 实现的合同运行器，也不能拥有消费者领域事件。EventBuffer 白盒合同由 `domain` 包内测试验证，消费者合同只验证真实聚合与事务确认顺序。

## 下一步

`R-01` 至 `R-06` 设计合同已经确认，正式 module path 已绑定，最终实施决策简报已经生成。
