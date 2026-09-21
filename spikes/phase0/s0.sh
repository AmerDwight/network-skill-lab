#!/usr/bin/env bash
# S0: host environment check (kernel features, cgroup, docker, privileged container basics).
source "$(dirname "$0")/lib.sh"

PROBE_IMG=nsl/spike-probe
PROBE=nsl-s0-probe
trap 'cleanup_containers $PROBE' EXIT

section "host"
log "kernel  $(uname -r)"
log "docker  $(docker info --format 'ver={{.ServerVersion}} cgroupDriver={{.CgroupDriver}} cgroupVer={{.CgroupVersion}} storage={{.Driver}} ncpu={{.NCPU}} memMB={{.MemTotal}}' | awk -F'memMB=' '{printf "%s memMB=%d\n",$1,$2/1048576}')"
check "cgroup v2 unified on host" test "$(stat -fc %T /sys/fs/cgroup)" = cgroup2fs
check "docker cgroup v2" test "$(docker info --format '{{.CgroupVersion}}')" = 2

section "kernel config (/proc/config.gz)"
check "config.gz readable" test -r /proc/config.gz
for opt in VXLAN NF_TABLES NF_CONNTRACK NF_NAT BRIDGE BRIDGE_NETFILTER VLAN_8021Q DUMMY VETH MACVLAN \
           IP_NF_IPTABLES IP_NF_NAT IP_NF_FILTER NETFILTER_XT_MATCH_CONNTRACK NETFILTER_XT_MATCH_COMMENT \
           NETFILTER_XT_MATCH_MULTIPORT NETFILTER_XT_MATCH_ADDRTYPE IP_SET OVERLAY_FS CGROUP_BPF BPF_SYSCALL; do
  val="$(zcat /proc/config.gz | grep -E "^CONFIG_${opt}=" | cut -d= -f2 || true)"
  check "CONFIG_$opt=${val:-unset}" test "$val" = y -o "$val" = m
done

section "privileged container: functional probes"
docker build -q -t "$PROBE_IMG" "$SPIKE_DIR/probe" >/dev/null
docker run -d --name "$PROBE" --privileged "$PROBE_IMG" sleep 600 >/dev/null
p() { docker exec "$PROBE" sh -c "$*"; }
check "vxlan link create"     p "ip link add vx0 type vxlan id 42 dstport 4789 dev eth0"
check "dummy link create"     p "ip link add d0 type dummy"
check "bridge create"         p "ip link add br0 type bridge"
check "veth pair create"      p "ip link add v0 type veth peer name v1"
check "802.1q vlan create"    p "ip link add link eth0 name eth0.10 type vlan id 10"
check "macvlan create"        p "ip link add mv0 link eth0 type macvlan"
check "nft add table"         p "nft add table inet nsl && nft list tables | grep -q nsl"
check "iptables nat table"    p "iptables -t nat -L -n"
check "conntrack sysctl"      p "test -r /proc/sys/net/netfilter/nf_conntrack_max"
check "conntrack -L"          p "conntrack -L"
check "/dev/kmsg readable"    p "test -c /dev/kmsg && head -c 1 /dev/kmsg"
check "/sys/fs/cgroup is cgroup2" p "grep -q ' /sys/fs/cgroup cgroup2 ' /proc/mounts"
check "/sys/fs/cgroup writable"   p "mkdir /sys/fs/cgroup/nsl-probe && rmdir /sys/fs/cgroup/nsl-probe"
check "sysctl net.ipv4.ip_forward writable" p "sysctl -w net.ipv4.ip_forward=1"
check "bridge netfilter sysctl present" p "test -e /proc/sys/net/bridge/bridge-nf-call-iptables || (modprobe br_netfilter 2>/dev/null; test -e /proc/sys/net/bridge/bridge-nf-call-iptables)"

section "modules now loaded on host"
log "$(awk '{print $1}' /proc/modules | grep -E '^(vxlan|bridge|br_netfilter|8021q|dummy|macvlan|nf_tables|nft_|xt_|ip_tables|iptable_|nf_conntrack)' | sort | tr '\n' ' ')"

finish
