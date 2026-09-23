#!/usr/bin/env bash
set -euo pipefail

[ -L /etc/resolv.conf ]
[ "$(readlink -f /etc/resolv.conf)" = "/run/systemd/resolve/stub-resolv.conf" ]
