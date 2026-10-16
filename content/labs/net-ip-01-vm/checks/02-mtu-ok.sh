#!/usr/bin/env bash
set -euo pipefail

[ "$(cat "/sys/class/net/$NSL_IFACE/mtu")" = "1500" ]
