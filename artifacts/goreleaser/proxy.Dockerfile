FROM ubuntu:focal

WORKDIR /
COPY ctrlmesh-proxy .

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

COPY artifacts/scripts/proxy-poststart.sh /poststart.sh
RUN mkdir /ctrlmesh && chmod 777 /ctrlmesh

ENTRYPOINT ["/ctrlmesh-proxy"]
