#!/usr/bin/env bash
set -euo pipefail

ip -br link show "$NSL_IFACE" | grep -qw UP
