# `application` 与事务公共合同

状态：已确认

## 目标

为 TMS 与 Parcel 提供最小、显式、可测试的应用处理和事务边界，同时避免 Command Bus、Service Locator、Repository 注册表或隐藏自动重试进入框架核心。

## 已确认决定

- Command 与 Query 是消费者拥有的请求结构，不定义框架标记接口或基类。
- 使用一个泛型 `Handler[Request, Response]` 和适配函数，不为 Command/Query 复制两个签名相同的接口。
- Unit of Work 不实现为持有全部 Repository 的通用可变对象；公共事务端口是 `Transactor`。
- 事务采用 `Required` 传播：无现有事务时创建，同一 Transactor 嵌套调用时加入现有事务。
- 事务句柄通过派生 context 隐式传播，业务作用域与授权身份必须显式传参；区分依据是失败模式而非传递机制。
- 事务写入参与者采用严格模式：context 中没有事务句柄时返回基础设施错误，不静默回退到连接池；读取能力必须显式选择事务读取或允许连接池读取的路径。
- 不支持 `RequiresNew`、通用保存点、跨数据库事务、自动重试或分布式事务。
- Clock 与 ID Generator 位于 `application`，应用层取得时间/ID 后显式传入消费者领域模型。
- 应用与领域错误使用 Go `error`、`errors.Is` 和 `errors.As`；不提供通用 Result、错误码或传输状态映射。

## 首版公共 API

```go
package application

import (
	"context"
	"errors"
	"time"
)

type Handler[Request, Response any] interface {
	Handle(ctx context.Context, request Request) (Response, error)
}

type HandlerFunc[Request, Response any] func(
	ctx context.Context,
	request Request,
) (Response, error)

func (f HandlerFunc[Request, Response]) Handle(
	ctx context.Context,
	request Request,
) (Response, error)

type Clock interface {
	Now() time.Time
}

type IDGenerator[ID any] interface {
	NewID(ctx context.Context) (ID, error)
}

type TxFunc func(ctx context.Context) error

type Transactor interface {
	WithinTransaction(ctx context.Context, fn TxFunc) error
}

func Transactional[Result any](
	ctx context.Context,
	transactor Transactor,
	fn func(context.Context) (Result, error),
) (Result, error)

var (
	ErrRollbackOnly        = errors.New("application: transaction is rollback only")
	ErrTransactionConflict = errors.New("application: transaction conflict")
	ErrCommitUncertain     = errors.New("application: transaction commit outcome uncertain")
)
```

具体实现可以拆分到多个 Go 文件，但 `v0.1` 不增加 Bus、Dispatcher、Middleware 注册表或依赖注入容器。

## Handler 合同

- 消费者使用具体结构体定义 Command 和 Query；框架不要求 marker method、名称后缀或序列化格式。
- `Handler` 是应用用例入口合同，不是 HTTP、gRPC 或消息 Handler。
- `HandlerFunc` 允许函数适配为 Handler，行为必须与直接调用函数相同。
- 无返回载荷的用例使用消费者自己的空响应类型或 `struct{}`；框架不定义 `Unit`/`None`。
- Handler 返回 `(Response, error)`，不包装为通用 Result。领域错误、授权错误和传输映射由消费者拥有。
- 框架不提供全局 Handler 注册、反射路由、自动中间件顺序或 Service Locator。消费者在 bootstrap 中显式装配。
- Query 默认不启动写事务；需要一致性快照的查询由消费者显式使用事务能力或专属查询端口。

## Clock 与 IDGenerator

- `Clock.Now()` 返回消费者可用于领域事实的当前时间；生产实现必须返回有明确时区的时间，持久化和事件映射统一转换为 UTC。
- `IDGenerator.NewID(ctx)` 支持本地或外部生成器，因此保留 context 与 error。消费者拥有具体 ID 类型及其合法性。
- Domain 聚合不依赖 `application.Clock` 或 `IDGenerator`；应用 Handler 先取得时间和 ID，再作为显式参数调用领域构造或行为。
- 框架不提供全局时钟、全局随机源或隐式 ID Hook。

## Transactor 传播语义

### 为什么事务句柄可以隐式传播而租户不可以

事务句柄通过派生 context 隐式传播，业务作用域必须通过显式参数传递。这两条规则并存不是自相矛盾，区分依据是失败模式而不是传递机制本身。

