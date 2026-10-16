#!/usr/bin/env bash
# V3: guest-agent exec keeps working while the guest's management link is down.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$D"
N=${1:-nsl-v2}

ip=$(dom_ip "$N")
iface=$(gexec "$N" "ip -o link | awk -F': ' '\$2!=\"lo\"{print \$2; exit}'")
{
  echo "domain $N mgmt iface $iface ip $ip"
  echo "ssh before: $($SSH nsl@"$ip" echo ok)"
  gexec "$N" "sudo ip link set dev $iface down" 
  sleep 1
  echo "ssh while down: $($SSH nsl@"$ip" echo ok 2>&1 || echo FAILED-as-expected)"
  t0=$(date +%s.%N)
  out=$(gexec "$N" "hostname; id -un; ip -br link | grep -v lo")
  printf 'guest-exec while down: ok in %.2fs\n' "$(echo "$(date +%s.%N) - $t0" | bc)"
  echo "$out" | sed 's/^/  /'
  echo "guest-exec exit code propagation: $(gexec "$N" 'exit 7' || echo "got $?")"
  gexec "$N" "sudo ip link set dev $iface up"
  until $SSH nsl@"$ip" true 2>/dev/null; do sleep 0.5; done
  echo "ssh after up: ok"
} | tee "$RES/v3.txt"
