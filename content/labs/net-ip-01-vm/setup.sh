#!/usr/bin/env bash
set -euo pipefail

if [ "$NSL_NODE" != "$NSL_NODE_A" ]; then
	exit 0
fi

if [ "$NSL_FAULT" = "mtu" ]; then
	ip link set "$NSL_IFACE" mtu "$NSL_FAULT_MTU"
fi

ip link set "$NSL_IFACE" down
