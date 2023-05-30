FROM golang:1.19 as builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum

COPY artifacts/ artifacts/
COPY pkg/ pkg/
COPY vendor/ vendor/

# Build
RUN CGO_ENABLED=0 GO111MODULE=on go build -mod=vendor -a -o cert-generator ./pkg/cmd/cert-generator/main.go


# Use distroless as minimal base image to package the manager binary
FROM ubuntu:focal
RUN apt-get update && \
  apt-get install --no-install-recommends -y ca-certificates iproute2 iptables && \
  apt-get clean && rm -rf  /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old
WORKDIR /
COPY --from=builder /workspace/cert-generator .
COPY artifacts/scripts/proxy-init.sh /init.sh
ENTRYPOINT ["/init.sh"]
