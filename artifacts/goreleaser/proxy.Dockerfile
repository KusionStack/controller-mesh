FROM ubuntu:focal

WORKDIR /
COPY kridge-proxy .

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
RUN useradd -m --uid 1359 kridge-proxy && \
  echo "kridge-proxy ALL=NOPASSWD: ALL" >> /etc/sudoers

COPY artifacts/scripts/proxy-poststart.sh /poststart.sh
RUN mkdir /kridge && chmod 777 /kridge

ENTRYPOINT ["/kridge-proxy"]
