#!/usr/bin/env bash

# Copy openkruise/controllermesh

if [ ! "${KUBERNETES_SERVICE_HOST}" ]; then
    echo "Not found KUBERNETES_SERVICE_HOST env. ctrlmesh can only work under in-cluster mode."
    exit 1
fi

SA_DIR=/var/run/secrets/kubernetes.io/serviceaccount
if [ ! -f "${SA_DIR}/token" ]; then
    echo "Not found ${SA_DIR}/token file. ctrlmesh can only work under in-cluster mode."
    exit 1
fi

# Remove the old chains, to generate new configs.
iptables -t nat -D PREROUTING -p tcp -j ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t mangle -D PREROUTING -p tcp -j ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t nat -D OUTPUT -p tcp -j ctrlmesh_PROXY_OUTPUT 2>/dev/null

# Flush and delete the ctrlmesh chains.
iptables -t nat -F ctrlmesh_PROXY_OUTPUT 2>/dev/null
iptables -t nat -X ctrlmesh_PROXY_OUTPUT 2>/dev/null
iptables -t nat -F ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t nat -X ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t mangle -F ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t mangle -X ctrlmesh_PROXY_INBOUND 2>/dev/null
iptables -t mangle -F ctrlmesh_PROXY_DIVERT 2>/dev/null
iptables -t mangle -X ctrlmesh_PROXY_DIVERT 2>/dev/null
iptables -t mangle -F ctrlmesh_PROXY_TPROXY 2>/dev/null
iptables -t mangle -X ctrlmesh_PROXY_TPROXY 2>/dev/null

# Must be last, the others refer to it
iptables -t nat -F ctrlmesh_PROXY_REDIRECT 2>/dev/null
iptables -t nat -X ctrlmesh_PROXY_REDIRECT 2>/dev/null
iptables -t nat -F ctrlmesh_PROXY_IN_REDIRECT 2>/dev/null
iptables -t nat -X ctrlmesh_PROXY_IN_REDIRECT 2>/dev/null

if [ "${1:-}" = "clean" ]; then
  echo "Only cleaning, no new rules added"
  exit 0
fi

# Set defaults
PROXY_APISERVER_PORT=${PROXY_APISERVER_PORT:-5443}
PROXY_WEBHOOK_PORT=${PROXY_WEBHOOK_PORT:-5445}
PROXY_UID=${PROXY_UID:-1359}
INBOUND_INTERCEPTION_MODE=${INBOUND_INTERCEPTION_MODE:-REDIRECT}
INBOUND_TPROXY_MARK=${INBOUND_TPROXY_MARK:-1359}
INBOUND_TPROXY_ROUTE_TABLE=${INBOUND_TPROXY_ROUTE_TABLE:-139}
INBOUND_WEBHOOK_PORT=${INBOUND_WEBHOOK_PORT-}

# Dump out our environment for debugging purposes.
echo "Environment:"
echo "------------"
echo "KUBERNETES_SERVICE_HOST=${KUBERNETES_SERVICE_HOST-}"
echo "KUBERNETES_SERVICE_PORT=${KUBERNETES_SERVICE_PORT-}"
echo "PROXY_UID=${PROXY_UID-}"
echo "INBOUND_INTERCEPTION_MODE=${INBOUND_INTERCEPTION_MODE}"
echo "INBOUND_TPROXY_MARK=${INBOUND_TPROXY_MARK}"
echo "INBOUND_TPROXY_ROUTE_TABLE=${INBOUND_TPROXY_ROUTE_TABLE}"
echo "PROXY_APISERVER_PORT=${PROXY_APISERVER_PORT-}"
echo "PROXY_WEBHOOK_PORT=${PROXY_WEBHOOK_PORT-}"
echo "INBOUND_WEBHOOK_PORT=${INBOUND_WEBHOOK_PORT-}"
echo

set -o errexit
set -o nounset
set -o pipefail
set -x # echo on

# Create a new chain for redirecting outbound traffic to the apiserver port.
# In both chains, '-j RETURN' bypasses Proxy and '-j ctrlmesh_PROXY_REDIRECT' redirects to Proxy.
iptables -t nat -N ctrlmesh_PROXY_REDIRECT
iptables -t nat -A ctrlmesh_PROXY_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_APISERVER_PORT}"

# Use this chain also for redirecting inbound traffic to the webhook port when not using TPROXY.
iptables -t nat -N ctrlmesh_PROXY_IN_REDIRECT
iptables -t nat -A ctrlmesh_PROXY_IN_REDIRECT -p tcp -j REDIRECT --to-port "${PROXY_WEBHOOK_PORT}"

