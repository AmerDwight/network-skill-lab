#!/usr/bin/env bash
set -euo pipefail

kubectl -n "$NSL_NAMESPACE" rollout status "deploy/$NSL_APP" --timeout=5s >/dev/null
