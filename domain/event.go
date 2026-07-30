package domain

import (
	"reflect"
	"time"
)

// EventType 表示由消费者拥有的稳定领域事件类型。
type EventType string

// EventVersion 表示领域事件结构版本，从 1 开始。
type EventVersion uint32

// Event 是已经发生的进程内业务事实。
type Event interface {
	Type() EventType
	Version() EventVersion
	OccurredAt() time.Time
}

// EventBuffer 按记录顺序保存尚未确认提交的领域事件。零值可用。
type EventBuffer struct {
	events []Event
}

// Record 记录一个有效事件。无效元数据表示开发契约错误并触发 panic。
func (b *EventBuffer) Record(event Event) {
	if event == nil || isNilEvent(event) {
		panic("domain: record nil event")
	}
	if event.Type() == "" {
		panic("domain: record event with empty type")
	}
	if event.Version() == 0 {
		panic("domain: record event with zero version")
	}
	if event.OccurredAt().IsZero() {
		panic("domain: record event with zero occurred time")
	}
	b.events = append(b.events, event)
}

// Pending 返回保持记录顺序的浅拷贝。
func (b *EventBuffer) Pending() []Event {
	return append([]Event(nil), b.events...)
}

// Len 返回当前尚未确认的事件数。
func (b *EventBuffer) Len() int {
	return len(b.events)
}

// Acknowledge 清空全部当前待处理事件。
func (b *EventBuffer) Acknowledge() {
	clear(b.events)
	b.events = nil
}

func isNilEvent(event Event) bool {
	value := reflect.ValueOf(event)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}
