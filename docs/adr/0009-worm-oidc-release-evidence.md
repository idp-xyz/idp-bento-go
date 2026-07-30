# ADR-0009：使用合规模式 WORM 与 OIDC 签名发布证据

状态：已接受

日期：2026-07-30

## 背景

GitHub Actions Artifact、workflow log、Release asset 和普通 Git 分支都可以删除或到期，不能证明发布证据在稳定版本生命周期后仍未被替换。Object Lock 的 Governance 模式可由特权身份绕过，长期 S3/KMS 密钥进入 CI 又会扩大凭据泄漏和离职风险。

## 决定

稳定发布证据写入启用 Versioning 与 Object Lock 的 S3-compatible 存储，使用 `COMPLIANCE` 模式锁定 3 个日历年并通过客户管理密钥加密。GitHub Actions 仅从受保护 `release-evidence` Environment 使用 OIDC 换取最长 15 分钟的最小权限身份；证据对象写入并回读后，以 KMS/HSM 托管的 ECDSA P-256/SHA-256 key 签署 DSSE 证据回执和稳定版本授权。Git tag 由 Ruleset 防移动，稳定版本到提交及候选证据的绑定由 DSSE 授权签名。

## 后果

- 发布证据在三年内即使由管理员也不能缩短保留或删除，能够对抗普通账号失陷和事后改写。
- CI 不保存长期 S3 access key 或签名私钥，writer 与 verifier 权限分离。
- 误写和失败证据同样占用三年存储，只能用新 request ID 纠正，不能清理历史。
- 存储、OIDC、加密 key、签名 key、旧公钥和灾难恢复成为稳定发布的持续基础设施责任。
- Git tag 签名语义由可验证的 DSSE 版本映射实现，而不是依赖维护者个人 GPG key。

## 考虑过的替代方案

- 只保存 GitHub Artifact/attestation：内容可验证但 Artifact 可删除或到期，不能单独满足保留锁。
- Object Lock Governance：便于管理员清理误写，但同一绕过能力也能删除或缩短真实发布证据。
- 普通审计 Git 仓库：签名 commit 可提供篡改迹象，但管理员仍可删除仓库、重写 ref 或移除历史对象。
- CI 中保存长期 S3/KMS 凭据：实现简单，但扩大跨 run 复用、泄漏和轮换风险。
- 维护者 GPG 签名 tag：把组织发布身份绑定到个人密钥，无法同时固定消费者证明和 WORM object version。

## 链接

- [GitHub Actions 候选与发布合同](../design/github-actions-release-contract.md)
- [WORM 发布证据与签名合同](../design/worm-release-evidence-contract.md)
- [双消费者候选合同证明格式](../design/consumer-proof-format.md)
- [维护与发布治理](../design/maintenance-governance.md)
