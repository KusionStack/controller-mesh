FROM golang:1.20 as builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY artifacts/ artifacts/
COPY pkg/ pkg/
COPY vendor/ vendor/

RUN CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOARCH=amd64 go build -mod=vendor -a -o ctrlmesh-proxy ./pkg/cmd/proxy/main.go

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

# Sudoers used to allow tcpdump and other debug utilities.
RUN useradd -m --uid 1359 ctrlmesh-proxy && \
  echo "ctrlmesh-proxy ALL=NOPASSWD: ALL" >> /etc/sudoers
WORKDIR /
COPY artifacts/scripts/proxy-poststart.sh /poststart.sh
RUN mkdir /ctrlmesh && chmod 777 /ctrlmesh
COPY --from=builder /workspace/ctrlmesh-proxy .
ENTRYPOINT ["/ctrlmesh-proxy"]