取错租户是静默失败。适配器从 context 补全出错误的租户后，SQL 依然合法、查询依然成功、类型系统无从察觉，结果是跨租户数据泄露，而它可能在很长时间内不被发现。这类风险只能通过强制显式传参消除，因此 Repository 键、`eventing.Envelope.Scope` 和一切授权范围都必须由调用方显式提供。

取不到事务句柄是响亮失败。适配器发现 context 中没有事务时立即返回基础设施错误，调用在第一次执行时就失败，不存在"看起来成功但结果错误"的中间状态。事务句柄也不携带任何业务语义，误用不会造成越权。

因此规则是：安全属性和业务身份必须显式；纯粹的请求生命周期基础设施可以隐式，前提是缺失时必须立即失败而不是降级。`Transactor` 的派生 context 属于后者，`context.Context` 仍然不得成为租户、主体、授权范围或业务标识的来源。

### 事务外调用与读取路径

`Inserter`、`VersionedUpdater`、`OutboxWriter`、Inbox 状态写入以及其他会改变持久化状态的事务参与者在 `v0.1` 采用严格模式：context 中找不到所属 `Transactor` 的事务句柄时返回基础设施错误，不静默回退到连接池。

`Loader` 与消费者查询端口可以存在两种明确路径，但实现和装配不得含混：事务读取路径要求活动事务；允许事务外读取的路径在没有事务时显式使用构造时注入的连接池，在存在同一数据库事务时加入该事务。context 已绑定另一数据库/Transactor 时仍返回 `ErrTransactionConflict`，不能绕过到连接池。

静默回退对写入最危险——遗漏 `WithinTransaction` 的代码在开发和测试环境下看似成功，只有生产并发下才暴露为业务写入与 Outbox 不一致。读取路径允许连接池不是静默降级，而是消费者在 Repository 构造和方法实现中主动选择的合同；消费者应使用 `ReadExecutor` 与 `RequireExecutor` 这类不同名称使选择在代码审查中可见。

### 外层调用

- 当前 context 没有此 Transactor 创建的事务时，`WithinTransaction` 开始一个事务并把不透明事务句柄绑定到派生 context。
- 回调必须使用传入的派生 context 调用 Repository、Outbox 和其他事务参与者。
- 派生 context 只在回调动态范围内有效，不能保存到结构体、goroutine、缓存或回调结束后的异步任务。
- 回调返回 nil 且 context 未取消时尝试提交；只有提交成功，`WithinTransaction` 才返回 nil。

### 同一 Transactor 嵌套

- 嵌套调用加入现有事务，不创建新事务或保存点；只有最外层调用负责提交或回滚。
- 内层回调返回错误时，事务被永久标记为 rollback-only，并向调用方返回该错误。
- 即使外层捕获并忽略内层错误，最外层也必须回滚并返回可通过 `errors.Is(err, ErrRollbackOnly)` 判定的错误。
- 内层发生 panic 时先标记 rollback-only，再原样重新 panic；外层最终不能提交。

### 不同 Transactor 冲突

- context 已绑定一个框架事务时，另一个 Transactor 不得隐式开启第二个独立事务。
- 该场景返回包装了 `ErrTransactionConflict` 的错误，阻止框架伪装跨数据库原子性。

## 提交、回滚与错误语义

- 回调返回错误：回滚，并保留回调原始错误；回滚也失败时使用 `errors.Join` 或等价方式同时保留两个原因。
- context 在提交前取消或超时：回滚，保持 `context.Canceled` 或 `context.DeadlineExceeded` 可通过 `errors.Is` 判定。
- 回调 panic：执行 best-effort 回滚后原样重新 panic，不转换为普通 error。
- 提交明确失败且数据库确认未提交：返回底层提交错误，不返回业务响应。
- 连接丢失等场景无法确定提交结果：返回包装了 `ErrCommitUncertain` 的错误。调用方不得把它当作普通回滚后自动重试。
- `Transactional[Result]` 只有在提交成功后返回回调 Result；任何错误返回 Result 的零值和 error，防止调用方使用未提交结果。
- 框架不自动重试序列化失败、死锁、连接错误、`ErrCommitUncertain` 或 Repository `ErrConflict`。消费者只有在命令幂等、重新加载并重新授权后才能显式重试。

## 领域事件与 Unit of Work 顺序

命令 Handler 的推荐顺序：

