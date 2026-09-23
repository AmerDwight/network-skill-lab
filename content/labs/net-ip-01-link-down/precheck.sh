#!/usr/bin/env bash
set -euo pipefail

fail() {
	echo "precheck on $NSL_NODE: $*" >&2
	exit 1
}

if ! ip link show "$NSL_IFACE" >/dev/null 2>&1; then
	fail "$NSL_IFACE does not exist"
fi

if [ "${NSL_SUBNET#*/}" != "24" ]; then
	fail "subnet $NSL_SUBNET is not a /24"
fi

network="${NSL_SUBNET%/*}"
for address in "$NSL_IP_A" "$NSL_IP_B"; do
	if [ "${address%.*}" != "${network%.*}" ]; then
		fail "$address is outside $NSL_SUBNET"
	fi
done

if [ "$NSL_IP_A" = "$NSL_IP_B" ]; then
	fail "both nodes were given $NSL_IP_A"
fi

if [ "$NSL_NODE" != "$NSL_NODE_A" ]; then
	exit 0
fi

if ip -br link show "$NSL_IFACE" | grep -qw UP; then
	fail "$NSL_IFACE is still up, the fault was not applied"
fi

if [ "$NSL_FAULT" = "mtu" ]; then
	mtu=$(cat "/sys/class/net/$NSL_IFACE/mtu")
	if [ "$mtu" != "$NSL_FAULT_MTU" ]; then
		fail "$NSL_IFACE has mtu $mtu, want $NSL_FAULT_MTU"
	fi
fi
