# 测试画像

本仓库是 Go 框架库。`flow-tdd` 读这份文件来定位本仓库的测试约定。

## 依赖

标准库 `testing`。核心包只依赖标准库，测试遵守同一条线——需要断言、mock 或夹具时，用标准库和 `testkit` 里已有的东西拼出来。

## 断言

got/want 句式，失败信息带上实际值：

```go
if got := buffer.Len(); got != 2 {
    t.Fatalf("Len() = %d, want 2", got)
}
```

结构体比较打印 `%#v`，让失败信息本身就能定位：

```go
if pending := buffer.Pending(); len(pending) != 2 || pending[0] != first {
    t.Fatalf("Pending() = %#v", pending)
}
```

## 表驱动

多个用例用表加 `t.Run`，每个子用例独立命名、独立失败：

```go
for _, tc := range []struct {
    name string
    in   EventVersion
    want error
}{
    {name: "zero version rejected", in: 0, want: ErrInvalidVersion},
    {name: "first version accepted", in: 1, want: nil},
} {
    t.Run(tc.name, func(t *testing.T) {
        if got := Validate(tc.in); !errors.Is(got, tc.want) {
            t.Fatalf("Validate(%d) = %v, want %v", tc.in, got, tc.want)
        }
    })
}
```

## 合同运行器

端口的语义测试走 `testkit`：

- `testkit.RunRepositoryContract`
- `testkit.RunOutboxContract`
- `testkit.RunInboxContract`

它们接收工厂，工厂为每个子测试构造**真实实现**。这是 `docs/design/testkit-contract.md` 选定的路线：框架提供合同，实现由消费者或适配器提供。

新增合同用例时改运行器本身，让所有实现同时受益。

## 确定性替身

`testkit` 提供三样，用来消除不确定性：

- **固定时钟**——不可变。需要推进时间时用新的时间造一个新实例。
- **顺序 ID 生成器**——并发安全；取消的调用不消耗 ID，序列耗尽返回稳定错误。
- **记录式 Publisher**——校验 context 和信封之后再记录，保存和返回的信封都深拷贝 Payload。

时间用固定时钟，ID 用顺序生成器，这样测试在任何机器任何时刻都得到同一个结果。

## 依赖方向

`testkit` 是测试专用的公共包，生产包从它这里取不到任何东西——这条有依赖图门禁把守。写测试时顺手确认改动没有把生产代码逼向 `testkit`。

## 范例

`domain/event_test.go` 是这套约定的活样本：最小技术类型（`testEvent`、`pointerEvent`）、固定时间、got/want、内部切片泄漏的防御性断言。
