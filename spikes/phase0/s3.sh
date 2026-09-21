#!/usr/bin/env bash
# S3: k3s server + agent in two nsl/node containers, offline (internal networks only),
# cross-node pod ping over flannel VXLAN, cold start and resource measurements.
source "$(dirname "$0")/lib.sh"

IMG=nsl/node
S=nsl-s3-k3s01; A=nsl-s3-k3s02
export S A
NETS=(nsl-s3-mgmt nsl-s3-link)
TOKEN=nsl-spike-token
cleanup() { cleanup_containers $S $A; docker network rm "${NETS[@]}" >/dev/null 2>&1 || true; }
trap cleanup EXIT
cleanup

run_node() {
  docker run -d --name "$1" --hostname "$1" --privileged --cgroupns=private \
    --tmpfs /run --tmpfs /run/lock --tmpfs /tmp --mount type=volume,dst=/var/lib/rancher/k3s/agent/containerd \
    --network nsl-s3-mgmt "$IMG" >/dev/null
}
wait_systemd() {
  for _ in $(seq 1 120); do
    case "$(docker exec "$1" systemctl is-system-running 2>/dev/null || true)" in running|degraded) return 0;; esac
    sleep 0.25
  done
  return 1
}
x() { docker exec "$@"; }
k() { docker exec $S kubectl --kubeconfig /etc/rancher/k3s/k3s.yaml "$@"; }

section "topology (both networks internal: no internet)"
docker network create --internal --subnet 10.0.9.0/24 nsl-s3-mgmt >/dev/null
docker network create --internal --subnet 10.0.8.0/24 nsl-s3-link >/dev/null
t0=$(now_ms)
run_node $S; run_node $A
docker network connect --ip 10.0.8.10 nsl-s3-link $S
docker network connect --ip 10.0.8.20 nsl-s3-link $A
check "systemd up on $S" wait_systemd $S
check "systemd up on $A" wait_systemd $A
t1=$(now_ms)
x $S env NSL_NODE=k3s01 NSL_ROLE=k3s-server NSL_K3S_TOKEN=$TOKEN NSL_IFACES=eth1=10.0.8.10/24 NSL_DNS=10.0.9.1 nsl-bootstrap > "$RESULTS_DIR/s3-bootstrap-k3s01.log" 2>&1 &
x $A env NSL_NODE=k3s02 NSL_ROLE=k3s-agent NSL_K3S_SERVER=10.0.8.10 NSL_K3S_TOKEN=$TOKEN NSL_IFACES=eth1=10.0.8.20/24 NSL_DNS=10.0.9.1 nsl-bootstrap > "$RESULTS_DIR/s3-bootstrap-k3s02.log" 2>&1 &
wait
t2=$(now_ms)
measure systemd_wait_seconds "$(awk "BEGIN{printf \"%.2f\", ($t1-$t0)/1000}")"
measure bootstrap_seconds "$(awk "BEGIN{printf \"%.2f\", ($t2-$t1)/1000}")"

section "cluster comes up"
ready=0
for i in $(seq 1 360); do
  n="$(k get nodes --no-headers 2>/dev/null | awk '$2=="Ready"' | wc -l || true)"
  if [ "$n" -eq 2 ]; then ready=1; break; fi
  sleep 0.5
done
t3=$(now_ms)
check "two nodes Ready" test $ready -eq 1
measure cold_start_to_2_ready_seconds "$(awk "BEGIN{printf \"%.2f\", ($t3-$t0)/1000}")"
log "$(k get nodes -o wide --no-headers 2>/dev/null)"
node_ip_is_eth1() { k get node k3s01 -o jsonpath='{.status.addresses[?(@.type=="InternalIP")].address}' | grep -q 10.0.8.10; }
check "node k3s01 internal IP is eth1" node_ip_is_eth1
for _ in $(seq 1 240); do
  pend="$(k get pods -A --no-headers 2>/dev/null | awk '$4!="Running" && $4!="Completed"' | wc -l || true)"
  [ "$pend" -eq 0 ] && [ "$(k get pods -A --no-headers 2>/dev/null | wc -l)" -gt 3 ] && break
  sleep 0.5
done
t4=$(now_ms)
measure system_pods_settled_seconds "$(awk "BEGIN{printf \"%.2f\", ($t4-$t0)/1000}")"
log "$(k get pods -A -o wide --no-headers 2>/dev/null)"
pods_settled() {
  local i bad
  for i in $(seq 1 60); do
    bad="$(k get pods -A --no-headers 2>/dev/null | awk '$4!="Running" && $4!="Completed"' | wc -l || true)"
    [ "$bad" -eq 0 ] && return 0
    sleep 1
  done
  return 1
}
check "all system pods Running/Completed" pods_settled

