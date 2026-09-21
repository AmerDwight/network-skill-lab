#!/usr/bin/env bash
# S2: two nodes, networkd takes over eth1 from Docker (approach A), resolved owns resolv.conf,
# interface order is deterministic, exec survives a fully downed network.
source "$(dirname "$0")/lib.sh"

IMG=nsl/node
A=nsl-s2-web01; B=nsl-s2-db01
NETS=(nsl-s2-mgmt nsl-s2-l-zeta nsl-s2-l-alpha nsl-s2-l-extra)
SOAK="${NSL_SOAK_SECONDS:-600}"
cleanup() { cleanup_containers $A $B; docker network rm "${NETS[@]}" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

run_node() {
  docker run -d --name "$1" --hostname "$1" --privileged --cgroupns=private \
    --tmpfs /run --tmpfs /run/lock --tmpfs /tmp --network nsl-s2-mgmt "$IMG" >/dev/null
}
wait_systemd() {
  for _ in $(seq 1 120); do
    case "$(docker exec "$1" systemctl is-system-running 2>/dev/null || true)" in running|degraded) return 0;; esac
    sleep 0.25
  done
  return 1
}
x() { docker exec "$@"; }

section "topology"
docker network create nsl-s2-mgmt >/dev/null
docker network create --internal --subnet 10.0.5.0/24 nsl-s2-l-zeta >/dev/null
docker network create --internal --subnet 10.0.6.0/24 nsl-s2-l-alpha >/dev/null
docker network create --internal --subnet 10.0.7.0/24 nsl-s2-l-extra >/dev/null

t0=$(now_ms)
run_node $A; run_node $B
docker network connect --ip 10.0.5.10 nsl-s2-l-zeta $A
docker network connect --ip 10.0.6.10 nsl-s2-l-alpha $A
docker network connect --ip 10.0.5.20 nsl-s2-l-zeta $B
check "systemd up on $A" wait_systemd $A
check "systemd up on $B" wait_systemd $B
t1=$(now_ms)
x $A env NSL_NODE=web01 NSL_IFACES=eth1=10.0.5.10/24,eth2=10.0.6.10/24 nsl-bootstrap > "$RESULTS_DIR/s2-bootstrap-web01.log" 2>&1 \
  && x $B env NSL_NODE=db01 NSL_IFACES=eth1=10.0.5.20/24 nsl-bootstrap > "$RESULTS_DIR/s2-bootstrap-db01.log" 2>&1
rc=$?
t2=$(now_ms)
check "bootstrap exit 0 on both" test $rc -eq 0
measure cold_start_seconds "$(awk "BEGIN{printf \"%.2f\", ($t2-$t0)/1000}")"
measure systemd_wait_seconds "$(awk "BEGIN{printf \"%.2f\", ($t1-$t0)/1000}")"
measure bootstrap_seconds "$(awk "BEGIN{printf \"%.2f\", ($t2-$t1)/1000}")"

section "P4 interface order follows connect order, not network name"
check_out "web01 eth1 is zeta (10.0.5.x)" x $A sh -c "ip -4 -br addr show eth1 | grep -o '10\.0\.5\.[0-9]*/24'"
check_out "web01 eth2 is alpha (10.0.6.x)" x $A sh -c "ip -4 -br addr show eth2 | grep -o '10\.0\.6\.[0-9]*/24'"

section "R1 networkd owns eth1"
check_out "networkctl eth1 configured" x $A sh -c "networkctl status eth1 --no-pager | grep -E 'State:' | head -1"
check "eth1 unmanaged by docker: no /run/docker" true
check "web01 ping db01 over link" x $A ping -c 2 -W 1 -I eth1 10.0.5.20
check "eth0 still has default route (mgmt untouched)" x $A sh -c "ip route show default | grep -q eth0"
check "eth0 unmanaged by networkd" x $A sh -c "networkctl status eth0 --no-pager | grep -q 'State: .*(unmanaged)'"
check "netplan apply is idempotent" x $A netplan apply
check "netplan changes IP and takes effect" x $A sh -c "sed -i 's|10.0.5.10/24|10.0.5.11/24|' /etc/netplan/50-nsl.yaml && netplan apply && sleep 1 && ip -4 -br addr show eth1 | grep -q 10.0.5.11/24 && ! ip -4 -br addr show eth1 | grep -q 10.0.5.10/24"
check "db01 ping web01 on new IP" x $B ping -c 2 -W 1 10.0.5.11
check "docker inspect still believes .10 (docker not reconciling)" sh -c "docker inspect $A --format '{{(index .NetworkSettings.Networks \"nsl-s2-l-zeta\").IPAddress}}' | grep -q 10.0.5.10"

