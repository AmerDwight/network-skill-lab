#!/usr/bin/env bash
set -euo pipefail

[ "$(ip -4 route show "$NSL_SUBNET_B" | awk '$2 == "via" { print $3; exit }')" = "$NSL_IP_GW_A" ]
