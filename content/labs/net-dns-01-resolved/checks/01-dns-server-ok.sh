#!/usr/bin/env bash
set -euo pipefail

resolvectl dns eth1 | grep -Fqw "$NSL_IP_DNS"
