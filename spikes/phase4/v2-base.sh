#!/usr/bin/env bash
# Builds base.qcow2: Ubuntu 24.04 cloud image + qemu-guest-agent, then powers off.
set -euo pipefail
. "$(dirname "$0")/lib.sh"
cd "$D"
[ -f spike-key ] || ssh-keygen -t ed25519 -N '' -f spike-key -q
destroy_domain nsl-base
rm -f base.qcow2 base-seed.iso
qemu-img create -q -f qcow2 -b noble-server-cloudimg-amd64.img -F qcow2 base.qcow2 8G
seed_iso nsl-base base-seed.iso "$(cat <<'UD'
package_update: true
packages: [qemu-guest-agent]
runcmd:
  - systemctl enable --now qemu-guest-agent
  - cloud-init clean --logs
  - truncate -s 0 /etc/machine-id
  - rm -f /var/lib/dbus/machine-id
power_state: {mode: poweroff, timeout: 60, condition: true}
UD
)"
domain_xml nsl-base "$D/base.qcow2" "$D/base-seed.iso" 1024 2 default > nsl-base.xml
$V define nsl-base.xml >/dev/null
start=$(date +%s)
$V start nsl-base >/dev/null
while [ "$($V domstate nsl-base)" != "shut off" ]; do sleep 3; done
echo "base build: $(( $(date +%s) - start ))s"
$V undefine nsl-base >/dev/null
qemu-img info base.qcow2 | grep -E 'disk size|backing file:'
