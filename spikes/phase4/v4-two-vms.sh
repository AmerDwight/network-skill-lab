#!/usr/bin/env bash
# V4+V5: two VMs sharing an isolated libvirt link network plus NAT management; cold-start timing and resources.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$D"
destroy_domain nsl-v2
for n in nsl-a nsl-b; do destroy_domain $n; rm -f "$n.qcow2" "$n-seed.iso"; done
$V net-destroy nsl-link-a >/dev/null 2>&1 || true
$V net-create /dev/stdin >/dev/null <<XML
<network><name>nsl-link-a</name><bridge name='nslbr-a' stp='off' delay='0'/></network>
XML
mem_before=$(free -m | awk '/Mem:/{print $3}')
netcfg() { printf 'version: 2\nethernets:\n  enp1s0: {dhcp4: true, dhcp-identifier: mac}\n  enp2s0: {addresses: [%s/24]}\n' "$1"; }
qemu-img create -q -f qcow2 -b base.qcow2 -F qcow2 nsl-a.qcow2
qemu-img create -q -f qcow2 -b base.qcow2 -F qcow2 nsl-b.qcow2
seed_iso nsl-a nsl-a-seed.iso "" "$(netcfg 10.10.0.1)"
seed_iso nsl-b nsl-b-seed.iso "" "$(netcfg 10.10.0.2)"
domain_xml nsl-a "$D/nsl-a.qcow2" "$D/nsl-a-seed.iso" 1024 1 default nsl-link-a > nsl-a.xml
domain_xml nsl-b "$D/nsl-b.qcow2" "$D/nsl-b-seed.iso" 1024 1 default nsl-link-a > nsl-b.xml
$V define nsl-a.xml >/dev/null; $V define nsl-b.xml >/dev/null
t0=$(date +%s.%N)
$V start nsl-a >/dev/null; $V start nsl-b >/dev/null
until agent_ping nsl-a && agent_ping nsl-b; do sleep 0.5; done
t_agent=$(echo "$(date +%s.%N) - $t0" | bc)
ipa=""; ipb=""
until [ -n "$ipa" ] && [ -n "$ipb" ]; do ipa=$(dom_ip nsl-a); ipb=$(dom_ip nsl-b); sleep 0.5; done
until $SSH nsl@"$ipa" true 2>/dev/null && $SSH nsl@"$ipb" true 2>/dev/null; do sleep 0.5; done
t_ssh=$(echo "$(date +%s.%N) - $t0" | bc)
sleep 5
mem_after=$(free -m | awk '/Mem:/{print $3}')
{
  echo "nsl-a mgmt $ipa  nsl-b mgmt $ipb"
  printf 'both guest-agents ready: %.1fs\n' "$t_agent"
  printf 'both ssh ready:          %.1fs (target <90s cold start)\n' "$t_ssh"
  echo "link a->b: $($SSH nsl@"$ipa" ping -c 3 -W 1 10.10.0.2 | tail -2 | head -1)"
  echo "link b->a: $($SSH nsl@"$ipb" ping -c 3 -W 1 10.10.0.1 | tail -2 | head -1)"
  echo "nsl-a enp2s0: $($SSH nsl@"$ipa" ip -br addr show enp2s0)"
  echo "host bridge nslbr-a addrs: [$(ip -br addr show nslbr-a | awk '{$1="";$2="";print}' | xargs)] (expect none)"
  echo "a -> host via link (10.10.0.254): $($SSH nsl@"$ipa" ping -c 1 -W 1 10.10.0.254 >/dev/null 2>&1 && echo REACHABLE || echo unreachable-as-expected)"
  echo "a -> internet via mgmt: $($SSH nsl@"$ipa" "getent hosts archive.ubuntu.com >/dev/null && echo ok || echo no-dns")"
  echo "host used memory: before ${mem_before}MiB after ${mem_after}MiB (+$((mem_after-mem_before)))"
  echo "qemu rss:"; ps -C qemu-system-x86_64 -o rss=,args= | awk '{printf "  %d MiB %s\n", $1/1024, $4}'
  echo "overlay disk usage: $(du -shc nsl-a.qcow2 nsl-b.qcow2 | tail -1 | cut -f1) for two overlays"
  echo "host load: $(cut -d' ' -f1-3 /proc/loadavg)"
} | tee "$RES/v4.txt"
