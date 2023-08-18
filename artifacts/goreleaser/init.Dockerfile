FROM ubuntu:focal
WORKDIR /
COPY cert-generator .
COPY artifacts/scripts/proxy-init.sh /init.sh

RUN apt-get update && \
  apt-get install --no-install-recommends -y ca-certificates iproute2 iptables && \
  apt-get clean && rm -rf  /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old \


ENTRYPOINT ["/init.sh"]
