#!/usr/bin/env bash
set -euo pipefail

expected_meta='<meta name="go-import" content="go.idp.xyz/idp-bento-go git https://github.com/idp-xyz/idp-bento-go.git">'
paths=(
    idp-bento-go
    idp-bento-go/domain
)

for path in "${paths[@]}"; do
    url="https://go.idp.xyz/${path}?go-get=1"
    body="$(curl --fail --silent --show-error --max-time 10 "${url}")"
    count="$(grep -oF "${expected_meta}" <<<"${body}" | wc -l || true)"
    if [[ ${count} -ne 1 ]]; then
        printf '元数据数量错误：%s（实际 %s）\n' "${url}" "${count}" >&2
        exit 1
    fi
    printf 'PASS %s\n' "${url}"
done

printf '\nTLS 证书：\n'
openssl s_client -connect go.idp.xyz:443 -servername go.idp.xyz </dev/null 2>/dev/null \
    | openssl x509 -noout -subject -issuer -dates -ext subjectAltName
