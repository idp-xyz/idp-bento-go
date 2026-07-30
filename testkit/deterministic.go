package testkit

import (
	"context"
	"errors"
	"sync"
	"time"
)

var ErrIDSequenceExhausted = errors.New("testkit: id sequence exhausted")

// FixedClock 是不可变的固定时钟。
type FixedClock struct {
	now time.Time
}

// NewFixedClock 创建固定时钟；零时间表示测试装配错误并触发 panic。
func NewFixedClock(now time.Time) FixedClock {
	if now.IsZero() {
		panic("testkit: fixed clock requires non-zero time")
	}
	return FixedClock{now: now}
}

// Now 返回构造时提供的精确时间值。
func (c FixedClock) Now() time.Time {
	return c.now
}

// SequenceIDGenerator 按构造顺序并发安全地返回 ID。
type SequenceIDGenerator[ID any] struct {
	mu   sync.Mutex
	ids  []ID
	next int
}

// NewSequenceIDGenerator 防御性复制 ID 序列。
func NewSequenceIDGenerator[ID any](ids ...ID) *SequenceIDGenerator[ID] {
	return &SequenceIDGenerator[ID]{ids: append([]ID(nil), ids...)}
}

// NewID 返回下一个 ID；取消和耗尽都不消费序列元素。
func (g *SequenceIDGenerator[ID]) NewID(ctx context.Context) (ID, error) {
	var zero ID
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	g.mu.Lock()
	defer g.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if g.next >= len(g.ids) {
		return zero, ErrIDSequenceExhausted
	}
	id := g.ids[g.next]
	g.next++
	return id, nil
}
