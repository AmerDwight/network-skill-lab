#!/usr/bin/env bash
# V3b: guest-agent exec still works with every interface down and nftables dropping everything.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$D"
N=${1:-nsl-a}
ip=$(dom_ip "$N")
{
  if gexec "$N" "command -v nft" >/dev/null; then
    gexec "$N" "sudo nft add table inet spike; sudo nft 'add chain inet spike in { type filter hook input priority 0; policy drop; }'; sudo nft 'add chain inet spike out { type filter hook output priority 0; policy drop; }'"
    echo "nft policy drop installed: $(gexec "$N" "sudo nft list ruleset | grep -c 'policy drop'") chains"
  else
    echo "nft not present in stock cloud image (build.sh installs nftables); skipping nft step"
  fi
  gexec "$N" "for i in \$(ls /sys/class/net | grep -v lo); do sudo ip link set dev \$i down; done"
  echo "ssh with everything down: $($SSH nsl@"$ip" echo ok 2>&1 || echo FAILED-as-expected)"
  t0=$(date +%s.%N)
  out=$(gexec "$N" "ip -br link | grep -v lo; echo exit-probe; exit 3" || echo "exit=$?")
  printf 'guest-exec with all links down + nft drop: ok in %.2fs\n' "$(echo "$(date +%s.%N) - $t0" | bc)"
  echo "$out" | sed 's/^/  /'
  gexec "$N" "sudo nft delete table inet spike 2>/dev/null; for i in \$(ls /sys/class/net | grep -v lo); do sudo ip link set dev \$i up; done"
  until $SSH nsl@"$ip" true 2>/dev/null; do sleep 0.5; done
  echo "restored: ssh ok"
} | tee "$RES/v3b.txt"
