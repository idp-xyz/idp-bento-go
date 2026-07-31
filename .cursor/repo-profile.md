# 仓库画像

`flow-*` 技能读这份文件来定位本仓库的文档去处和边界。技能本身在所有 IDP 仓库里逐字节相同，差异全部落在这里。

## 术语表

路径：`docs/design/domain-language.md`

懒创建——第一个术语敲定时才生成。条目格式：

```md
**术语名**：
一到两句话说明它*是什么*。
_避免_：不推荐使用的近义词
```

这个文件只放定义。规格、草稿和实现细节各有各的去处。

现有的 `docs/design/domain-contract.md` 是领域契约，讲的是约束而非词汇，两者分开。

## ADR

目录：`docs/adr/`

单 context 仓库，所有 ADR 落在同一个目录，编号连续。扫一遍目录取最大编号加一，文件名 `NNNN-slug.md`。

结构沿用既有 ADR，参照 `docs/adr/0002-core-adapter-boundary.md`：

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

`## 链接` 指向 `docs/design/` 下的契约文档，用相对路径。

**索引**：`docs/adr/README.md` 是这批 ADR 唯一的入口。写完 ADR 之后在列表末尾追加一行，同步它才算写完：

```md
- [ADR-NNNN：<标题>](./NNNN-slug.md)
```

## 设计文档

`docs/design/` 拥有框架目标、包边界、兼容性契约、发布风险和未决问题。决策简报也放这里。

## 缝

公共缝是包的导出面：`domain`、`repository`、`eventing`、`application`、`testkit`。

适配器（`postgres` 及其子包）的缝是它实现的那个端口，它的 SQL 留在缝的另一侧。

## 硬边界

- **TMS 与 Parcel 的业务语言、规则、数据和生命周期不得进入本仓库。** 这是所有权边界，无法正面表述。
- 框架不导入产品包，公共 API 需要真实消费者用例和兼容性测试证明。
- 核心包只依赖 Go 标准库；可选基础设施依赖隔离在明确的包和端口之后。
- 未定的选择标成 `Draft` 或 `UNBOUND`，让读者一眼看出它还没落地。
- 决策简报确认之后再开始框架实现。

## 评审标准

`flow-code-review` 的标准轴以这些文档为准，按相关性取用：

- `AGENTS.md` 的「代码边界」一节——框架不导入产品包、公共 API 需要消费者用例和兼容性测试证明、核心包只依赖标准库。
- `docs/design/package-boundaries.md`——包之间允许的依赖方向。
- `docs/design/public-api-compatibility-contract.md`——公共 API 的兼容性分级与破坏性变更的判定。
- `docs/design/` 下与改动区域对应的契约文档。
- 改动区域覆盖到的 ADR。

`gofmt`、`go vet` 和 CI 里的门禁已经把守的东西，评审时跳过。

## 规格来源

本仓库不用 issue tracker。规格轴按这个顺序找依据：

1. 用户直接指定的决策简报或 ADR 路径。
2. 本轮 `flow-grilling` 达成的共识（还在会话里）。
3. `docs/design/` 下与改动主题对应的决策简报。
4. 改动区域覆盖到的 ADR。

都找不到时跳过规格轴，在报告里写明「无规格可依」。

## 验证

**最小相关**——改动落在哪个包就跑哪个：

```
go test -count=1 ./<包>/
```

**完整验证**——CI 跑的同一道门禁：

```
IDP_BENTO_POSTGRES_DSN=<dsn> go run ./internal/cmd/candidatecheck -initial-v0-1
```

它依次检查 Go 工具链版本（1.26.5）、module 身份、`go mod tidy -diff`、`gofmt -l -e`、公共 API 快照、Envelope 兼容性清单、生产依赖图，然后跑 `go vet ./...` 和 `go test -count=1 ./...`。

候选测试连真实 PostgreSQL 跑，`IDP_BENTO_POSTGRES_DSN` 是必需的——缺了它门禁会直接失败而不是跳过。

## 测试

见 [test-profile.md](test-profile.md)。
