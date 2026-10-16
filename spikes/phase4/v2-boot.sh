#!/usr/bin/env bash
# V2: boot one VM from an overlay of base.qcow2; time guest-agent and SSH readiness.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$D"
N=${1:-nsl-v2}
destroy_domain "$N"
rm -f "$N.qcow2" "$N-seed.iso"
qemu-img create -q -f qcow2 -b base.qcow2 -F qcow2 "$N.qcow2"
seed_iso "$N" "$N-seed.iso"
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
  echo "domain $N ip $ip"
  printf 'guest-agent ready: %.1fs (target <45s)\n' "$t_agent"
  printf 'ssh ready:         %.1fs (target <60s)\n' "$t_ssh"
  echo "uname: $($SSH nsl@"$ip" uname -r)"
} | tee "$RES/v2.txt"
