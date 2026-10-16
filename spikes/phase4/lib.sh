D=/var/tmp/nsl-spike
V="virsh --connect qemu:///system"
RES="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/results"

domain_xml() {
  local name=$1 disk=$2 seed=$3 mem=$4 vcpu=$5; shift 5
  cat <<XML
<domain type='kvm'>
  <name>$name</name>
  <memory unit='MiB'>$mem</memory>
  <vcpu>$vcpu</vcpu>
  <os><type arch='x86_64' machine='q35'>hvm</type><boot dev='hd'/></os>
  <features><acpi/><apic/></features>
  <cpu mode='host-passthrough'/>
  <clock offset='utc'/>
  <on_poweroff>destroy</on_poweroff>
  <on_reboot>restart</on_reboot>
  <devices>
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2' discard='unmap'/>
      <source file='$disk'/>
      <target dev='vda' bus='virtio'/>
    </disk>
    <disk type='file' device='cdrom'>
      <driver name='qemu' type='raw'/>
      <source file='$seed'/>
      <target dev='sda' bus='sata'/>
      <readonly/>
    </disk>
$(for net in "$@"; do printf "    <interface type='network'><source network='%s'/><model type='virtio'/></interface>\n" "$net"; done)
    <channel type='unix'>
      <target type='virtio' name='org.qemu.guest_agent.0'/>
    </channel>
    <serial type='file'><source path='$D/$name-serial.log' append='off'/><target port='0'/></serial>
    <console type='file'><source path='$D/$name-serial.log' append='off'/><target type='serial' port='0'/></console>
    <rng model='virtio'><backend model='random'>/dev/urandom</backend></rng>
    <memballoon model='none'/>
  </devices>
</domain>
XML
}

seed_iso() {
  local name=$1 out=$2 extra_user_data=${3:-} netcfg=${4:-}
  local ud="$D/$name-user-data" md="$D/$name-meta-data" nc="$D/$name-network-config"
  cat > "$ud" <<UD
#cloud-config
hostname: $name
ssh_pwauth: false
users:
  - name: nsl
    sudo: ALL=(ALL) NOPASSWD:ALL
    shell: /bin/bash
    ssh_authorized_keys:
      - $(cat "$D/spike-key.pub")
$extra_user_data
UD
  printf 'instance-id: %s-%s\nlocal-hostname: %s\n' "$name" "$(date +%s)" "$name" > "$md"
  if [ -n "$netcfg" ]; then
    printf '%s\n' "$netcfg" > "$nc"
    cloud-localds -N "$nc" "$out" "$ud" "$md"
  else
    cloud-localds "$out" "$ud" "$md"
  fi
}

destroy_domain() {
  $V destroy "$1" >/dev/null 2>&1 || true
  $V undefine "$1" >/dev/null 2>&1 || true
}

agent_ping() { $V qemu-agent-command "$1" '{"execute":"guest-ping"}' --timeout 2 >/dev/null 2>&1; }

dom_ip() { $V domifaddr "$1" --source lease 2>/dev/null | awk '/ipv4/{split($4,a,"/"); print a[1]; exit}'; }

SSH="ssh -i $D/spike-key -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=2 -o LogLevel=ERROR"
gexec() {
  local dom=$1; shift
  local cmd="$*"
  local pid
  pid=$($V qemu-agent-command "$dom" "$(python3 -c 'import json,sys; print(json.dumps({"execute":"guest-exec","arguments":{"path":"/bin/sh","arg":["-c",sys.argv[1]],"capture-output":True}}))' "$cmd")" \
    | python3 -c 'import json,sys; print(json.load(sys.stdin)["return"]["pid"])')
  local st
  while :; do
    st=$($V qemu-agent-command "$dom" "{\"execute\":\"guest-exec-status\",\"arguments\":{\"pid\":$pid}}")
    python3 -c 'import json,sys; sys.exit(0 if json.load(sys.stdin)["return"]["exited"] else 1)' <<<"$st" && break
    sleep 0.2
  done
  python3 -c '
import json,sys,base64
r=json.load(sys.stdin)["return"]
sys.stdout.write(base64.b64decode(r.get("out-data","")).decode())
sys.stderr.write(base64.b64decode(r.get("err-data","")).decode())
sys.exit(r["exitcode"])' <<<"$st"
}
