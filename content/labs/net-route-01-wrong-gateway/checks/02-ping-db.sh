#!/usr/bin/env bash
set -euo pipefail

ping -c 3 -i 0.5 -W 1 "$NSL_IP_DB" >/dev/null
