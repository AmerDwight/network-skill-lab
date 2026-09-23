#!/usr/bin/env bash
set -euo pipefail

fail() {
	echo "precheck on $NSL_NODE: $*" >&2
	exit 1
}

carries() {
	ip -4 -o addr show dev "$1" scope global | grep -Fq " $2/24 "
}

gateway_for() {
	ip -4 route show "$1" | awk '$2 == "via" { print $3; exit }'
}

if [ "$NSL_SUBNET_A" = "$NSL_SUBNET_B" ]; then
	fail "both links were drawn on $NSL_SUBNET_A"
fi

case "$NSL_NODE" in
	web01)
		carries eth1 "$NSL_IP_WEB" || fail "eth1 does not carry $NSL_IP_WEB"
		gateway=$(gateway_for "$NSL_SUBNET_B")
		if [ "$gateway" != "$NSL_WRONG_GW" ]; then
			fail "the route to $NSL_SUBNET_B goes via ${gateway:-nothing}, want the faulty $NSL_WRONG_GW"
		fi
		;;
	gw01)
		carries eth1 "$NSL_IP_GW_A" || fail "eth1 does not carry $NSL_IP_GW_A"
		carries eth2 "$NSL_IP_GW_B" || fail "eth2 does not carry $NSL_IP_GW_B"
		forwarding=$(cat /proc/sys/net/ipv4/ip_forward)
		if [ "$forwarding" != "$NSL_GW_FORWARD" ]; then
			fail "ip_forward is $forwarding, want $NSL_GW_FORWARD"
		fi
		;;
	db01)
		carries eth1 "$NSL_IP_DB" || fail "eth1 does not carry $NSL_IP_DB"
		gateway=$(gateway_for "$NSL_SUBNET_A")
		if [ "$gateway" != "$NSL_IP_GW_B" ]; then
			fail "the return route to $NSL_SUBNET_A goes via ${gateway:-nothing}, want $NSL_IP_GW_B"
		fi
		;;
esac