1. 使用显式作用域键和明确允许事务外读取的 Loader 加载聚合及其 Revision；若用例需要一致性快照，也可以把加载放入第 4 步的事务回调。
2. 执行授权、业务规则和领域行为，领域聚合把事实记录到 `EventBuffer`。
3. 在进入事务前取得 `Pending()` 的稳定事件列表并映射为消费者拥有的集成事件候选；映射不得发布。
4. `WithinTransaction` 内保存聚合、授权证据及适用 Outbox 记录。
5. `WithinTransaction` 返回 nil 后调用 `EventBuffer.Acknowledge()`。
6. 事务失败、取消、panic 或提交结果不确定时不得调用 `Acknowledge()`。

框架不自动扫描任意聚合、调用 Repository、映射领域事件或写 Outbox。消费者应用用例明确组织这些步骤，保证业务写入和 Outbox 处于同一事务。

## 关键场景：提交结果不确定后的恢复

`ErrCommitUncertain` 是本合同中最微妙的状态，其正确处理依赖分散在三份文档中的规则协同。本节集中呈现完整路径。

场景：Handler 加载聚合（`Revision=7`）、执行领域行为、在 `WithinTransaction` 内保存聚合并 `Enqueue` 集成事件，提交阶段数据库连接中断，无法判断事务是否已提交。

框架此时的行为是：`WithinTransaction` 返回包装 `ErrCommitUncertain` 的错误，`Transactional[Result]` 返回 Result 零值，Handler 不得调用 `EventBuffer.Acknowledge()`，因此聚合的待处理事件被完整保留。框架不自动重试。

`ErrConflict` 不能证明本命令已经生效：Revision 变化也可能由另一个并发命令造成。稳定 `EventID` 只能阻止重复 Outbox 记录，不能阻止聚合状态、授权证据或其他本地副作用被第二次修改。因此框架不得把“重新加载后发生版本冲突”描述为提交成功证明。

消费者只有具备命令级幂等能力时才可以自动恢复。推荐使用稳定 `CommandID`，并在同一业务事务内保存命令处理记录、结果引用或足以查询既有结果的业务键。收到 `ErrCommitUncertain` 后：先丢弃内存聚合并按 `CommandID` 查询处理结果；已存在则返回已记录结果，不重新执行；确认不存在时才重新加载并以相同 `CommandID` 和 EventID 执行；仍无法判断时继续返回结果不确定，由人工或后续协调流程处理。

如果消费者没有命令处理记录或等价的业务幂等证明，`ErrCommitUncertain` 必须直接暴露给上层并禁止自动重试。即使相同 `Source + EventID` 会让再次 `Enqueue` 返回 `ErrAlreadyEnqueued`，它也不能撤销已经重复执行的业务状态变化。

## 明确拒绝的能力

- Command Bus、Query Bus、Mediator、反射 Dispatcher 或全局 Handler Registry。
- 持有任意 Repository map 的通用 Unit of Work。
- `RequiresNew`、自动保存点、跨数据库两阶段提交或 Saga 自动化。
- 自动事务重试、自动领域事件发布或 Repository Hook。
- 从 `context.Context` 推断租户、主体、授权范围或业务身份。
- 通用 HTTP/gRPC 错误码、Response Envelope 或业务 Result 类型。

## 最小测试合同

- HandlerFunc 与 Handler 具有相同行为，并正确传播 context、响应和错误。
- Clock、IDGenerator 能被固定测试实现替换，不依赖全局状态。
- 外层事务只提交一次；错误、取消和 panic 均执行回滚。
- 同一 Transactor 嵌套只使用一个事务，内层错误使外层 rollback-only。
- 不同 Transactor 嵌套返回 `ErrTransactionConflict`。
- 提交结果不确定时可以通过 `errors.Is` 识别 `ErrCommitUncertain`。
- `Transactional` 在失败时返回 Result 零值，在提交成功后才暴露结果。
- 成功提交后才确认领域事件；所有失败路径均保留待处理事件。
- 事务 context 不能在回调结束后继续用于 Repository 操作或 goroutine。
- 写事务参与者在没有事务句柄的 context 上返回基础设施错误，不静默使用连接池执行写入。
- 允许事务外读取的 Loader 明确使用 ReadExecutor；存在匹配事务时加入事务，存在其他 Transactor 时返回冲突。
- 提交结果不确定后，只有具备稳定 CommandID 和事务性命令处理记录或等价业务幂等证明时才能自动恢复；EventID 去重不能替代命令幂等。

## 下一步

PostgreSQL Transactor 已实现 Required 传播、rollback-only、owner 冲突和提交结果不确定分类，并通过单元测试与 PostgreSQL 16.12 仓库验收。下一步在 TMS、Parcel 候选合同中验证真实 Repository 与 Outbox/Inbox 的事务组合。
