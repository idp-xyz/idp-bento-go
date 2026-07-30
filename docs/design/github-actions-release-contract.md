# GitHub Actions 候选与发布合同

状态：配置已确认；框架仓库与组织所有权已绑定；发布部署未验证

日期：2026-07-30

## 主题与范围

本文确认 `B-05` 的 CI 平台、跨仓候选协调方式、最小权限边界、消费者证明来源校验和持久化要求。本文不创建 GitHub workflow、App、Environment、Ruleset、secret、外部证据存储、tag 或 RC，也不访问 TMS/Parcel 仓库。

## 已确认决定

| 项目 | 结论 |
| --- | --- |
| CI 平台 | GitHub Actions，框架仓库为 `idp-xyz/idp-bento-go`，repository ID `1315735832`，owner ID `311024230` |
| PR CI | 只运行框架仓内门禁，不访问消费者、不获得发布或跨仓凭据 |
| 候选入口 | 受保护的 `release-candidate` Environment 运行 `candidatecheck`，通过后才允许创建不可变 RC tag |
| 跨仓触发 | 框架协调作业通过 GitHub API `workflow_dispatch` 调用 TMS、Parcel 各自拥有的专用 `bento-contract` workflow |
| 消费者源码 | 始终在消费者自己的 workflow 中 checkout 和测试；框架作业不 checkout、缓存或归档消费者源码 |
| 稳定发布 | 受保护的 `stable-release` Environment 在双消费者证明、不可变证据写入和批准完成后创建稳定 tag |
| 证明传输 | GitHub Actions Artifact 只用于把严格 JSON v1 从受信任消费者 run 传给协调作业 |
| 权威证据 | 使用 S3-compatible Object Lock `COMPLIANCE` 锁定 3 年，并通过 GitHub OIDC + KMS ECDSA P-256 签署 DSSE 回执；GitHub Artifact 可删除或到期，只作传输 |

GitHub Actions 是可替换的发布基础设施，不改变 Go module identity、公共 API 或消费者所有权，因此不新增 ADR。若迁移 CI 平台，新平台必须满足本文的来源、权限、证据和失败关闭合同。

## 工作流分层

### 框架 PR CI

- 事件使用 `pull_request` 和受保护默认分支的 `push`，禁止用 `pull_request_target` 执行 PR checkout 的代码。
- 默认 `GITHUB_TOKEN` 权限为 `contents: read`；不授予 `contents: write`、`actions: write`、`id-token: write` 或跨仓 App 凭据。
- 执行格式、module 整理、snapshot/manifest、依赖图、apidiff、`go vet`、单元测试和 PostgreSQL 16 集成矩阵。
- 第三方 Action 固定完整 commit SHA；runner 镜像和 PostgreSQL 容器固定可审计版本，候选阶段不得使用浮动 `@main`、`@vN` 或未固定容器 digest。

### 框架候选

1. 只从受保护默认分支的精确框架提交启动，输入候选版本、前序 API 基线身份、TMS 预期提交、Parcel 预期提交和合同套件版本。
2. 在 `release-candidate` Environment 中取得 PostgreSQL DSN 和必要的短期身份，执行 `candidatecheck`；首个候选只允许 `v0.1.0-rc.1` 使用 `-initial-v0-1`。
3. 验证 `B-01B` 的 DNS/TLS/vanity 合同后，由仅该 job 持有的 `contents: write` 身份创建指向输入框架提交的不可变 RC tag。
4. 在空 module/cache 环境中通过 `go.idp.xyz/idp-bento-go` 下载 RC，记录真实 module checksum 和 Git origin；不得从 checkout 目录计算替代 checksum。
5. 生成 128-bit 随机 `request_id`，并行触发两个消费者合同作业；单个消费者初始超时为 30 分钟，协调总超时为 40 分钟。
6. 只从预期仓库、预期 workflow 和与 `request_id` 对应的成功 run 下载一个证明 Artifact，完成来源校验和 `proofcheck`。
7. 将最小证据写入 WORM 存储并取得对象版本、内容摘要、保留策略和签名结果；失败时不得进入稳定发布。

RC 可以在消费者失败后保留用于诊断和下一候选修复，但不得移动、覆盖或重用相同版本号。

### 消费者合同

TMS、Parcel 各自拥有一个受保护的 `bento-contract` workflow。实际仓库坐标和 workflow ID 在外部绑定时登记，框架不规定消费者其他 CI。

`workflow_dispatch` 必需输入：

