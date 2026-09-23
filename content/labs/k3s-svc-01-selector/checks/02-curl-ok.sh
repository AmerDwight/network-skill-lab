#!/usr/bin/env bash
set -euo pipefail

cluster_ip=$(kubectl -n "$NSL_NAMESPACE" get svc "$NSL_APP" -o jsonpath='{.spec.clusterIP}')
[ "$(curl -s --max-time 3 "http://$cluster_ip:$NSL_PORT/")" = ok ]