section "R3 cross-node pod traffic over flannel"
k run p1 --image=docker.io/rancher/mirrored-library-busybox:1.37.0 --image-pull-policy=Never --restart=Never \
  --overrides='{"spec":{"nodeName":"k3s01"}}' -- sleep 3600 >/dev/null
k run p2 --image=docker.io/rancher/mirrored-library-busybox:1.37.0 --image-pull-policy=Never --restart=Never \
  --overrides='{"spec":{"nodeName":"k3s02"}}' -- sleep 3600 >/dev/null
check "pods running on distinct nodes" k wait --for=condition=Ready pod/p1 pod/p2 --timeout=90s
p2ip="$(k get pod p2 -o jsonpath='{.status.podIP}')"
log "p1 on $(k get pod p1 -o jsonpath='{.spec.nodeName}') / p2 on $(k get pod p2 -o jsonpath='{.spec.nodeName}') ip $p2ip"
check "p1 pings p2 across nodes" k exec p1 -- ping -c 2 -W 2 "$p2ip"
check_out "p1 resolves kubernetes.default.svc.cluster.local via coredns" k exec p1 -- sh -c "nslookup kubernetes.default.svc.cluster.local | tail -2"
check_out "p1 resolves via coredns (wget probe)" k exec p1 -- sh -c "wget -qO- --timeout=3 https://kubernetes.default 2>&1 | head -c 120 || true"
check "agent host reaches p2 pod directly" x $A ping -c 1 -W 2 "$p2ip"
check "server host reaches p2 pod via vxlan" x $S ping -c 1 -W 2 "$p2ip"
check "vxlan device flannel.1 exists on agent" x $A sh -c "ip -d link show flannel.1 | grep -q vxlan"

section "k3s server timeline (seconds since container boot)"
x $S journalctl -u k3s --no-pager -o short-monotonic | grep -E "Starting k3s|Imported images|Importing images|up and running|Wrote kubeconfig|Node k3s0[12] .*Ready|Started Lightweight" | sed 's/ k3s01 k3s\[[0-9]*\]://' | while read -r l; do log "  $l"; done
x $A journalctl -u k3s-agent --no-pager -o short-monotonic | grep -E "Starting k3s|Imported images|Importing images|Started Lightweight|Node.*Ready|joined" | sed 's/ k3s02 k3s\[[0-9]*\]://' | while read -r l; do log "  agent: $l"; done

section "offline proof"
check "server imported airgap images" sh -c "docker exec $S journalctl -u k3s --no-pager | grep -q 'Imported docker.io/rancher/mirrored-pause'"
check "agent imported airgap images"  sh -c "docker exec $A journalctl -u k3s-agent --no-pager | grep -q 'Imported docker.io/rancher/mirrored-pause'"
check "no successful registry pull on server" sh -c "! docker exec $S journalctl -u k3s --no-pager | grep -qiE 'Pulled image|Successfully pulled'"
check "no successful registry pull on agent"  sh -c "! docker exec $A journalctl -u k3s-agent --no-pager | grep -qiE 'Pulled image|Successfully pulled'"
log "transient pull attempts before import finished: server=$(docker exec $S journalctl -u k3s --no-pager | grep -ci 'failed to pull image') agent=$(docker exec $A journalctl -u k3s-agent --no-pager | grep -ci 'failed to pull image')"
docker exec $S journalctl -u k3s --no-pager -o short-monotonic > "$RESULTS_DIR/s3-journal-k3s01.log" 2>&1
docker exec $A journalctl -u k3s-agent --no-pager -o short-monotonic > "$RESULTS_DIR/s3-journal-k3s02.log" 2>&1
check_out "images in containerd on agent" x $A sh -c "crictl images -q | wc -l"
check "k3s data dir came pre-extracted from image" x $S sh -c "test -d /var/lib/rancher/k3s/data && ! mountpoint -q /var/lib/rancher/k3s"
check_out "containerd snapshotter in use" x $A sh -c "grep -o 'snapshotter = \"[a-z]*\"' /var/lib/rancher/k3s/agent/etc/containerd/config.toml | head -1"

section "tools"
check "kubectl on server (KUBECONFIG via profile)" x $S bash -lc "kubectl get nodes"
check "crictl ps on agent" x $A crictl ps
check "ctr images on agent" x $A ctr -a /run/k3s/containerd/containerd.sock -n k8s.io images ls
check "helm version" x $S helm version
check "k9s binary runs" x $S k9s version

section "R5 resources (after 30s settle)"
sleep 30
docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} mem={{.MemUsage}}' $S $A | while read -r l; do log "$l"; done
measure server_mem_MB "$(docker stats --no-stream --format '{{.MemUsage}}' $S | awk '{print $1}')"
measure agent_mem_MB "$(docker stats --no-stream --format '{{.MemUsage}}' $A | awk '{print $1}')"
measure host_free_MB "$(free -m | awk '/Mem:/{print $7}')"

finish