| 输入 | 规则 |
| --- | --- |
| `request_id` | 32 位小写十六进制随机值；只用于 run/artifact 关联，不进入证明 JSON |
| `framework_commit` | RC tag 指向的完整 Git object ID |
| `candidate_version` | 合法的预发布 SemVer，与不可变 RC 完全一致 |
| `module_path` | 固定为 `go.idp.xyz/idp-bento-go` |
| `module_checksum` | 协调作业空缓存下载得到的真实 `h1:` checksum |
| `contract_suite_version` | 两个消费者共同执行的合同矩阵版本 |
| `consumer_commit` | `B-06` 批准的该消费者完整 Git object ID |

消费者 workflow 必须从受保护 workflow 定义启动，随后 detached checkout 精确 `consumer_commit`，并在运行合同前比较 `git rev-parse HEAD`。证明中的 `consumer_commit` 只能来自该命令结果，不能来自分支名、dispatch payload 回显或 workflow 自身的 `GITHUB_SHA`。

每个 run 只上传一个名称包含消费者和 `request_id` 的严格 JSON v1 Artifact。Artifact 不得包含源码、测试日志、数据库、业务夹具、Payload、SQL、租户数据或凭据。失败 run 不生成 `PASS` 证明；超时、取消和跳过统一失败关闭。

## 身份与权限

使用两个职责分离的 GitHub App，避免一个跨仓令牌同时拥有消费者工作流控制和框架源码写权限：

| 身份 | 安装范围 | 最大权限 | 用途 |
| --- | --- | --- | --- |
| 合同协调 App | 仅 TMS、Parcel 仓库 | Metadata read；Actions read/write | dispatch 专用合同 workflow、轮询预期 run、读取最小证明 Artifact |
| module 读取 App | 仅 `idp-xyz/idp-bento-go` | Metadata read；Contents read | 消费者从 GitHub 仓库通过正式 vanity path 下载候选 module |
| 框架 `GITHUB_TOKEN` | 当前框架 run | 普通 job 为 Contents read；创建 tag 的隔离 job 才临时 Contents write | 仓内 checkout 与受保护 tag 创建 |
| 证据写入身份 | 仅 `release-evidence` Environment 的证据 job | `id-token: write`，由外部存储信任策略换取最长 15 分钟的写入/签名身份 | 写入 WORM 证据并签名，不访问消费者源码 |

- GitHub App installation token 必须按 job 生成、限制仓库集合和权限并在作业结束后失效；不得跨 run 复用。
- App private key 若由 GitHub Secret 托管，必须限制到受保护 Environment，记录轮换与撤销；它是受保护密钥，不得被描述为短期 token。
- fork PR、普通 PR job 和消费者业务 job不得读取发布 Environment secret。
- 两个 App 的名称、App ID、installation ID、secret 名称和轮换负责人仍为真实外部值，未绑定前不能创建 RC。

## 证明来源校验

严格 JSON 只能证明内容，不能自证来源。协调作业下载证明前必须通过 GitHub API 固定并保存：

- 消费者仓库的不可变 repository ID 和 owner/name。
- 专用 workflow ID、workflow 文件的提交 SHA、run ID、run attempt、event、actor、开始/结束时间和 `success` conclusion。
- `request_id` 对应的唯一 run-name 和唯一 Artifact；重复、冲突或无法唯一关联时失败。
- Artifact ID、名称、GitHub 返回的内容 digest，以及下载后证明文件的 SHA-256。
- 证明中的消费者、候选版本、module checksum、合同套件和消费者提交与协调输入完全一致。

`proofcheck` 必须显式接收 `-tms-commit` 和 `-parcel-commit`，把两份证明提交与 `B-06` 批准基线比较。仅验证 Git object ID 格式不构成基线验证。

协调作业不下载消费者日志或其他 Artifact。需要诊断时由消费者在自己的权限边界内查看，框架权威证据只保存发布判断所需的最小元数据。

## WORM 证据合同

GitHub Actions Artifact 在单次上传后内容固定，但可以被删除并受保留期限制，因此仅是传输载体。稳定 tag 前必须先由 `evidencecheck` 校验 manifest 与原始证明，再按[WORM 发布证据与签名合同](./worm-release-evidence-contract.md)写入 Object Lock Compliance 存储并签署 DSSE 回执。

