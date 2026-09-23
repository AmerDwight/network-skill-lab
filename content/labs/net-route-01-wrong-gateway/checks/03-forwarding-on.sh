#!/usr/bin/env bash
set -euo pipefail

[ "$(cat /proc/sys/net/ipv4/ip_forward)" = "1" ]
