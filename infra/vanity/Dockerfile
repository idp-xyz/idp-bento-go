FROM nginx@sha256:7068961d45b07b2af510ac002e9daa63a1d3eba2111202d6768798690800fffd

LABEL org.opencontainers.image.source="https://github.com/idp-xyz/idp-bento-go"
LABEL org.opencontainers.image.description="go.idp.xyz vanity metadata service"

COPY --chown=101:101 nginx.conf /etc/nginx/nginx.conf
COPY --chown=101:101 site/index.html /usr/share/nginx/html/index.html
COPY --chown=101:101 site/idp-platform-go.html /usr/share/nginx/html/idp-platform-go.html

USER 101:101
EXPOSE 8080
STOPSIGNAL SIGQUIT

ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
