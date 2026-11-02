#!/usr/bin/env bash
set -euo pipefail

# 每行：请求路径 和 该路径必须返回的唯一 go-import 元数据。
# 每个 module 至少验根路径与一个真实公共包子路径。
checks=(
    'idp-bento-go|<meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">'
    'idp-bento-go/domain|<meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">'
    'idp-platform-go|<meta name="go-import" content="go.idp.xyz/idp-platform-go git https://github.com/idp-xyz/idp-platform-go.git">'
    'idp-platform-go/numbering/postgres|<meta name="go-import" content="go.idp.xyz/idp-platform-go git https://github.com/idp-xyz/idp-platform-go.git">'
)

for check in "${checks[@]}"; do
    path="${check%%|*}"
    expected_meta="${check#*|}"
    url="https://go.idp.xyz/${path}?go-get=1"
    body="$(curl --fail --silent --show-error --max-time 10 "${url}")"
    count="$(grep -oF "${expected_meta}" <<<"${body}" | wc -l || true)"
    total="$(grep -oF 'name="go-import"' <<<"${body}" | wc -l || true)"
    if [[ ${count} -ne 1 || ${total} -ne 1 ]]; then
        printf '元数据错误：%s（匹配 %s，go-import 共 %s）\n' "${url}" "${count}" "${total}" >&2
        exit 1
    fi
    printf 'PASS %s\n' "${url}"
done

printf '\nTLS 证书：\n'
openssl s_client -connect go.idp.xyz:443 -servername go.idp.xyz </dev/null 2>/dev/null \
    | openssl x509 -noout -subject -issuer -dates -ext subjectAltName
