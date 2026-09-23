#!/usr/bin/env bash
set -euo pipefail

kubectl -n "$NSL_NAMESPACE" get endpointslices -l "kubernetes.io/service-name=$NSL_APP" \
	-o jsonpath='{.items[*].endpoints[*].conditions.ready}' | grep -qw true
