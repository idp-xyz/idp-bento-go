# `go.idp.xyz` module 解析合同

状态：设计已确认；DNS、TLS 与 metadata 部署已验证；真实 RC 空缓存获取已验收并可复现；双消费者证明与受保护证据未完成

日期：2026-07-31

## 主题与范围

本文确认 `go.idp.xyz/idp-bento-go` 在 `v0.1` 的跨仓解析方式，固定首次 RC 前必须通过的基础设施验收，并记录 2026-07-31 的实际 DNS、TLS 与 metadata 部署证据。本文不创建 GitHub 凭据、CI 工作流、tag 或发布候选，也不改变正式 module path。

## 已确认决定

| 项目 | 结论 |
| --- | --- |
| 正式 module path | `go.idp.xyz/idp-bento-go` |
| Git 仓库 | `https://github.com/idp-xyz/idp-bento-go` |
| `v0.1` 解析方式 | `go.idp.xyz` 通过 HTTPS vanity metadata 直接指向 GitHub Git 仓库 |
| 私有 GOPROXY | `v0.1` 不引入；未来如需集中缓存、审计或离线可用性，可在不改变 module path 的前提下另行绑定 |
| 元数据可见性 | `go-import` 元数据可以公开，只暴露 module prefix、VCS 类型和已确认的仓库地址；源码访问继续由 GitHub 仓库权限控制 |
| 消费者边界 | TMS、Parcel 只通过正式 module path 和不可变 SemVer 获取框架，不复制源码、不使用浮动分支或生产 `replace` |

该选择不新增 ADR。ADR-0008 已经固定“模块身份与 Git transport 解耦”；vanity metadata 是该决定下可替换的发布基础设施实现，不满足难以逆转条件。

## HTTP 合同

解析服务必须通过有效的公开 HTTPS 证书响应：

- `GET https://go.idp.xyz/idp-bento-go?go-get=1`
- `GET https://go.idp.xyz/idp-bento-go/<任意包子路径>?go-get=1`

两类请求都必须返回 `200`，并在 HTML `<head>` 中包含且只包含一个适用于该 module prefix 的 `go-import` 元数据：

```html
<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">
</head>
<body></body>
</html>
```

- prefix 必须精确为 `go.idp.xyz/idp-bento-go`，不得使用 GitHub 路径作为 module identity。
- VCS 必须为 `git`，仓库根必须精确指向 `https://github.com/idp-xyz/idp-bento-go.git`。
- 带 `go-get=1` 的请求不得在返回元数据前重定向到 GitHub、登录页或通用站点首页。
- 不带 `go-get=1` 的浏览器请求可以重定向到 GitHub 仓库，但不能影响 Go 命令的解析响应。
- 不配置 `GOINSECURE`，不允许无效证书、明文 HTTP 或跳过 TLS 校验。

## 私有仓库认证边界

如果 GitHub 仓库需要认证，vanity 页面仍不承载 Git 凭据。CI 在 Git 访问层使用 `B-05` 后续绑定的短期最小权限凭据，并遵守：

- `GOPRIVATE=go.idp.xyz`，避免把私有 module 路径、版本和校验查询发送到公共 GOPROXY 或公共 checksum database。
- 隔离验收作业显式使用 `GOPROXY=direct`；`GOPRIVATE=go.idp.xyz` 已同时派生本 prefix 的 `GONOPROXY` 与 `GONOSUMDB`，消费者普通构建可保留组织级 GOPROXY，同时让 `GOPRIVATE` 对本 module 走直连。
- 不得用 `GOSUMDB=off` 或 `GOFLAGS=-mod=mod` 之类的全局开关代替 `GOPRIVATE`。全局关闭 checksum database 会一并放弃公共依赖和 `golang.org/toolchain` 的校验，属于供应链降级；空缓存验收作业必须保留默认 `GOSUMDB=sum.golang.org`。
- `GIT_TERMINAL_PROMPT=0`，缺少凭据时立即失败，不能等待交互输入。
- 凭据不得写入 clone URL、`go.env`、仓库文件、构建参数、日志、module cache 制品或最终镜像层。
- 不使用带明文 token 的全局 Git `insteadOf` 配置；凭据只能经 Git credential helper 在请求时提供。凭据载体、轮换、撤销和审计由 `B-05` 绑定。

