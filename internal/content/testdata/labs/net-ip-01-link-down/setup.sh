#!/usr/bin/env bash
set -euo pipefail

case "$NSL_NODE" in
web01)
	ip link set "$NSL_IFACE" down
	;;
*)
	exit 0
	;;
esac
