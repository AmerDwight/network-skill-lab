#!/usr/bin/env bash
# V1: KVM and libvirt capability check on the runner host.
set -uo pipefail
R="$(dirname "$0")/results/v1.txt"; : > "$R"
log() { printf '%s\n' "$*" | tee -a "$R"; }
log "kernel $(uname -r)"
log "/dev/kvm: $(ls -la /dev/kvm 2>&1)"
log "kvm rw: $(sg kvm -c 'test -r /dev/kvm -a -w /dev/kvm && echo yes || echo no')"
log "libvirtd: $(systemctl is-active libvirtd)"
log "virsh version: $(sg libvirt -c 'virsh --connect qemu:///system version' | grep -E 'Running hypervisor' )"
log "kvm domain type: $(sg libvirt -c 'virsh --connect qemu:///system capabilities' | grep -c "domain type='kvm'")"
log "host cpu: $(sg libvirt -c 'virsh --connect qemu:///system capabilities' | grep -o '<model>[^<]*</model>' | head -1)"
log "networks: $(sg libvirt -c 'virsh --connect qemu:///system net-list --all' | tail -n +3 | awk '{print $1":"$2}' | tr '\n' ' ')"
log "virt-host-validate:"; sg libvirt -c 'virt-host-validate qemu' 2>&1 | sed 's/^/  /' | tee -a "$R" | tail -12
