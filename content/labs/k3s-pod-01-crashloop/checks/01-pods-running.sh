#!/usr/bin/env bash
set -euo pipefail

[ "$(kubectl -n "$NSL_NAMESPACE" get deploy "$NSL_APP" -o jsonpath='{.status.availableReplicas}')" = "$NSL_REPLICAS" ]
