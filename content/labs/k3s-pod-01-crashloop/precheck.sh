#!/usr/bin/env bash
set -euo pipefail

fail() {
	echo "precheck on $NSL_NODE: $*" >&2
	exit 1
}

if [ "$NSL_NODE" != k3s01 ]; then
	exit 0
fi

if ! kubectl get namespace "$NSL_NAMESPACE" >/dev/null 2>&1; then
	fail "namespace $NSL_NAMESPACE was not created"
fi

crashed() {
	local pods restarts count
	pods=$(kubectl -n "$NSL_NAMESPACE" get pods -l "app=$NSL_APP" --no-headers)
	if printf '%s' "$pods" | grep -qE 'CrashLoopBackOff|Error'; then
		return 0
	fi
	restarts=$(kubectl -n "$NSL_NAMESPACE" get pods -l "app=$NSL_APP" -o jsonpath='{.items[*].status.containerStatuses[*].restartCount}')
	for count in $restarts; do
		if [ "$count" -gt 0 ]; then
			return 0
		fi
	done
	return 1
}

deadline=$((SECONDS + 24))
while [ "$SECONDS" -lt "$deadline" ]; do
	if crashed; then
		exit 0
	fi
	sleep 2
done

fail "no pod of $NSL_APP restarted or reached CrashLoopBackOff, the fault was not applied"
