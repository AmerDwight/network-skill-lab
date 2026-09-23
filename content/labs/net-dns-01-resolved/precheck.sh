#!/usr/bin/env bash
set -euo pipefail

fail() {
	echo "precheck on $NSL_NODE: $*" >&2
	exit 1
}

case "$NSL_NODE" in
	dns01)
		answer=$(dig +time=2 +tries=1 +short "@$NSL_IP_DNS" "$NSL_HOST.$NSL_DOMAIN" A)
		if [ "$answer" != "$NSL_IP_TARGET" ]; then
			fail "the responder answered ${answer:-nothing} for $NSL_HOST.$NSL_DOMAIN, want $NSL_IP_TARGET"
		fi
		;;
	web01)
		resolvectl dns eth1 | grep -Fqw "$NSL_WRONG_DNS" || fail "eth1 does not point at the faulty nameserver $NSL_WRONG_DNS"
		;;
esac
