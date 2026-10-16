#!/usr/bin/env bash
# V2b: boot an overlay of the build.sh output and verify the node contract (eth0 naming, tools, bootstrap).
set -euo pipefail
. "$(dirname "$0")/lib.sh"
IMG=${IMG:-$D/build/nsl-node.qcow2}
cd "$D"
N=nsl-img
destroy_domain "$N"
rm -f "$N.qcow2" "$N-seed.iso"
qemu-img create -q -f qcow2 -b "$IMG" -F qcow2 "$N.qcow2"
seed_iso "$N" "$N-seed.iso" "" "$(printf 'version: 2\nethernets:\n  eth0: {dhcp4: true, dhcp-identifier: mac}\n')"
domain_xml "$N" "$D/$N.qcow2" "$D/$N-seed.iso" 1024 1 default > "$N.xml"
$V define "$N.xml" >/dev/null
t0=$(date +%s.%N)
$V start "$N" >/dev/null
until agent_ping "$N"; do sleep 0.5; done
t_agent=$(echo "$(date +%s.%N) - $t0" | bc)
ip=""; until [ -n "$ip" ]; do ip=$(dom_ip "$N"); [ -n "$ip" ] || sleep 0.5; done
until $SSH nsl@"$ip" true 2>/dev/null; do sleep 0.5; done
t_ssh=$(echo "$(date +%s.%N) - $t0" | bc)
{
  echo "image $IMG ($(qemu-img info "$IMG" | awk -F': ' '/disk size/{print $2}'))"
  printf 'guest-agent ready: %.1fs   ssh ready: %.1fs\n' "$t_agent" "$t_ssh"
  echo "links: $($SSH nsl@"$ip" ip -br link | awk '{print $1}' | tr '\n' ' ')"
  echo "tools: $($SSH nsl@"$ip" 'for t in nft iptables tcpdump netplan nmcli snmpwalk k3s; do printf "%s=%s " $t $(command -v $t >/dev/null && echo yes || echo no); done')"
  echo "bootstrap: $($SSH nsl@"$ip" 'ls /usr/local/sbin/nsl-bootstrap /etc/sudoers.d/nsl /etc/nsl 2>&1 | tr "\n" " "')"
  echo "sudo: $($SSH nsl@"$ip" sudo -n id -un)"
  echo "machine-id: $($SSH nsl@"$ip" cat /etc/machine-id)"
  echo "services: $($SSH nsl@"$ip" 'systemctl is-active qemu-guest-agent ssh systemd-networkd NetworkManager nginx | tr "\n" " "')"
} | tee "$RES/v2b.txt"
destroy_domain "$N"
