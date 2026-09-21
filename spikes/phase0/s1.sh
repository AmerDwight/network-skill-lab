#!/usr/bin/env bash
# S1: build nsl/node and verify systemd boots cleanly with all tools present.
source "$(dirname "$0")/lib.sh"

IMG=nsl/node
C=nsl-s1-node
trap 'cleanup_containers $C' EXIT

section "build"
t0=$(now_ms)
docker build -t "$IMG" "$SPIKE_DIR/../../images/node" > "$RESULTS_DIR/s1-build.log" 2>&1 || { log "build failed, see results/s1-build.log"; tail -20 "$RESULTS_DIR/s1-build.log"; exit 1; }
measure build_seconds $(( ($(now_ms) - t0) / 1000 ))
measure image_size "$(docker image inspect "$IMG" --format '{{.Size}}' | awk '{printf "%.0f MB", $1/1048576}')"

section "boot"
t0=$(now_ms)
docker run -d --name "$C" --privileged --cgroupns=private --tmpfs /run --tmpfs /run/lock --tmpfs /tmp "$IMG" >/dev/null
state=starting
for _ in $(seq 1 120); do
  state="$(docker exec "$C" systemctl is-system-running 2>/dev/null || true)"
  case "$state" in running|degraded) break;; esac
  sleep 0.25
done
measure boot_seconds "$(awk "BEGIN{printf \"%.2f\", ($(now_ms) - $t0)/1000}")"
log "systemd state: $state"
check "systemd reached running or degraded" test "$state" = running -o "$state" = degraded
failed="$(docker exec "$C" systemctl list-units --state=failed --no-legend --plain | awk '{print $1}' | tr '\n' ' ')"
log "failed units: ${failed:-none}"
check "no failed units" test -z "$failed"
for u in systemd-networkd systemd-resolved ssh rsyslog cron chrony dbus; do
  check "unit active: $u" docker exec "$C" systemctl is-active --quiet "$u"
done
check "NetworkManager inactive" bash -c "! docker exec $C systemctl is-active --quiet NetworkManager"
check "k3s installed but inactive" bash -c "docker exec $C systemctl is-enabled k3s 2>/dev/null | grep -q disabled"
check_out "journalctl works" docker exec "$C" sh -c "journalctl --no-pager -n 1 -o cat | head -c 80"
check_out "resolvectl works" docker exec "$C" sh -c "resolvectl status | head -1"

section "tools in PATH"
for b in ip ifconfig ethtool ping arping tracepath traceroute mtr nc socat iperf3 telnet nmap arp-scan dig nslookup resolvectl \
         tcpdump tshark iftop conntrack iptables nft ufw netplan networkctl nmcli dhclient sshd chronyc nginx rsyslogd \
         snmpwalk snmpd lldpd snmptrapd curl wget openssl rsync ps killall lsof strace htop sar vim nano less jq yq tmux tree git \
         man file unzip sudo crontab k3s kubectl crictl ctr helm k9s kustomize; do
  check "bin: $b" docker exec "$C" sh -c "command -v $b"
done
check "k3s airgap tarball present" docker exec "$C" test -s /usr/share/nsl/k3s-airgap-images-amd64.tar.zst
check_out "k3s version" docker exec "$C" k3s --version
check "man page for ip present" docker exec "$C" sh -c "man -w ip"
check "history hook installed" docker exec "$C" test -r /etc/profile.d/nsl-history.sh

finish
