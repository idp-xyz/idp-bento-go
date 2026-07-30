# `idp-bento-go` 候选前置门禁运行手册

状态：仓内入口已实现；外部发布绑定未完成

日期：2026-07-30

## 目标与边界

`internal/cmd/candidatecheck` 是平台无关的候选前置门禁入口。它把已经确认的 Go 工具链、公共兼容性、架构依赖、静态分析和 PostgreSQL 真实测试收敛为一个失败即停止的命令，供未来受保护 CI 直接调用。

该命令不创建 tag、module、RC、provenance 或消费者合同证明，也不访问 TMS/Parcel。运行成功只表示当前 checkout 具备进入不可变 RC 创建流程的仓内条件；实际 Git 仓库和 vanity 解析合同虽已绑定，`B-01B` 剩余的线上部署验收、`B-04` 至 `B-06` 和双消费者合同仍分别阻断跨仓获取、发布权限、CI 证明与稳定版本。

## 必需输入

- 必须从 `idp-bento-go` module 根目录运行。
- 当前 Go 工具链必须精确为 `go1.26.5`，module identity 必须精确为 `go.idp.xyz/idp-bento-go`。
- `IDP_BENTO_POSTGRES_DSN` 必须通过环境变量提供，并指向可执行仓库集成矩阵的 PostgreSQL 16 实例。DSN 不得作为命令参数或写入日志。
- 必须且只能选择一种比较模式：首个 `v0.1` 初始候选使用 `-initial-v0-1`；存在任一不可变候选或稳定前序后必须使用 `-baseline <前序 api 目录>`。
- `-baseline` 必须来自独立、只读、不可变的前序制品目录，不能指向当前 checkout 的 `api/`。

`-initial-v0-1` 只允许首个 `v0.1.0-rc.1` 使用。后续 RC、稳定版本和补丁/minor 候选不得用它绕过历史比较；受保护 CI 在 `B-05` 绑定时必须根据目标版本强制这一规则。

## 执行命令

首个初始候选：

```powershell
$env:IDP_BENTO_POSTGRES_DSN = '<由 CI 安全注入>'
go run ./internal/cmd/candidatecheck -initial-v0-1
```

存在前序不可变基线的候选：

```powershell
$env:IDP_BENTO_POSTGRES_DSN = '<由 CI 安全注入>'
go run ./internal/cmd/candidatecheck -baseline D:\artifacts\idp-bento-go\<前序版本>\api
```

本地运行可以使用临时 PostgreSQL 16 实例；受保护 CI 必须使用当时最新稳定 16.x，并保存测试输出中的完整 PostgreSQL 版本、UTF8 编码和 READ COMMITTED 证据。

## 固定门禁顺序

命令按以下顺序执行，任一失败立即停止：

1. 校验并输出精确 Go 工具链与 module identity。
2. 使用 `go mod tidy -diff` 检查 `go.mod`/`go.sum`，不静默改写文件。
3. 检查排除 `backup/` 的全部 Go 文件格式；`gofmt` 产生任何文件名都失败。
4. 使用 `apisnapshot -require-all` 检查八个公共包 snapshot。
5. 检查 `envelope-v1.json` 与 `envelope-current.json` 行为清单。
6. 执行生产依赖图门禁。
7. 使用 `go run ./internal/cmd/apicheck` 逐一比较八个 snapshot 与当前包；命令通过固定版本的官方 `apidiff` 库执行比较，只对当前包加载固定规范目标 `linux/amd64`、`CGO_ENABLED=0`，任何输出都作为公共变化失败，宿主平台不得改变基线。
8. 非初始候选使用 `compatcheck` 比较独立旧基线与当前候选。
9. 执行 `go vet ./...`。
10. 执行 `go test -count=1 ./...`，禁用测试缓存；DSN 前置检查保证 PostgreSQL 矩阵不能因环境缺失而跳过。

首个初始候选共执行 18 个步骤；提供旧基线时增加第 19 个历史兼容性步骤。

## 失败规则

- 未显式选择初始/旧基线模式、同时选择两种模式或传入当前 `api/` 作为旧基线：前置失败。
- Go 版本、module identity、module 文件、格式、snapshot、manifest、依赖图或 apidiff 漂移：阻断 RC。
- PostgreSQL DSN 缺失、数据库不可达、版本不是 major 16 或任一 `I-04` 用例失败：阻断 RC。
- 对非初始候选无法取得不可变旧 `api/` 基线：阻断 RC，不允许改用 `-initial-v0-1`。
- 命令成功但 `B-01B` 的 vanity 解析尚未通过线上验收、`B-04` 未绑定或 `B-05` 未完全绑定：仍不得创建远端 RC。
- 任一 TMS/Parcel 结果失败、缺失、超时或 module checksum 不同：可以创建修复用下一 RC，但不得创建稳定 tag。

## CI 证据边界

`B-05` 已确认使用 GitHub Actions。真实 workflow 创建后，CI 至少保存候选版本、框架提交、精确 Go/PostgreSQL 版本、module checksum、完整 `candidatecheck` 输出、不可变旧基线身份、TMS/Parcel 预期与实际提交、合同套件版本、结果和完成时间。

框架 repository ID 与 Organization owner ID 已完成核验；精确 GitHub run/workflow、消费者 repository ID、App、证明存储 URI、签名/provenance 实现仍属于 `B-05` 的真实外部绑定，详见[GitHub Actions 候选与发布合同](./github-actions-release-contract.md)。批准人由受保护平台记录，不写入可随代码修改的 JSON 充当授权证明。

不可变 RC 创建并由两个消费者执行合同后，协调作业先按[双消费者候选合同证明格式](./consumer-proof-format.md)运行 `proofcheck`，再按[WORM 发布证据与签名合同](./worm-release-evidence-contract.md)运行 `evidencecheck`、写入三年 Compliance Lock 对象并验证 DSSE 回执。两个命令都必须提供 `B-06` 的 TMS/Parcel 预期提交；全部步骤位于稳定 tag 之前。

## 当前状态

候选入口、前置失败测试、18/19 步编排测试和停止传播测试已经实现。当前没有真实 `IDP_BENTO_POSTGRES_DSN`，因此本轮只验证了命令编排和“缺少 DSN 必须失败”；既有 PostgreSQL 16.12 仓库矩阵证据不等于新的受保护 CI 候选证明。
