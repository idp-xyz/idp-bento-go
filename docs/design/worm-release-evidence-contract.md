# WORM 发布证据与签名合同

状态：配置已确认；存储、OIDC 与签名部署未验证

日期：2026-07-30

## 主题与范围

本文固定 `B-05` 的权威发布证据格式、S3-compatible Object Lock、保留策略、GitHub OIDC 信任边界和签名回执。本文不创建 bucket、角色、KMS key、GitHub Environment、对象、tag 或 RC，也不把示例值描述为真实证据。

## 已确认决定

| 项目 | 结论 |
| --- | --- |
| 权威存储 | 支持 Versioning 与 Object Lock 的 S3-compatible 存储 |
| 锁定模式 | `COMPLIANCE`；禁止使用可由特权身份绕过的 `GOVERNANCE` 作为稳定发布证据 |
| 保留期 | 每个成功发布证据对象从写入时起保留 3 个日历年；策略可以延长，既有对象不得缩短 |
| 写入身份 | GitHub Actions `release-evidence` Environment 通过 OIDC 换取最长 15 分钟的临时身份，不使用静态 S3 access key |
| 加密 | 传输使用 TLS，静态使用客户管理密钥的服务端加密；证据不因加密而放宽最小数据边界 |
| 签名 | KMS/HSM 托管的 ECDSA P-256、SHA-256 对 DSSE 回执签名；私钥材料不得进入 GitHub Secret、runner 或仓库 |
| 稳定 tag | Git ref 由 Ruleset 保护且不可移动；稳定版本、目标提交和候选证据之间的绑定由 DSSE 发布授权签名，不依赖个人 GPG key |
| 内容校验 | `evidencecheck` 严格验证 manifest、两份原始证明、GitHub 来源字段、摘要和 40 分钟时间窗；WORM、OIDC、KMS 和 DSSE 必须由平台另行验证 |

三年保留覆盖当前最长 12 个月支持窗口，并保留至少两年的审计余量。法律、客户或安全要求可以对新对象采用更长保留或 Legal Hold，但不能缩短已经进入 Compliance Lock 的对象。

该决定记录为 [ADR-0009](../adr/0009-worm-oidc-release-evidence.md)。Object Lock Compliance 会让误写对象在三年内也无法由管理员删除，代价明确且不可逆，因此不能只作为普通运行参数记录。

## 对象布局

每次候选协调使用唯一 `request_id`，对象前缀固定为：

```text
releases/go.idp.xyz/idp-bento-go/<candidate_version>/<request_id>/
  manifest.json
  proofs/tms.json
  proofs/parcel.json
  receipt.dsse.json
```

- `candidate_version` 必须是合法预发布 SemVer；对象 key 不接受分支名、用户输入路径片段或 URL 编码绕过。
- `request_id` 必须是 32 位小写十六进制随机值。
- 三个内容对象使用 `If-None-Match: *` 或等价原子“仅当不存在才创建”条件上传，禁止覆盖。
- 每次上传必须取得不可变 `version_id`、服务端 SHA-256、`COMPLIANCE` 模式和 `retain_until`；ETag 不能替代 SHA-256。
- `receipt.dsse.json` 在前三个对象写入并回读验证后生成，也使用相同锁定模式和保留截止时间。
- 重试使用新的 `request_id`；同一前缀出现已存在对象时失败，不通过新 version 静默覆盖旧证据。

稳定版本另写一份签名授权：

```text
stable/go.idp.xyz/idp-bento-go/<stable_version>/<request_id>/
  release-authorization.dsse.json
```

发布授权在推送稳定 tag 前签名并锁定，内容固定稳定版本、目标框架提交、候选版本、module checksum 和候选 `receipt.dsse.json` 摘要。tag 推送后必须回读远端 ref，确认仍指向已签名目标；失败时保留授权并失败关闭，不改写授权或复用另一个提交。

## Object Lock 与加密门禁

bucket/container 在首次证据写入前必须满足：