- 候选版本、框架提交、module path、checksum、Git origin 和前序 API 基线身份。
- 精确 Go、runner、PostgreSQL 版本以及 `candidatecheck` run ID、结论和输出摘要。
- 两个消费者的仓库 ID、预期/实际提交、workflow ID/SHA、run ID/attempt、Artifact digest、证明文件摘要和严格 JSON v1。
- 合同套件版本、请求 ID、超时判断、GitHub Environment 审批记录引用。
- 证据对象 URI、不可变版本 ID、内容 SHA-256、保留截止时间和签名/attestation 身份。

对象不得包含消费者源码、业务类型、测试数据、数据库快照、完整日志或凭据。对象创建后不得覆盖；重试产生新对象版本并明确记录哪个版本被稳定发布采用。

Object Lock 模式、三年保留、对象前缀、OIDC subject、ECDSA P-256 与 DSSE 格式已经确认。实际 provider/endpoint/account/region/bucket、角色、audience、加密/signing key、Action SHA 和演练证据仍为 `UNBOUND`。GitHub Release asset、Actions Artifact、workflow log、普通 Git 分支或可强推审计仓库都不能单独满足本合同。

## 稳定发布

稳定发布 job 必须：

1. 在 `stable-release` Environment 中验证 `B-04` 的真实批准和 `B-06` 的发布日期、支持矩阵及两个消费者基线提交。
2. 重新读取 WORM 对象并验证版本、摘要、签名、保留策略和双消费者 `PASS`，不得信任前一 job 的布尔输出。
3. 确认稳定版本指向与 RC 完全相同的框架提交，RC tag 未移动，且没有版本号复用。
4. 先写入并验证 DSSE 稳定发布授权，再使用隔离的短期 CI 身份创建 Ruleset 保护且不可移动的稳定 tag；远端 ref 必须等于授权中的版本与目标提交。
5. 发布失败时保留证据并创建新版本修复，不覆盖 tag、证明或证据对象。

## 失败关闭规则

- App、workflow、repository ID、run、Artifact 或 WORM 来源不匹配：阻断稳定发布。
- 任一消费者运行失败、超时、取消、跳过、缺失或针对非预期提交：阻断稳定发布。
- rerun 产生不同证明：视为新的 run attempt，只能显式选择一个并保存全部冲突事实；不能静默覆盖。
- Action 未固定完整 SHA、runner/container 身份漂移或 workflow 来自未保护提交：阻断候选。
- WORM 不可用、保留锁未生效、摘要/签名无法验证或对象可覆盖：阻断稳定 tag。
- CI 平台故障只能使用治理合同允许的等价受信任通道，不能把本地 JSON 当作发布授权。

## 已核验外部事实

2026-07-30 通过 GitHub 公共 API 与 Git transport 完成只读核验：

- 框架仓库 owner/name 为 `idp-xyz/idp-bento-go`，repository ID 为 `1315735832`，Organization owner ID 为 `311024230`，公开可见，默认分支为 `main`。
- 迁移前个人 owner ID 为 `310467973`；GitHub 不支持把个人账号转换为 Organization，因此创建 `idp-xyz` 后转移仓库。旧 URL 已验证自动重定向到新地址。
- 远端 `HEAD` 与 `refs/heads/main` 当时均指向 `27543027c997a9eb77246c1ef62c85f3f22acc1e`；该观测值可变，不是 `B-06` 基线或发布授权。
- 迁移后 Actions workflow、Ruleset、Environment 和 tag 的 API 结果均为空，默认分支设置页明确显示尚未配置保护规则，证明发布部署尚未开始。

## 剩余外部绑定

`B-05` 只有在以下真实值和运行证据全部存在后才能标记 `BOUND`：

- TMS、Parcel 的 GitHub repository ID、owner/name、默认分支、专用 workflow ID 和保护规则。
- 两个 GitHub App 的名称、ID、installation、精确权限、secret/轮换负责人及一次短期 token 验证。
- 所有第三方 Action 的完整 commit SHA、runner 镜像和 PostgreSQL 容器 digest。
- WORM provider/endpoint/account/region/bucket、角色、OIDC audience、加密/signing key、恢复/审计负责人和一次三年 Compliance Lock 演练。
- `release-candidate`、`stable-release` Environment、Ruleset、审批人与稳定 tag 保护的实际配置。
- 一次不含真实发布的失败路径演练，以及首次 RC 的完整受保护运行证据。

框架仓库与公司 Organization 所有权已经绑定，但发布控制面尚未部署；不创建占位 workflow，也不伪造其余外部标识。
