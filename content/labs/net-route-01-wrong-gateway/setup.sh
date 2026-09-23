#!/usr/bin/env bash
set -euo pipefail

case "$NSL_NODE" in
	gw01)
		sysctl -w "net.ipv4.ip_forward=$NSL_GW_FORWARD" >/dev/null
		;;
	db01)
		ip route add "$NSL_SUBNET_A" via "$NSL_IP_GW_B"
		;;
	web01)
		ip route add "$NSL_SUBNET_B" via "$NSL_WRONG_GW"
		;;
esac