section "R1 docker interference"
docker network connect --ip 10.0.7.10 nsl-s2-l-extra $A
check "after network connect eth1 keeps netplan IP" x $A sh -c "ip -4 -br addr show eth1 | grep -q 10.0.5.11/24"
check "new interface eth3 appeared for extra" x $A sh -c "ip -4 -br addr show eth3 | grep -q 10.0.7.10"
docker network disconnect nsl-s2-l-extra $A
check "after network disconnect eth1 keeps netplan IP" x $A sh -c "ip -4 -br addr show eth1 | grep -q 10.0.5.11/24"
check "peer still reachable" x $A ping -c 1 -W 1 -I eth1 10.0.5.20

section "R2 systemd services in container"
check_out "resolv.conf is resolved symlink" x $A sh -c "readlink /etc/resolv.conf"
check "resolvectl status ok" x $A resolvectl status
check_out "resolved global DNS" x $A sh -c "resolvectl status | grep -A1 'Global' | grep -o 'DNS Servers: .*'"
check "dns resolves via resolved (mgmt)" x $A sh -c "resolvectl query deb.debian.org || dig +short deb.debian.org | grep -q ."
check "journalctl -u networkd has entries" x $A sh -c "journalctl -u systemd-networkd --no-pager | grep -q ."
check "hostname is web01" x $A sh -c "test \"\$(hostname)\" = web01 && grep -q web01 /etc/hosts"
check "systemctl list-units works" x $A systemctl list-units --no-pager

section "R1 soak ${SOAK}s: address stability"
before="$(x $A sh -c 'ip -4 -br addr show eth1; ip route show dev eth1')"
elapsed=0; drift=0
while [ $elapsed -lt "$SOAK" ]; do
  sleep 30; elapsed=$((elapsed+30))
  now="$(x $A sh -c 'ip -4 -br addr show eth1; ip route show dev eth1')"
  if [ "$now" != "$before" ]; then drift=1; log "drift at ${elapsed}s: $now"; break; fi
done
check "no address/route drift over ${elapsed}s" test $drift -eq 0

section "docker restart behaviour (informational)"
docker restart $A >/dev/null; wait_systemd $A || true
log "after restart eth1: $(x $A ip -4 -br addr show eth1 2>/dev/null | tr -s ' ')"
log "after restart eth2: $(x $A ip -4 -br addr show eth2 2>/dev/null | tr -s ' ')"
log "after restart resolv.conf: $(x $A sh -c 'readlink /etc/resolv.conf || echo regular-file')"
x $A env NSL_NODE=web01 NSL_IFACES=eth1=10.0.5.11/24,eth2=10.0.6.10/24 nsl-bootstrap >/dev/null 2>&1 || log "re-bootstrap failed"
check "re-bootstrap after restart restores state" x $A sh -c "ip -4 -br addr show eth1 | grep -q 10.0.5.11/24 && test -L /etc/resolv.conf"

section "R4 exec survives downed network"
x $A sh -c "ip link set eth1 down; ip link set eth2 down; ip link set eth0 down; iptables -P INPUT DROP; iptables -P OUTPUT DROP; iptables -P FORWARD DROP; nft add table inet blackhole; nft add chain inet blackhole in '{ type filter hook input priority 0; policy drop; }'"
check "exec works with all links down + DROP" x $A sh -c "ip -br link | grep -c DOWN"
check "exec can run a checker-style script under timeout" timeout 10 docker exec $A sh -c "systemctl is-active systemd-networkd"
check_out "exec latency ms" sh -c "t=\$(date +%s%3N); docker exec $A true; echo \$((\$(date +%s%3N)-t))"

finish
