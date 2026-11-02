# `go.idp.xyz` vanity 服务

本目录实现 `docs/design/module-resolution-contract.md` 已确认的 HTTPS vanity metadata 合同。服务只公开 module prefix、VCS 类型和 Git 仓库地址，不保存或代理 GitHub 凭据。

## 架构

```text
公共 DNS go.idp.xyz
  -> 宿主 Nginx :80/:443（HTTP-01、TLS、HTTPS 重定向）
  -> 127.0.0.1:8188
  -> idp-bento-go-vanity 容器 :8080
```

容器使用固定 digest 的官方 Nginx 镜像，以 UID/GID `101:101` 运行，根文件系统只读，删除全部 Linux capabilities，只绑定宿主回环地址。页面对 module 根路径和任意包子路径返回同一个唯一 `go-import` 元数据。

当前服务两个 module：

| module | 仓库 | 页面 |
| --- | --- | --- |
| `go.idp.xyz/idp-bento-go` | `https://github.com/idp-xyz/idp-bento-go.git` | `site/index.html`（未匹配任何 module 前缀的路径也落到这一页，保持既有行为） |
| `go.idp.xyz/idp-platform-go` | `https://github.com/idp-xyz/idp-platform-go.git` | `site/idp-platform-go.html` |

新增 module：在 `site/` 加一页（只含该 module 的一条 `go-import`），在 `nginx.conf` 加一条 `location ~ ^/<module>(/|$)`，在 `Dockerfile` 复制该页，在 `verify.sh` 与 `compose.yaml` 健康检查补上根路径与一个真实包子路径，镜像版本号加一。

## 文件

- `Dockerfile`、`nginx.conf`、`site/index.html`：不可变 vanity 镜像。
- `compose.yaml`：回环端口、健康检查、资源限制和日志轮换。
- `host/go.idp.xyz.http.conf`：首次签发证书时使用的 HTTP-only 配置。
- `host/go.idp.xyz.conf`：最终 HTTPS 反向代理配置。
- `host/install.sh`：备份现有同名站点、签发/复用证书、验证并 reload Nginx；任一步失败会尝试恢复原配置。
- `host/harden.sh`：部署验收后把运行材料移交 root，撤销临时 SSH 公钥和 Docker 组权限，并锁定临时部署账号。
- `verify.sh`：验证 module 根路径、真实公共包子路径、唯一 metadata 和 TLS SAN。

## 部署

目标主机必须已经运行 Docker、宿主 Nginx、certbot 和 `/var/www/certbot` HTTP-01 机制，且 `127.0.0.1:8188` 未被占用。

```bash
cd infra/vanity
docker compose build --pull=false
docker compose up -d
docker compose ps
curl --fail 'http://127.0.0.1:8188/idp-bento-go?go-get=1'
sudo ./host/install.sh
./verify.sh
```

主机已有 certbot 账户时不需要提供邮箱。若这是主机上的首个 ACME 账户，显式提供平台维护邮箱：

```bash
sudo env CERTBOT_EMAIL=ops@idp.xyz ./host/install.sh
```

certbot 的 `certbot.timer` 和现有 `/etc/letsencrypt/renewal-hooks/deploy/reload-nginx.sh` 负责自动续期与 Nginx reload。不要把 GitHub token、SSH 私钥或其他源码凭据放入本目录、镜像、Compose 环境变量或 Nginx 配置。

## 生产权限收口

公网验收和 CI 通过后，从部署目录之外以 root 执行：

```bash
bash /home/idp-vanity/services/go.idp.xyz/host/harden.sh
```

脚本先验证当前容器和 metadata，再执行以下受限变更：

- 将部署目录移动到 root 拥有的 `/opt/idp-bento-go-vanity`；Compose 项目名固定为 `goidpxyz`，不会因此创建第二套容器。
- 将原 `authorized_keys` 备份到 `/var/backups/idp-bento-go-vanity/hardening-<UTC 时间>/`，只删除本次任务创建的三把临时公钥。
- 从 `idp-vanity` 移除 `docker` 组，锁定密码并把 shell 改为 `/usr/sbin/nologin`。
- 重新解析 Compose、显示现有容器并执行一次公网 metadata 检查。

收口后所有更新必须通过明确授权的 root/发布运维流程从 `/opt/idp-bento-go-vanity` 执行，不得重新授予常驻 Docker 组或复用临时密钥。

## 回滚与撤销

安装脚本在 `/var/backups/idp-bento-go-vanity/<UTC 时间>/` 保存它修改前的精确同名站点文件；失败时自动恢复。人工回滚必须只恢复该备份中的 `sites-available`/`sites-enabled`，随后执行 `nginx -t`，成功后才能 reload。

停止容器不会删除证书或 Nginx 配置：

```bash
docker compose down
```

删除证书、备份或主机配置属于独立破坏性操作，不由本目录自动执行。