# Handling of inbound ports. Traffic will be redirected to Proxy, which will process and forward
# to the local webhook. If not set, no inbound port will be intercepted by the iptables.
if [ -n "${INBOUND_WEBHOOK_PORT}" ]; then
  if [ "${INBOUND_INTERCEPTION_MODE}" = "TPROXY" ] ; then
    # When using TPROXY, create a new chain for routing all inbound traffic to
    # Proxy. Any packet entering this chain gets marked with the ${INBOUND_TPROXY_MARK} mark,
    # so that they get routed to the loopback interface in order to get redirected to Proxy.
    # In the ctrlmesh_PROXY_INBOUND chain, '-j ctrlmesh_PROXY_DIVERT' reroutes to the loopback
    # interface.
    # Mark all inbound packets.
    iptables -t mangle -N ctrlmesh_PROXY_DIVERT
    iptables -t mangle -A ctrlmesh_PROXY_DIVERT -j MARK --set-mark "${INBOUND_TPROXY_MARK}"
    iptables -t mangle -A ctrlmesh_PROXY_DIVERT -j ACCEPT

    # Route all packets marked in chain ctrlmesh_PROXY_DIVERT using routing table ${INBOUND_TPROXY_ROUTE_TABLE}.
    ip -f inet rule add fwmark "${INBOUND_TPROXY_MARK}" lookup "${INBOUND_TPROXY_ROUTE_TABLE}"
    # In routing table ${INBOUND_TPROXY_ROUTE_TABLE}, create a single default rule to route all traffic to
    # the loopback interface.
    ip -f inet route add local default dev lo table "${INBOUND_TPROXY_ROUTE_TABLE}" || ip route show table all

    # Create a new chain for redirecting inbound traffic to the common Envoy
    # port.
    # In the ctrlmesh_PROXY_INBOUND chain, '-j RETURN' bypasses Envoy and
    # '-j ctrlmesh_PROXY_TPROXY' redirects to Envoy.
    iptables -t mangle -N ctrlmesh_PROXY_TPROXY
    iptables -t mangle -A ctrlmesh_PROXY_TPROXY ! -d 127.0.0.1/32 -p tcp -j TPROXY --tproxy-mark "${INBOUND_TPROXY_MARK}"/0xffffffff --on-port "${PROXY_PORT}"

    table=mangle
  else
    table=nat
  fi
  iptables -t "${table}" -N ctrlmesh_PROXY_INBOUND
  iptables -t "${table}" -A PREROUTING -p tcp -j ctrlmesh_PROXY_INBOUND

  if [ "${INBOUND_INTERCEPTION_MODE}" = "TPROXY" ]; then
    iptables -t mangle -A ctrlmesh_PROXY_INBOUND -p tcp --dport "${INBOUND_WEBHOOK_PORT}" -m socket -j ctrlmesh_PROXY_DIVERT || echo "No socket match support"
    iptables -t mangle -A ctrlmesh_PROXY_INBOUND -p tcp --dport "${INBOUND_WEBHOOK_PORT}" -m socket -j ctrlmesh_PROXY_DIVERT || echo "No socket match support"
    iptables -t mangle -A ctrlmesh_PROXY_INBOUND -p tcp --dport "${INBOUND_WEBHOOK_PORT}" -j ctrlmesh_PROXY_TPROXY
  else
    iptables -t nat -A ctrlmesh_PROXY_INBOUND -p tcp --dport "${INBOUND_WEBHOOK_PORT}" -j ctrlmesh_PROXY_IN_REDIRECT
  fi
fi

# Create a new chain for selectively redirecting outbound packets to Proxy.
iptables -t nat -N ctrlmesh_PROXY_OUTPUT

# Jump to the ctrlmesh_PROXY_OUTPUT chain from OUTPUT chain for all tcp traffic.
iptables -t nat -A OUTPUT -p tcp -j ctrlmesh_PROXY_OUTPUT

for uid in ${PROXY_UID}; do
  # Avoid infinite loops. Don't redirect Proxy traffic directly back to
  # Proxy for non-loopback traffic.
  iptables -t nat -A ctrlmesh_PROXY_OUTPUT -m owner --uid-owner "${uid}" -j RETURN
done

# Redirect all apiserver outbound traffic to Proxy.
iptables -t nat -A ctrlmesh_PROXY_OUTPUT -d "${KUBERNETES_SERVICE_HOST}" -j ctrlmesh_PROXY_REDIRECT

# Generate certs
mount -o remount,rw "${SA_DIR}"
mkdir -p "${SA_DIR}/ctrlmesh"
/cert-generator -dns-name "${KUBERNETES_SERVICE_HOST}" -dir "${SA_DIR}/ctrlmesh"
if [ ! -f "${SA_DIR}/ca.crt" ]; then
  (cd "${SA_DIR}"; ln -s ctrlmesh/ca-cert.pem ca.crt)
elif [ "$(readlink "${SA_DIR}/ca.crt")" = "..data/ca.crt" ]; then
  (cd "${SA_DIR}"; rm -f ca.crt; ln -s ctrlmesh/ca-cert.pem ca.crt)
fi

exit 0
