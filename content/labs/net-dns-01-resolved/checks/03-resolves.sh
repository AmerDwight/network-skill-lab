#!/usr/bin/env bash
set -euo pipefail

[ "$(timeout 5 getent hosts "$NSL_HOST.$NSL_DOMAIN" | awk '{ print $1; exit }')" = "$NSL_IP_TARGET" ]
