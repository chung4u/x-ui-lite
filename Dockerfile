FROM alpine:3.22 AS release
ARG TARGETARCH
ARG XUI_VERSION=v1.1.3
WORKDIR /release
RUN set -eu; \
    archive="x-ui-linux-${TARGETARCH}.tar.gz"; \
    base="https://github.com/chung4u/x-ui-lite/releases/download/${XUI_VERSION}"; \
    wget -qO "$archive" "$base/$archive"; wget -qO SHA256SUMS "$base/SHA256SUMS"; \
    awk -v name="$archive" '$2 == name { print; found++ } END { exit found != 1 }' SHA256SUMS > selected.sha256; \
    sha256sum -c selected.sha256; \
    mkdir /bundle; tar -xzf "$archive" -C /bundle; \
    cd /bundle; sha256sum -c PACKAGE.SHA256

FROM debian:bookworm-slim
WORKDIR /root
COPY --from=release /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/ca-certificates.crt
COPY --from=release /bundle/ /root/
VOLUME ["/etc/x-ui"]
CMD ["./x-ui"]
