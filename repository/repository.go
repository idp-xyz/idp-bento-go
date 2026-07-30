package repository

import (
	"context"
	"errors"
)

// Revision 是聚合持久化层的乐观并发令牌。
type Revision int64

// Loaded 把已加载聚合与其当前 Revision 一起返回。
type Loaded[A any] struct {
	Aggregate A
	Revision  Revision
}

// Loader 按消费者拥有的强类型键加载聚合。
type Loader[K, A any] interface {
	Load(ctx context.Context, key K) (Loaded[A], error)
}

// Inserter 创建尚未持久化的聚合。
type Inserter[A any] interface {
	Insert(ctx context.Context, aggregate A) (Revision, error)
}

// VersionedUpdater 使用预期 Revision 原子更新聚合。
type VersionedUpdater[K, A any] interface {
	Update(ctx context.Context, key K, aggregate A, expectedRevision Revision) (Revision, error)
}

var (
	// ErrNotFound 表示作用域内没有可见聚合。
	ErrNotFound = errors.New("repository: not found")
	// ErrAlreadyExists 表示作用域内的聚合身份已经存在。
	ErrAlreadyExists = errors.New("repository: already exists")
	// ErrConflict 表示条件更新未满足预期 Revision。
	ErrConflict = errors.New("repository: conflict")
)
