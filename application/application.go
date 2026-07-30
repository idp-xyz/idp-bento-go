package application

import (
	"context"
	"errors"
	"time"
)

// Handler 是业务无关的应用用例入口。
type Handler[Request, Response any] interface {
	Handle(ctx context.Context, request Request) (Response, error)
}

// HandlerFunc 把函数适配为 Handler。
type HandlerFunc[Request, Response any] func(
	ctx context.Context,
	request Request,
) (Response, error)

// Handle 调用被适配的函数。
func (f HandlerFunc[Request, Response]) Handle(
	ctx context.Context,
	request Request,
) (Response, error) {
	return f(ctx, request)
}

// Clock 提供显式时间来源。
type Clock interface {
	Now() time.Time
}

// IDGenerator 为消费者拥有的 ID 类型生成新值。
type IDGenerator[ID any] interface {
	NewID(ctx context.Context) (ID, error)
}

// TxFunc 是在事务派生 context 中执行的回调。
type TxFunc func(ctx context.Context) error

// Transactor 提供 Required 传播的事务边界。
type Transactor interface {
	WithinTransaction(ctx context.Context, fn TxFunc) error
}

// Transactional 只在事务明确提交成功后返回回调结果。
func Transactional[Result any](
	ctx context.Context,
	transactor Transactor,
	fn func(context.Context) (Result, error),
) (Result, error) {
	var result Result
	err := transactor.WithinTransaction(ctx, func(txCtx context.Context) error {
		value, err := fn(txCtx)
		if err != nil {
			return err
		}
		result = value
		return nil
	})
	if err != nil {
		var zero Result
		return zero, err
	}
	return result, nil
}

var (
	// ErrRollbackOnly 表示嵌套事务已经被永久标记为只能回滚。
	ErrRollbackOnly = errors.New("application: transaction is rollback only")
	// ErrTransactionConflict 表示 context 中存在另一个事务所有者。
	ErrTransactionConflict = errors.New("application: transaction conflict")
	// ErrCommitUncertain 表示无法判断提交是否已经发生。
	ErrCommitUncertain = errors.New("application: transaction commit outcome uncertain")
)
