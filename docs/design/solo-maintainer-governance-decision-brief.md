# 单维护者治理模式与证据披露决策简报

状态：`Draft`；本简报确认前不得开始实现

日期：2026-07-31

## 主题与范围

本简报只决定两件事：治理文档如何诚实描述当前只有一名维护者的事实，以及该事实如何进入发布证据链并被校验器强制。

不在范围内：`B-05` 的平台选择（GitHub Team 或等价受保护平台）、真实维护者与消费者代表人选、任何机器门禁的技术内容、任何公共 API 变更。

## 已验证事实

- `maintenance-governance.md` 第 30 行要求维护组不少于 3 人；第 46 行要求高风险变更的批准来自相互独立账号且作者不能自批；第 52 至 56 行的变更分级表每一档最低评审都是 1 至 2 名非作者维护者；第 82 行要求指定不少于 3 名维护者；第 85 行要求验证任何单一个人都不能绕过门禁发布稳定版本。同一文档第 13 至 15 行把这些条款标为 `CONFIRMED`。
- `framework-baseline.md` 第 35 行 `F-17` 标 `CONFIRMED`，第 124、125、178 行重复同一要求。
- `public-api-compatibility-contract.md` 第 288 行要求治理系统核验双维护者、双消费者和适用安全批准。
- `consumer-proof-format.md` 第 79 行把「`B-04` 要求的维护者、双消费者代表和适用安全批准已经满足」列为稳定 tag 前置条件。
- `implementation-readiness-audit.md` 第 89 行 `B-04` 为 `UNBOUND IDENTITIES`；第 90 行记录当前明确保持 GitHub Free，私有仓库 Ruleset 不执行。
- 实际执行者为一人，不存在第二个独立账号。上述条款不是等待即可满足的门禁，而是与当前组织形态直接矛盾的前提。
- 与人数无关且已经可执行的保证包括：`candidatecheck` 的仓内候选门禁、`proofcheck` 的同候选同套件双消费者比对、`evidencecheck` 的 manifest 与来源校验、tag Ruleset 防移动、WORM `COMPLIANCE` 三年保留、DSSE 绑定版本与目标提交。它们防手滑、事后篡改、版本号复用和跳过测试，防不了单人判断错误。
- `idp-tms` 与 `idp-parcel` 的 `tests/bentocontract/candidate.go` 第 35 至 39 行各自定义 `GovernanceMode = "SOLO_BOOTSTRAP"`、`ApprovalNotice = "NO INDEPENDENT HUMAN APPROVAL"`、`BypassableNotic = "OWNER-BYPASSABLE"`，两侧完全一致，且全工作区没有任何引用。
- `internal/consumerproof/proof.go` 的 `Proof` 只有九个字段，没有治理位；`Read` 使用 `DisallowUnknownFields`，因此新增字段对旧读取器是硬失败。
- 两侧已对 `v0.1.0-rc.2` 产出真实证明并通过 `proofcheck`；该结果由本地运行产生，明确不是发布授权，也从未进入 WORM。
- `internal/releaseevidence` 的 manifest v1 有 `framework` 与 `tms`/`parcel` 分块，同样严格拒绝未知字段。

## 决策一：治理条款的表述

### A. 保留三人要求，等待 `B-04` 绑定

优点是目标形态不动。代价是继续把不可满足的条件标为 `CONFIRMED`，稳定版本在结构上无法发出，并违反仓库约束中「不得把建议描述为已实现能力」——只是方向相反，把做不到的要求描述为已确认的制度。

### B. 删除人工评审要求，只保留机器门禁

优点是文档与现实一致。代价是丢掉目标组织形态；将来增加维护者时没有可回退的合同，需要重新设计职责分离。

### C. 保留目标形态，把当前模式显式声明为单维护者（建议）

角色表与治理原则保留为目标形态。变更分级表的「最低评审」整列改为机器门禁要求。新增「当前治理模式」一节，声明当前处于单维护者模式、职责分离由机器门禁替代、唯一缺失的保证是独立人工判断，并要求该事实随每次发布证据一起公布。`F-17` 同步描述目标形态与当前模式两层。`B-04` 从 `UNBOUND` 改为明确的单人声明状态，不再暗示「将来会绑定」即可解除。

**建议采用 C。**

## 决策二：披露载体与格式版本

### 载体

治理模式是框架侧事实，但三个常量目前在消费者仓库，两者存在语义错位，必须先裁定。

