FROM ubuntu:focal

WORKDIR /
COPY ctrlmesh-manager .

RUN ln -s /usr/bin/* /usr/sbin/ && apt-get update -y \
  && apt-get install --no-install-recommends -y ca-certificates \
  && apt-get clean && rm -rf /var/log/*log /var/lib/apt/lists/* /var/log/apt/* /var/lib/dpkg/*-old /var/cache/debconf/*-old


ENTRYPOINT ["/ctrlmesh-manager"]
