# Build the manager binary
FROM golang:1.19 as builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY e2e/ e2e/
COPY vendor/ vendor/

RUN CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOARCH=amd64 go build -mod=vendor -a -o testapp ./e2e/customoperator/app/main.go


FROM ubuntu:focal


RUN apt-get update && \
  apt-get install --no-install-recommends -y \
  ca-certificates \
  curl \
  iputils-ping \
  tcpdump \
  iproute2 \
  iptables \
  net-tools \
  telnet \
  lsof \
  linux-tools-generic \
  sudo && \
  apt-get clean && \
  rm -rf  /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old

WORKDIR /
COPY --from=builder /workspace/testapp .
ENTRYPOINT ["/testapp"]
