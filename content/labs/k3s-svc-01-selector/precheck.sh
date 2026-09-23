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

pod_field() {
	kubectl -n "$NSL_NAMESPACE" get pods -l "app=$NSL_APP" -o "jsonpath={.items[0].$1}" 2>/dev/null || true
}

phase=""
node=""
deadline=$((SECONDS + 24))
while [ "$SECONDS" -lt "$deadline" ]; do
	phase=$(pod_field status.phase)
	node=$(pod_field spec.nodeName)
	if [ "$phase" = Running ] && [ "$node" = k3s02 ]; then
		break
	fi
	sleep 2
done

if [ "$phase" != Running ]; then
	fail "the pod of $NSL_APP is ${phase:-missing}, want Running"
fi
if [ "$node" != k3s02 ]; then
	fail "the pod of $NSL_APP runs on ${node:-no node}, want k3s02"
fi

ready=$(kubectl -n "$NSL_NAMESPACE" get endpointslices -l "kubernetes.io/service-name=$NSL_APP" -o jsonpath='{.items[*].endpoints[*].conditions.ready}')
if [ "${NSL_SVC_TARGET_OFF:-}" = "1" ]; then
	case "$ready" in
	*true*) ;;
	*) fail "service $NSL_APP has no ready endpoint, this case needs a matching selector" ;;
	esac
	target=$(kubectl -n "$NSL_NAMESPACE" get svc "$NSL_APP" -o jsonpath='{.spec.ports[0].targetPort}')
	if [ "$target" = "$NSL_PORT" ]; then
		fail "service $NSL_APP already targets port $NSL_PORT, the fault was not applied"
	fi
else
	case "$ready" in
	*true*) fail "service $NSL_APP already has a ready endpoint, the fault was not applied" ;;
	esac
fi
