#!/usr/bin/env bash
set -Eeuo pipefail

site_name="go.idp.xyz"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
available="/etc/nginx/sites-available/${site_name}"
enabled="/etc/nginx/sites-enabled/${site_name}"
http_source="${script_dir}/${site_name}.http.conf"
https_source="${script_dir}/${site_name}.conf"
backup_dir="/var/backups/idp-bento-go-vanity/$(date -u +%Y%m%dT%H%M%SZ)"
expected_meta='<meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">'
had_available=false
had_enabled=false

if [[ ${EUID} -ne 0 ]]; then
    printf '必须以 root 执行。\n' >&2
    exit 1
fi

for command_name in certbot curl grep install ln nginx systemctl; do
    command -v "${command_name}" >/dev/null 2>&1 || {
        printf '缺少命令：%s\n' "${command_name}" >&2
        exit 1
    }
done

test -f "${http_source}"
test -f "${https_source}"
systemctl is-active --quiet nginx

body="$(curl --fail --silent --show-error --max-time 5 \
    'http://127.0.0.1:8188/idp-bento-go?go-get=1')"
meta_count="$(grep -oF "${expected_meta}" <<<"${body}" | wc -l || true)"
if [[ ${meta_count} -ne 1 ]]; then
    printf '回环 vanity 响应不满足唯一 go-import 元数据合同。\n' >&2
    exit 1
fi

install -d -m 0750 "${backup_dir}"
if [[ -e ${available} || -L ${available} ]]; then
    cp -a -- "${available}" "${backup_dir}/sites-available"
    had_available=true
fi
if [[ -e ${enabled} || -L ${enabled} ]]; then
    cp -a -- "${enabled}" "${backup_dir}/sites-enabled"
    had_enabled=true
fi

rollback() {
    local status=$?
    trap - ERR
    set +e
    rm -f -- "${available}" "${enabled}"
    if [[ ${had_available} == true ]]; then
        cp -a -- "${backup_dir}/sites-available" "${available}"
    fi
    if [[ ${had_enabled} == true ]]; then
        cp -a -- "${backup_dir}/sites-enabled" "${enabled}"
    fi
    if nginx -t; then
        systemctl reload nginx
    fi
    printf '安装失败，已尝试恢复原站点配置。备份：%s\n' "${backup_dir}" >&2
    exit "${status}"
}
trap rollback ERR

install -d -m 0755 /var/www/certbot
install -m 0644 "${http_source}" "${available}"
ln -sfn "${available}" "${enabled}"
nginx -t
systemctl reload nginx

certbot_args=(
    certonly
    --webroot
    --webroot-path /var/www/certbot
    --domain "${site_name}"
    --non-interactive
    --agree-tos
    --keep-until-expiring
    --key-type ecdsa
)
if [[ -n ${CERTBOT_EMAIL:-} ]]; then
    certbot_args+=(--email "${CERTBOT_EMAIL}")
else
    certbot_args+=(--no-eff-email)
fi
certbot "${certbot_args[@]}"

test -s "/etc/letsencrypt/live/${site_name}/fullchain.pem"
test -s "/etc/letsencrypt/live/${site_name}/privkey.pem"
install -m 0644 "${https_source}" "${available}"
nginx -t
systemctl reload nginx

trap - ERR
printf 'go.idp.xyz Nginx/TLS 安装完成。配置备份：%s\n' "${backup_dir}"
