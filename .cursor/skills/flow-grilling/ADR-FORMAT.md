# ADR 格式

本仓库是单 context 仓库，所有 ADR 落在 `docs/adr/`。

## 编号

扫一遍 `docs/adr/`，取现有最大编号加一，文件名 `NNNN-slug.md`。

## 结构

沿用既有 ADR 的中文结构，写成 `docs/adr/0002-core-adapter-boundary.md` 那样：

```md
# ADR-NNNN：<决定的简短标题>

状态：已接受

日期：YYYY-MM-DD

## 背景

为什么需要这个决定。约束、压力、既有事实。

## 决定

决定了什么。用肯定句写清楚边界，包括明确排除在外的东西。

## 后果

这个决定带来什么，以及代价是什么。

## 考虑过的替代方案

每个替代方案一行，说明它的优点和被否决的理由。

## 链接

- [相关契约](../design/xxx.md)
```

`## 链接` 指向 `docs/design/` 下相关的契约文档，用相对路径。

## 索引

写完 ADR 之后更新 `docs/adr/README.md`，在列表末尾追加一行：

```md
- [ADR-NNNN：<标题>](./NNNN-slug.md)
```

索引是这批 ADR 唯一的入口，同步它才算这份 ADR 写完。
