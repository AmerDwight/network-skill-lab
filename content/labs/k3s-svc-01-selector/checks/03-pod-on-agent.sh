#!/usr/bin/env bash
set -euo pipefail

[ "$(kubectl -n "$NSL_NAMESPACE" get pods -l "app=$NSL_APP" -o jsonpath='{.items[0].spec.nodeName}')" = k3s02 ]
