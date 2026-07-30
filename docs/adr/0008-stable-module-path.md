# ADR-0008：使用稳定 module path 解耦 Git transport

状态：已接受

日期：2026-07-29

## 背景

Go module path 会进入 `go.mod`、源码导入、checksum 和发布版本。若直接使用当前 GitLab/Gitea 主机地址，未来托管迁移会迫使所有消费者修改导入路径并形成新的模块身份。

## 决定

正式 module path 固定为 `go.idp.xyz/idp-bento-go`。实际私有 Git 地址以及 `go.idp.xyz` 的 vanity metadata 或私有 GOPROXY 路由作为发布基础设施单独绑定，不改变模块身份。

## 后果

- 代码托管平台可以迁移而不改变消费者导入路径。
- 公司必须长期控制 `idp.xyz` 并维护 HTTPS 解析或私有代理可用性。
- 基础设施未绑定时可以本地构建，但不能跨仓获取候选或发布 RC。

## 考虑过的替代方案

- 使用实际 Git 主机路径：部署简单，但把长期公共身份绑定到可替换平台。
- 使用临时占位路径：代码一旦被消费，后续替换会成为破坏性模块迁移。
- 使用产品仓库子路径：重新引入宿主关系并耦合产品所有权。

## 链接

- [框架基线](../design/framework-baseline.md)
- [`go.idp.xyz` module 解析合同](../design/module-resolution-contract.md)
- [`v0.1` 最终实施决策简报](../design/v0.1-implementation-decision-brief.md)
