#!/usr/bin/env bash
# Builds the nsl VM node image: Ubuntu 24.04 cloud image + the nsl/node package set,
# rootfs and qemu-guest-agent, flattened into a standalone qcow2.
set -euo pipefail

OUT_DIR="${NSL_VM_IMAGE_DIR:-$(cd "$(dirname "$0")" && pwd)/out}"
CLOUD_IMAGE_URL="${CLOUD_IMAGE_URL:-https://cloud-images.ubuntu.com/noble/current/noble-server-cloudimg-amd64.img}"
DISK_SIZE="${DISK_SIZE:-10G}"
BUILD_MEM_MIB="${BUILD_MEM_MIB:-2048}"
NETWORK="${NETWORK:-default}"
WITH_K3S="${WITH_K3S:-0}"
K3S_VERSION="${K3S_VERSION:-v1.36.4+k3s1}"
VIRSH="virsh --connect qemu:///system"
ROOTFS="$(cd "$(dirname "$0")/../node/rootfs" && pwd)"
DOMAIN="nsl-vm-build-$$"

for tool in qemu-img genisoimage curl virsh; do
  command -v "$tool" >/dev/null || { echo "missing tool: $tool" >&2; exit 1; }
done

mkdir -p "$OUT_DIR"
cd "$OUT_DIR"
chmod o+rx "$OUT_DIR"

base="$(basename "$CLOUD_IMAGE_URL")"
if [ ! -f "$base" ]; then
  curl -fsSL -o "$base.part" "$CLOUD_IMAGE_URL"
  mv -f "$base.part" "$base"
fi

rm -rf seed && mkdir seed
tar -C "$ROOTFS" --owner=0 --group=0 --numeric-owner -czf seed/rootfs.tar.gz .
printf 'instance-id: nsl-vm-build\nlocal-hostname: nsl-build\n' > seed/meta-data
cat > seed/user-data <<UD
#cloud-config
bootcmd:
  - echo 'wireshark-common wireshark-common/install-setuid boolean false' | debconf-set-selections
package_update: true
packages:
  - qemu-guest-agent
  - locales
  - iproute2
  - net-tools
  - ethtool
  - iputils-ping
  - iputils-arping
  - iputils-tracepath
  - traceroute
  - mtr-tiny
  - netcat-openbsd
  - socat
  - iperf3
  - telnet
  - nmap
  - arp-scan
  - bind9-dnsutils
  - tcpdump
  - tshark
  - iftop
  - conntrack
  - iptables
  - nftables
  - ufw
  - netplan.io
  - network-manager
  - isc-dhcp-client
  - openssh-server
  - chrony
  - nginx
  - rsyslog
  - snmp
  - snmpd
  - snmp-mibs-downloader
  - lldpd
  - snmptrapd
  - curl
  - wget
  - openssl
  - ca-certificates
  - rsync
  - procps
  - psmisc
  - lsof
  - strace
  - htop
  - sysstat
  - util-linux
  - vim
  - nano
  - less
  - jq
  - tmux
  - tree
  - git
  - bash-completion
  - man-db
  - file
  - unzip
  - sudo
  - cron
  - zstd
runcmd:
  - mount -o ro /dev/disk/by-label/cidata /mnt
  - tar -C / -xzf /mnt/rootfs.tar.gz
  - umount /mnt
  - mkdir -p /var/log/nsl /etc/rancher/k3s /etc/nsl
  - chmod 1777 /var/log/nsl
  - touch /var/log/nsl/commands.jsonl
  - chmod 0666 /var/log/nsl/commands.jsonl
  - id nsl >/dev/null 2>&1 || useradd -m -s /bin/bash -G sudo nsl
  - grep -q nsl-history /etc/bash.bashrc || echo '. /etc/profile.d/nsl-history.sh' >> /etc/bash.bashrc
  - systemctl enable systemd-networkd systemd-resolved ssh rsyslog cron chrony qemu-guest-agent
  - systemctl disable NetworkManager nginx snmpd lldpd snmptrapd ufw
  - printf 'GRUB_CMDLINE_LINUX="net.ifnames=0 biosdevname=0"\n' > /etc/default/grub.d/90-nsl.cfg
  - update-grub
$( [ "$WITH_K3S" = 1 ] && printf '  - "%s"\n' "curl -fsSL -o /usr/local/bin/k3s https://github.com/k3s-io/k3s/releases/download/${K3S_VERSION}/k3s && chmod 0755 /usr/local/bin/k3s && for t in kubectl crictl ctr; do ln -sf k3s /usr/local/bin/\$t; done" )
  - apt-get clean
  - rm -rf /var/lib/apt/lists/*
  - cloud-init clean --logs
  - truncate -s 0 /etc/machine-id
  - rm -f /var/lib/dbus/machine-id
power_state:
  mode: poweroff
  timeout: 120
  condition: true
UD
genisoimage -quiet -output seed.iso -volid cidata -joliet -rock seed/user-data seed/meta-data seed/rootfs.tar.gz

rm -f work.qcow2
qemu-img create -q -f qcow2 -b "$base" -F qcow2 work.qcow2 "$DISK_SIZE"

cat > domain.xml <<XML
<domain type='kvm'>
  <name>$DOMAIN</name>
  <memory unit='MiB'>$BUILD_MEM_MIB</memory>
  <vcpu>2</vcpu>
  <os><type arch='x86_64' machine='q35'>hvm</type><boot dev='hd'/></os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough'/>
  <on_poweroff>destroy</on_poweroff>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='$OUT_DIR/work.qcow2'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='$OUT_DIR/seed.iso'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
    <interface type='network'><source network='$NETWORK'/><model type='virtio'/></interface>
    <serial type='file'><source path='$OUT_DIR/build-serial.log' append='off'/><target port='0'/></serial>
    <rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
    <memballoon model='none'/>
  </devices>
</domain>
XML

start=$(date +%s)
$VIRSH create domain.xml >/dev/null
trap '$VIRSH destroy "$DOMAIN" >/dev/null 2>&1 || true' EXIT
while $VIRSH domstate "$DOMAIN" >/dev/null 2>&1; do sleep 5; done
trap - EXIT
echo "provisioning finished in $(( $(date +%s) - start ))s"

qemu-img convert -O qcow2 -c work.qcow2 nsl-node.qcow2.part
mv -f nsl-node.qcow2.part nsl-node.qcow2
rm -f work.qcow2 seed.iso domain.xml
rm -rf seed
qemu-img info nsl-node.qcow2 | grep -E 'virtual size|disk size'
echo "image: $OUT_DIR/nsl-node.qcow2"