仓库公开或私有的实际策略由 GitHub 仓库设置决定；本文不根据 URL 推断可见性，也不把“私有化交付”等同于“公开源码仓库”。

## 首次 RC 验收

`B-01B` 只有在以下证据全部存在后才能改为 `BOUND`：

1. DNS 将 `go.idp.xyz` 解析到公司控制的服务，TLS 证书有效且自动续期责任明确。
2. module 根路径和至少一个真实公共包子路径的 `?go-get=1` 响应满足上述精确元数据合同。
3. 在空 `GOMODCACHE`、空 `GOCACHE`、无本地 `replace`、`GOPROXY=direct` 和禁止交互提示的环境中，消费者身份能够下载真实不可变 `v0.1.0-rc.N`。
4. `go mod download -json go.idp.xyz/idp-bento-go@v0.1.0-rc.N` 返回精确 Path、Version、module checksum 和预期 Git origin；tag 必须来自绑定仓库且不得重写。
5. TMS、Parcel 的最小合同作业分别从正式 module path 编译，不从本地目录、GitHub module path 或源码副本取代候选。
6. 受保护 CI 保存解析响应摘要、DNS/TLS 检查、候选版本、完整框架提交、module checksum、Git origin 和作业时间；不得保存访问凭据或源码制品。

验收命令中的 RC 版本和凭据只能使用真实外部值。本文不提供伪造的成功输出。

截至 2026-07-31，第 1 至 4 项已经通过实际验收。

第 1、2 项公网验收：Google 公共 DNS 返回 `go.idp.xyz A 8.133.22.7`；Let's Encrypt ECDSA 证书的 SAN 为 `go.idp.xyz`，有效期至 2026-10-29；module 根路径、`domain` 真实公共包子路径和额外的 `postgres/outbox` 子路径均返回 `200`，且只包含一个精确 metadata。

第 3、4 项在首个不可变候选 `v0.1.0-rc.1` 上验收。空 `GOMODCACHE`、空 `GOCACHE`、无 `replace`、`GOPRIVATE=go.idp.xyz`、`GOPROXY=direct`、`GIT_TERMINAL_PROMPT=0` 环境下，`go mod download -json go.idp.xyz/idp-bento-go@v0.1.0-rc.1` 返回：

| 字段 | 值 |
| --- | --- |
| `Path` | `go.idp.xyz/idp-bento-go` |
| `Version` | `v0.1.0-rc.1` |
| `Sum` | `h1:lx/snQkwUQW5pWPOgNI4AsZrSxxi0YYqHfOemsgEuLQ=` |
| `GoModSum` | `h1:RFR6ylLNIIA7e4PPGVCzojYiH6DB8eHd2s5vI9XcUgY=` |
| `Origin.URL` | `https://github.com/idp-xyz/idp-bento-go.git` |
| `Origin.Ref` | `refs/tags/v0.1.0-rc.1` |
| `Origin.Hash` | `af68525c1d08fccae13eccd01f209465959673d6` |

同日在独立临时 `HOME`、全新空 `GOMODCACHE`/`GOCACHE`、临时空 main module（无 `require`、无 `replace`）中复跑该命令，返回完全相同的 `Sum`、`GoModSum`、`Origin.URL`、`Origin.Ref` 和 `Origin.Hash`，证明 tag 未被移动且同一版本产生同一 checksum。该次复跑保留默认 `GOSUMDB=sum.golang.org`，仅用 `GOPRIVATE=go.idp.xyz` 让本 module 跳过公共 proxy 与 checksum database；`GOTOOLCHAIN=go1.26.5` 在同一空缓存中下载并通过 checksum database 校验 `golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64`。Git 读取身份只经 credential helper 在请求时提供，验收环境不含带明文 token 的 `insteadOf` 配置。`-x` 输出确认解析路径为 `https://go.idp.xyz/idp-bento-go?go-get=1` 返回 `200` 后直连 `https://github.com/idp-xyz/idp-bento-go.git` 并只 fetch `refs/tags/v0.1.0-rc.1`。

