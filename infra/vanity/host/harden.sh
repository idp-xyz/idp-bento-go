#!/usr/bin/env bash
set -Eeuo pipefail

account="idp-vanity"
source_dir="/home/${account}/services/go.idp.xyz"
target_dir="/opt/idp-bento-go-vanity"
authorized_keys="/home/${account}/.ssh/authorized_keys"
backup_dir="/var/backups/idp-bento-go-vanity/hardening-$(date -u +%Y%m%dT%H%M%SZ)"
temporary_key_pattern='(codex-idp-bento-go-vanity-20260731|idp-bento-go-vanity-20260731|idp-bento-go-vanity-wsl-20260731)$'
expected_meta='<meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">'

if [[ ${EUID} -ne 0 ]]; then
    printf '必须以 root 执行。\n' >&2
    exit 1
fi

for command_name in chmod chown cp curl docker getent gpasswd grep id install mktemp mv tr usermod wc; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        printf '缺少命令：%s\n' "${command_name}" >&2
        exit 1
    }
done

getent passwd "${account}" >/dev/null
if [[ ! -d ${source_dir} ]]; then
    printf '部署源目录不存在：%s\n' "${source_dir}" >&2
    exit 1
fi
if [[ -e ${target_dir} || -L ${target_dir} ]]; then
    printf '目标目录已存在，拒绝覆盖：%s\n' "${target_dir}" >&2
    exit 1
fi
case "${PWD}/" in
    "${source_dir}/"*)
        printf '请从部署目录之外执行本脚本。\n' >&2
        exit 1
        ;;
esac

docker compose -f "${source_dir}/compose.yaml" config >/dev/null
body="$(curl --fail --silent --show-error --max-time 5 \
    'http://127.0.0.1:8188/idp-bento-go?go-get=1')"
count="$(grep -oF "${expected_meta}" <<<"${body}" | wc -l || true)"
if [[ ${count} -ne 1 ]]; then
    printf '当前 vanity 服务未通过唯一 metadata 检查，拒绝收口。\n' >&2
    exit 1
fi

install -d -m 0700 "${backup_dir}"
if [[ -f ${authorized_keys} ]]; then
    cp -a -- "${authorized_keys}" "${backup_dir}/authorized_keys"
fi

mv -- "${source_dir}" "${target_dir}"
chown -R root:root "${target_dir}"
chmod -R u=rwX,go=rX "${target_dir}"
chmod 0750 "${target_dir}"

if [[ -f ${authorized_keys} ]]; then
    filtered_keys="$(mktemp)"
    trap 'rm -f -- "${filtered_keys}"' EXIT
    grep -vE "${temporary_key_pattern}" "${authorized_keys}" >"${filtered_keys}" || true
    install -o "${account}" -g "${account}" -m 0600 "${filtered_keys}" "${authorized_keys}"
    if grep -Eq "${temporary_key_pattern}" "${authorized_keys}"; then
        printf '临时公钥仍然存在，停止收口。\n' >&2
        exit 1
    fi
fi

if id -nG "${account}" | tr ' ' '\n' | grep -Fxq docker; then
    gpasswd -d "${account}" docker
fi
usermod --lock --shell /usr/sbin/nologin "${account}"

docker compose -f "${target_dir}/compose.yaml" config >/dev/null
docker compose -f "${target_dir}/compose.yaml" ps
curl --fail --silent --show-error --max-time 10 \
    'https://go.idp.xyz/idp-bento-go?go-get=1' \
    | grep -Fq "${expected_meta}"

printf '生产权限收口完成。部署目录：%s\n' "${target_dir}"
printf '授权文件备份：%s\n' "${backup_dir}"
getent passwd "${account}"
id "${account}"
