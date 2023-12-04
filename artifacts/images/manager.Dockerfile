# Build the manager binary
FROM golang:1.20 as builder

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
COPY artifacts/ artifacts/
COPY pkg/ pkg/
COPY vendor/ vendor/

RUN CGO_ENABLED=0 GO111MODULE=on GOOS=linux GOARCH=amd64 go build -mod=vendor -a -o ctrlmesh-manager ./pkg/cmd/manager/main.go


FROM ubuntu:focal
# This is required by daemon connnecting with cri
RUN ln -s /usr/bin/* /usr/sbin/ && apt-get update -y \
  && apt-get install --no-install-recommends -y \
    sudo \
    net-tools \
    curl \
    ca-certificates \
  && apt-get clean && rm -rf /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old
WORKDIR /
COPY --from=builder /workspace/ctrlmesh-manager .
ENTRYPOINT ["/ctrlmesh-manager"]