一次早期尝试因作业环境设置 `GOSUMDB=off` 而失败：工具链模块 `golang.org/toolchain@v0.0.1-go1.26.5.linux-amd64` 报 `verifying module: checksum database disabled by GOSUMDB=off`。该失败属于验收环境配置错误，不是解析或凭据缺陷，处置方式是改用 `GOPRIVATE` 而非全局关闭校验。

第 5 项已由 TMS 单侧满足：`idp-tms` 在提交 `f8eb9c3` 锁定该精确候选，`tests/bentocontract` 从正式 module path 编译并比对 checksum 通过。Parcel 侧合同作业尚未建立，因此第 5 项整体未完成，第 6 项也未开始，`B-01B` 尚未整体升级为 `BOUND`。

## 失败关闭规则

- DNS、TLS、元数据、Git 认证或 tag 任一失败：阻断 RC，不允许临时改用 GitHub import path。
- 元数据指向非预期仓库或返回多个冲突 prefix：视为供应链风险，阻断所有候选获取并回滚解析部署。
- Git tag 被移动、删除或相同版本产生不同 checksum：阻断稳定发布并按发布安全事件处理。
- vanity 服务不可用：冷缓存构建和新候选获取失败；已有缓存不能作为发布门禁通过证据。
- GitHub 组织或仓库迁移：先更新 vanity 目标并完成空缓存验收，module path 保持不变。
- 未来引入私有 GOPROXY：必须证明候选版本、checksum、不可变性和凭据边界不弱于本合同，且不能改变消费者 import path。

## 所有权

- 框架维护组拥有 module prefix、预期 Git 仓库、不可变 tag 和本验收合同。
- 公司域名/平台维护方拥有 DNS、TLS、vanity 响应部署、可用性和变更保护。
- `B-05` 的 CI/安全维护方拥有 GitHub 读取身份、短期凭据和审计证据。
- TMS、Parcel 只拥有各自消费凭据和合同作业，不拥有 vanity 路由或框架 tag。

## 当前状态

解析方式、精确元数据和验收标准已经确认。2026-07-30 创建公司 Organization `idp-xyz` 并将仓库迁移到私有仓库 `https://github.com/idp-xyz/idp-bento-go`；repository ID 保持 `1315735832`，owner ID 变为 `311024230`，旧 URL 自动重定向。

2026-07-31 创建首个不可变候选 tag `v0.1.0-rc.1`，指向提交 `af68525c1d08fccae13eccd01f209465959673d6`。该 tag 不得移动或删除；同一版本产生不同 checksum 按发布安全事件处理。

2026-07-31 已在 `8.133.22.7` 完成实际部署：宿主 Nginx 独占 80/443 和 TLS，反向代理到只绑定 `127.0.0.1:8188` 的 `idp-bento-go-vanity` 容器；容器以 `101:101` 非 root 身份运行，根文件系统只读、删除全部 capabilities 且不含 GitHub 凭据。运行镜像 ID 为 `sha256:1cd9d483d3a89dca40fc492211d94fadbc7f22cc49c8a6d467120d34ccb29a27`，基础镜像固定为官方 `nginx@sha256:7068961d45b07b2af510ac002e9daa63a1d3eba2111202d6768798690800fffd`。部署材料位于 `infra/vanity/`。

Google 公共 DNS-over-HTTPS 返回 `go.idp.xyz A 8.133.22.7`；HTTP 请求 `301` 到同路径 HTTPS；Let's Encrypt ECDSA 证书 SAN 为 `go.idp.xyz`，有效期为 2026-07-31 至 2026-10-29。certbot `4.0.0` 使用现有账户和 `/var/www/certbot` webroot，`certbot.timer` 与现有 deploy hook 负责续期和 Nginx reload。module 根路径、`domain` 和 `postgres/outbox` 子路径已验证为 `200` 且唯一 metadata 精确匹配本合同。

当前剩余阻断不再是 DNS、TLS、metadata 服务、不可变 RC、私有 Git 读取身份或空缓存下载——第 1 至 4 项已经通过实际验收并可复现——而是 Parcel 侧消费证明和受保护证据作业。不得把当前可移动的 `main` 提交当作候选基线。