- Versioning 与 Object Lock 已启用，并有自动化测试证明上传对象返回不可变 version ID。
- 默认或请求级保留均为 `COMPLIANCE`，`retain_until` 按 UTC 从写入时间增加 3 个日历年。
- bucket policy 拒绝 `DeleteObject`、`DeleteObjectVersion`、缩短保留、绕过治理锁、未加密上传和缺失 SHA-256 checksum 的上传。
- writer 只能向 `releases/go.idp.xyz/idp-bento-go/*` 与 `stable/go.idp.xyz/idp-bento-go/*` 创建对象；不能列出其他前缀、读取消费者存储、删除对象或修改保留。
- verifier 只能按精确 bucket/key/version 读取对象、保留状态和 checksum；不能写入、删除或改变锁定。
- 使用客户管理加密密钥；密钥删除保护期不得早于最后一份证据到期，历史解密/验证能力必须覆盖全部保留期。
- 开启对象级审计日志，记录写入、读取、保留和拒绝操作；日志本身不包含证明正文或凭据。

创建错误对象也不能作为删除例外。修复方式是用新 `request_id` 写入纠正对象，并在后续授权中只引用正确 version；错误对象保持锁定并记录失败原因。

## GitHub OIDC 信任

写入和签名身份只接受 GitHub OIDC，至少约束：

| Claim/平台约束 | 要求 |
| --- | --- |
| `aud` | 精确等于存储 STS 配置的 audience，不接受通配 |
| `sub` | 精确等于 `repo:idp-xyz/idp-bento-go:environment:release-evidence` |
| repository | 固定 GitHub repository ID 与 owner ID，不能只信任可重命名 owner/name |
| workflow | 固定受保护 workflow path/ref；第三方复用 workflow 也必须固定完整 commit SHA |
| Environment | `release-evidence` 只允许受保护默认分支部署，并受 `B-04` 批准规则保护 |
| session | 最长 15 分钟，限制 session name 包含 run ID/attempt，不允许角色链继续扩权 |

writer 与 verifier 使用不同角色。候选证据 job 只在内容校验通过后取得 writer，上传完成即丢弃该身份，再以只读 verifier 回读精确 version、锁定状态与 checksum；`stable-release` job 也只使用 verifier 重新验证对象和 DSSE，不能复用 writer 凭据。

OIDC provider 不支持 repository ID 或 workflow 限制时，必须增加受信任 broker 执行等价校验；不能退化为只匹配组织通配符。fork PR、普通 PR、消费者 workflow 和本地命令不能取得证据角色。

## Release Evidence Manifest v1

`manifest.json` 是严格 UTF-8 JSON。未知字段、尾随 JSON、非法时间、缺失字段或来源错配全部失败。权威结构由 `internal/releaseevidence` 实现，顶层字段如下：

| 字段 | 规则 |
| --- | --- |
| `format_version` | 固定整数 `1` |
| `request_id` | 32 位小写十六进制 |
| `candidate` | 候选版本、框架提交、正式 module path/checksum、Git origin 和 API 比较身份 |
| `contract_suite_version` | 与两个证明及协调输入完全一致 |
| `requested_at` / `deadline_at` | RFC3339Nano；窗口大于 0 且不超过 40 分钟 |
| `framework` | 框架 repository/workflow/run、精确工具/镜像、`candidatecheck` 结果与输出摘要 |
| `tms` / `parcel` | 消费者 repository/workflow/run/actor、预期提交、Artifact/证明摘要和开始完成时间 |
| `created_at` | 不早于两个消费者完成时间且不晚于协调 deadline |

`candidate.api_comparison` 固定：

- `mode=initial-v0-1` 只允许 `v0.1.0-rc.1`，`baseline_version` 与 `baseline_sha256` 必须为空，`candidate_sha256` 必填。
- `mode=immutable` 要求合法前序版本、前序 API 制品 SHA-256 和候选 API 制品 SHA-256。

`framework` 必须记录正整数 repository/workflow/run ID 与 run attempt、workflow 完整提交/ref、runner image、精确 Go patch、带 `@sha256:` digest 的 PostgreSQL image、`PASS`、`candidatecheck` 输出 SHA-256 和完成时间。

每个消费者来源必须记录不同的 repository ID/owner/name、`B-06` 预期提交、workflow ID/完整 SHA/ref、run ID/attempt、actor ID、`workflow_dispatch`、`success`、Artifact ID/digest、原始证明文件 SHA-256 和 run 时间。证明自身的 `completed_at` 必须落在对应 run 内。

manifest 不包含自己的 object URI、version ID、checksum 或保留截止时间。上传之前这些值尚不存在，把它们放入 manifest 会形成自引用哈希循环；它们由上传后的签名回执固定。

## 离线内容校验

真实候选值存在后运行：

