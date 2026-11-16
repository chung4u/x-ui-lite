FROM golang:1.22-bullseye AS builder
ENV GOTOOLCHAIN=go1.27.3
WORKDIR /root
COPY . .
RUN go build -mod=readonly -buildvcs=false -trimpath -o /root/x-ui .


FROM debian:11-slim
RUN apt-get update && apt-get install -y --no-install-recommends -y ca-certificates \
    && apt-get clean && rm -rf /var/lib/apt/lists/* /tmp/* /var/tmp/*
WORKDIR /root
COPY --from=builder /root/x-ui /root/x-ui
COPY bin/geoip.dat bin/geosite.dat bin/xray-linux-amd64 bin/xray-linux-arm64 /root/bin/
VOLUME [ "/etc/x-ui" ]
CMD [ "./x-ui" ]
