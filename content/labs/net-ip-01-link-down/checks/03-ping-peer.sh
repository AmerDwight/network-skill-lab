#!/usr/bin/env bash
set -euo pipefail

ping -c 1 -W 1 "$NSL_IP_B" >/dev/null