- **A. 只加入消费者证明 JSON。** 改动最小，三个常量立刻有归宿。但由消费者自报框架治理模式语义错误，且两侧可以各报一个值而无人比对。
- **B. 只加入 manifest 的 `framework` 块。** 语义正确，框架侧单点声明，写入 WORM 后不可改。但那三个常量仍然无处可用，消费者也无法表明自己知道正在对一个无独立审批的候选签字。
- **C. 两侧都加，含义不同（建议）。** `manifest.framework` 记录权威 `governance_mode`；消费者证明记录 `acknowledged_governance_mode`；`evidencecheck` 校验两个消费者的值彼此相等且等于 manifest 的值。常量有了归宿，且不可能出现一侧默默不承认。

### 格式版本

`Read` 使用 `DisallowUnknownFields`，新增必填字段对 `v0.1.0-rc.2` 的两份现存证明是硬失败。

- **A. 升 `format_version` 为 `2`，`proofcheck` 同时接受 1 和 2。** 代价是 v1 永久允许没有治理披露的证明，正好保留了本简报要消灭的漏洞。
- **B. 保持 `format_version = 1`，字段必填，作废并重出两份证明（建议）。** 那两份证明本来就不是发布授权，从未进入 WORM，作废成本为零；而允许 v1 无披露会让漏洞长期存在。

`proofcheck` 与 `consumerproof` 现有的未知字段负向测试必须同步更新，否则新字段会被旧断言判为应当失败。

**建议采用载体 C 加格式版本 B。**

## 决策三：常量拼写与 ADR

- `BypassableNotic` 少一个 `e`。它当前零引用，是零成本改名的唯一窗口；一旦进入证据产出代码，改名要同时改动两个消费者仓库并作废已产出证明。建议在同一批改动中改为 `BypassableNotice`。
- 不建议新增 ADR。ADR 只记录难以逆转的决定，而本次声明可以通过增加维护者直接恢复，不满足该门槛。`ADR-0007` 的「双消费者发布门禁」属于机器可证部分，本次不变，其对 `maintenance-governance.md` 的引用保持有效。若确认者判断「以机器门禁替代人工职责分离」本身构成难以逆转的决定，则需要在实现前追加 ADR。此项由确认者裁定。
- 披露字段一旦随发布写入 WORM 即不可撤回，因此字段名称与取值语义必须在本简报固定，不留到实现阶段决定。

## 主要代价与缓解

- 公开声明没有独立审批会降低外部对稳定版本的信任。缓解方式是同时公布机器门禁清单与不可篡改证据位置，让读者能区分「无人复核判断」与「无法证明构建来源」两类风险。
- 变更分级表改为机器门禁要求后，短期内失去对判断错误的拦截。缓解方式是保留 24 小时冷静期，并在增加第二名维护者时按目标形态恢复评审列，无需重写文档结构。
- 两份 `rc.2` 证明作废需要两侧重跑合同。两侧产出器要求全部用例通过且工作树干净，重跑成本可控。

## 改动清单

| 位置 | 改动 |
| --- | --- |
| `docs/design/maintenance-governance.md` 第 13、30、46、52-56、82、85、91 行 | 决策状态项、治理原则人数、独立账号要求、变更分级最低评审列、人员绑定门禁；新增「当前治理模式」一节 |
| `docs/design/framework-baseline.md` 第 35、124、125、178 行 | `F-17` 及三处重复表述 |
| `docs/design/public-api-compatibility-contract.md` 第 288 行 | 双维护者核验表述 |
| `docs/design/consumer-proof-format.md` JSON v1 字段表、第 79 行 | 新增披露字段规则；修改稳定 tag 前置条件表述 |
| `docs/design/worm-release-evidence-contract.md` manifest v1 字段表 | `framework` 块新增权威 `governance_mode` |
| `docs/design/implementation-readiness-audit.md` 第 89 行 | `B-04` 状态与变更记录 |
| `docs/design/release-candidate-runbook.md` 第 71 行 | 批准人表述 |
| `internal/consumerproof/proof.go`、`internal/releaseevidence` | 新增字段、校验与跨文件一致性比较 |
| `internal/cmd/proofcheck`、`internal/cmd/evidencecheck` 及其测试 | 正负向用例，含未知字段断言更新 |
| 两侧 `tests/bentocontract/candidate.go` 与证明产出器 | `BypassableNotice` 改名；常量写入证明 |

## 后续门禁

1. 本简报确认前不得开始实现。
2. 确认后的实现顺序：先改常量名与设计文档，再改 `consumerproof` 与 `releaseevidence` 及其测试，最后重出两侧证明并重跑 `proofcheck` 与 `evidencecheck`。
3. 本简报不解除任何发布门禁。`B-05` 仍独立阻断受保护 CI、不可变 RC 与稳定发布；解决 `B-05`（公开仓库或受保护平台）之后，稳定版本才不再卡在写死的三人要求上。