```powershell
go run ./internal/cmd/evidencecheck `
  -manifest D:\evidence\manifest.json `
  -tms-proof D:\evidence\tms.json `
  -parcel-proof D:\evidence\parcel.json `
  -request-id '<32 位小写十六进制 request ID>' `
  -candidate v0.1.0-rc.1 `
  -framework-commit '<框架完整提交>' `
  -checksum 'h1:<真实 module checksum>' `
  -contract-suite r05-v1 `
  -tms-commit '<TMS 完整预期提交>' `
  -parcel-commit '<Parcel 完整预期提交>'
```

`evidencecheck` 会重新读取两份严格证明并对原始字节计算 SHA-256，调用 `proofcheck` 同源校验逻辑，并验证 manifest 的候选、来源、API 比较、不可变镜像和时间窗。它不访问 GitHub、S3 或 KMS，也不验证 Artifact digest 是否由 GitHub 签发、对象是否真的锁定或 DSSE 是否有效。

## DSSE 证据回执

前三个对象上传并回读后生成 DSSE envelope：

- `payloadType` 固定为 `application/vnd.idp-bento.evidence-receipt.v1+json`。
- `payload` 是 base64 编码的 UTF-8 回执 JSON；签名输入使用标准 DSSE PAE，不自行设计字符串拼接。
- `signatures` 首版必须恰好一个，`keyid` 固定 KMS/HSM 非对称密钥版本，`sig` 为其 ECDSA P-256/SHA-256 签名。

回执 payload 必须包含 `format_version=1`、module path、候选版本、框架提交、request ID、签名时间，以及 `manifest`、`tms-proof`、`parcel-proof` 三个对象的 bucket、key、version ID、SHA-256、`COMPLIANCE` 和相同 `retain_until`。回执不得包含凭据、对象正文或可变 latest 指针。

签名密钥至少每 12 个月轮换；旧公钥、key ID 和验证策略保留至使用该 key 的最后一份证据到期。禁用或删除 signing key 不能使既有证据无法验证。

## DSSE 稳定发布授权

稳定 tag 前生成第二种 DSSE payload：

- `payloadType` 固定为 `application/vnd.idp-bento.release-authorization.v1+json`。
- payload 固定 `format_version=1`、stable version、tag name、目标框架提交、candidate version、module checksum、request ID、候选回执对象的 bucket/key/version/SHA-256 和授权时间。
- 签名与证据回执使用相同信任模型，可以使用轮换后的新 key，但必须能够验证对应 key ID。

该签名授权实现治理文档中的“签名 tag 绑定”：Git ref 本身由 GitHub Ruleset 防移动，DSSE 则证明哪个稳定版本被授权指向哪个提交及哪份候选证据。个人本地 tag、未签名 mapping 或只带 GitHub Release 页面均不能替代。

## 失败关闭与恢复

- Object Lock、Versioning、Compliance mode、SSE、checksum 或 retention 任一不符合：阻断稳定发布。
- OIDC claim、角色权限、session duration、workflow/ref 或 Environment 不符合：拒绝换取凭据并阻断发布。
- manifest/proof 摘要、S3 回读 checksum、version ID、DSSE 签名或 key trust 不一致：阻断发布并使用新 request ID 重跑。
- writer 获得删除、覆盖、缩短保留或跨前缀权限：视为发布安全缺陷，停止所有稳定发布。
- 存储或 KMS 不可用：允许保留 RC，不能先创建稳定 tag 后补证据。
- tag 推送失败：按已签名授权重试同一 stable version/commit；不得改指向。远端 tag 已存在且不同则按安全事件处理。
- 灾难恢复必须从精确 object version 和保留的公钥验证，不使用可变 latest、缓存副本或未签名导出替代。

## 剩余外部绑定

以下值必须来自真实基础设施，当前仍为 `UNBOUND`：

- S3 endpoint/provider、account/project、region、bucket、对象前缀和真实 Object Lock 能力。
- writer/verifier 角色标识、OIDC provider/audience 与实际 trust policy；GitHub repository ID `1315735832` 和 Organization owner ID `311024230` 已核验，尚未写入或演练 trust policy。
- 加密 key、ECDSA signing key ID、公钥指纹、轮换/禁用/恢复负责人。
- bucket policy、三年默认保留、审计日志、Legal Hold 和灾难恢复配置。
- 第三方 Action/CLI 的不可变 SHA、S3/KMS 客户端版本和一次创建-回读-拒绝删除演练。

这些值及真实演练完成前，`B-05` 保持“基础 CI 已部署、发布部署未验证”，不得创建稳定发布。
