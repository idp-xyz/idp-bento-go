# 双消费者候选合同证明格式

状态：仓内格式与校验器已实现；GitHub Actions/WORM/OIDC/DSSE 配置已确认，外部身份与部署未绑定

日期：2026-07-30

## 目标与边界

本文把[消费者合同策略](./consumer-contracts.md)已经确认的最小可追溯结果固定为严格 JSON v1，并由 `internal/cmd/proofcheck` 校验 TMS、Parcel 是否对同一不可变候选返回通过结果。

证明只描述技术兼容性，不是产品测试报告或发布授权。它不得包含消费者源码、业务类型、事件 Payload、SQL、数据库快照、租户/主体标识、日志、凭据或失败详情。具体跨仓传输、签名、不可变存储、超时和批准身份仍属于 `B-05` 与 `B-04`。

## JSON v1

每个消费者产生一个 UTF-8 JSON 对象。未知字段、尾随 JSON 值和缺失必填字段均失败。

| 字段 | 规则 |
| --- | --- |
| `format_version` | 必须为整数 `1` |
| `consumer` | TMS 文件必须为 `TMS`；Parcel 文件必须为 `PARCEL` |
| `consumer_commit` | 消费者仓库完整小写 Git object ID；只接受 40 位 SHA-1 或 64 位 SHA-256 十六进制，并必须等于协调命令给出的该消费者 `B-06` 预期提交 |
| `candidate_version` | 必须与协调命令给出的合法预发布 SemVer 完全一致，例如 `v0.1.0-rc.1` |
| `module_path` | 必须为 `go.idp.xyz/idp-bento-go` |
| `module_checksum` | 必须与协调命令给出的 Go module `h1:` SHA-256 checksum 完全一致 |
| `contract_suite_version` | 1 至 128 字节、无空白的可打印 ASCII 标识；必须与协调命令期望值完全一致 |
| `result` | `proofcheck` 只接受 `PASS`；`FAIL`、未知值、缺失证明和跳过都阻断稳定 tag |
| `completed_at` | 可由 `time.RFC3339Nano` 解析的完成时间 |

以下只是格式示例，不是实际候选证明：

```json
{
  "format_version": 1,
  "consumer": "TMS",
  "consumer_commit": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "candidate_version": "v0.1.0-rc.1",
  "module_path": "go.idp.xyz/idp-bento-go",
  "module_checksum": "h1:AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
  "contract_suite_version": "r05-v1",
  "result": "PASS",
  "completed_at": "2026-07-30T08:00:00Z"
}
```

Parcel 使用相同字段，但 `consumer` 为 `PARCEL`，`consumer_commit` 为 Parcel 自身不可变提交。

## 生成责任

- TMS、Parcel 各自在自己的受保护 CI 和安全边界内生成证明。
- `module_checksum` 必须来自消费者实际获取并测试的精确候选 module，不得根据源码目录自行发明哈希，也不得使用本地 `replace`。
- `consumer_commit` 必须是运行合同的真实消费者提交；分支名、短 SHA、工作区路径或 CI run number 不能替代。
- `contract_suite_version` 标识消费者执行的合同矩阵版本。两个证明必须使用协调作业指定的同一值，避免一方使用陈旧合同。
- 失败或超时可以由平台保存诊断，但传回框架的稳定发布判断仍是失败关闭；不得把失败详情塞入证明 JSON 绕过最小数据边界。

## 协调校验

不可变 RC 和真实 checksum 已存在后运行：

```powershell
go run ./internal/cmd/proofcheck `
  -candidate v0.1.0-rc.1 `
  -checksum 'h1:<真实 module checksum>' `
  -contract-suite r05-v1 `
  -tms-commit '<TMS 完整预期提交>' `
  -parcel-commit '<Parcel 完整预期提交>' `
  -tms D:\evidence\tms.json `
  -parcel D:\evidence\parcel.json
```

`proofcheck` 只读两个文件，不更新、签名或移动证明。任一文件不存在、JSON 不严格、消费者身份错误、提交不完整或不等于预期基线、候选/checksum/合同版本错配、结果非 `PASS` 或时间非法都会返回非零退出码。

## 与发布授权的关系

`proofcheck` 通过只证明两个输入文件在内容层满足同候选、同合同和预期消费者提交。稳定 tag 前，受保护平台仍必须额外证明：

- 两个文件来自受信任的 TMS/Parcel 工作流，而不是本地伪造。
- 工作流针对同一不可变 RC 和 module checksum，且在规定超时内完成。
- 证明、候选日志和 `candidatecheck` 输出存入不可变审计位置。
- `B-04` 要求的维护者、双消费者代表和适用安全批准已经满足。
- 稳定 tag 由短期最小权限 CI 身份创建并受 Ruleset 防移动，DSSE 发布授权签名绑定版本、目标提交和候选证据；个人不能直接发布。

这些平台事实不能由仓库中的可编辑 JSON 自证。GitHub Actions 的精确来源与权限见[GitHub Actions 候选与发布合同](./github-actions-release-contract.md)，Object Lock、OIDC、manifest 与 DSSE 见[WORM 发布证据与签名合同](./worm-release-evidence-contract.md)；在 `B-04`、`B-05` 完全绑定前，示例或本地生成文件都不能解除发布门禁。

## 当前状态

严格读取、checksum/SemVer/Git object ID、预期 TMS/Parcel 提交、双消费者同候选比较、失败结果、候选/合同错配、未知字段和尾随 JSON 负向测试已经实现。真实不可变候选 `v0.1.0-rc.1` 已经存在，TMS 在提交 `f8eb9c3`、Parcel 在提交 `960a516` 都已从正式 module path 锁定同一 checksum，两侧合同作业均已在真实 PostgreSQL 16 上完整执行通过。两侧尚未按本文格式产出正式证明文件，因此 `proofcheck` 尚未产生任何发布结论。
