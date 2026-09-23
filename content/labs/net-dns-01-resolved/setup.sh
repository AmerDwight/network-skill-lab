#!/usr/bin/env bash
set -euo pipefail

install_responder() {
	cat >/usr/local/sbin/nsl-dns.py <<'PY'
import os
import socket
import struct

NAME = (os.environ["NSL_HOST"] + "." + os.environ["NSL_DOMAIN"]).lower()
TARGET = socket.inet_aton(os.environ["NSL_IP_TARGET"])
TTL = int(os.environ["NSL_TTL"])
BIND = os.environ["NSL_IP_DNS"]

TYPE_A = 1
CLASS_IN = 1


def question(data):
    offset = 12
    labels = []
    while offset < len(data):
        length = data[offset]
        offset += 1
        if length == 0:
            if offset + 4 > len(data):
                return None, 0, 0, 0
            qtype, qclass = struct.unpack("!HH", data[offset:offset + 4])
            return ".".join(labels).lower(), qtype, qclass, offset + 4
        if length & 0xC0 or offset + length > len(data):
            return None, 0, 0, 0
        labels.append(data[offset:offset + length].decode("ascii", "replace"))
        offset += length
    return None, 0, 0, 0


def reply(data):
    if len(data) < 17:
        return None
    flags = struct.unpack("!H", data[2:4])[0]
    if flags & 0x8000:
        return None
    name, qtype, qclass, end = question(data)
    if name is None:
        return None

    asked = data[12:end]
    echoed = flags & 0x0100
    if name != NAME or qclass != CLASS_IN:
        header = struct.pack("!HHHHH", 0x8483 | echoed, 1, 0, 0, 0)
        return data[:2] + header + asked
    if qtype != TYPE_A:
        header = struct.pack("!HHHHH", 0x8480 | echoed, 1, 0, 0, 0)
        return data[:2] + header + asked
    header = struct.pack("!HHHHH", 0x8480 | echoed, 1, 1, 0, 0)
    answer = struct.pack("!HHHIH", 0xC00C, TYPE_A, CLASS_IN, TTL, 4) + TARGET
    return data[:2] + header + asked + answer


def main():
    sock = socket.socket(socket.AF_INET, socket.SOCK_DGRAM)
    sock.bind((BIND, 53))
    while True:
        data, peer = sock.recvfrom(512)
        try:
            answer = reply(data)
        except (IndexError, struct.error, UnicodeDecodeError):
            continue
        if answer is not None:
            sock.sendto(answer, peer)


main()
PY
	chmod 0755 /usr/local/sbin/nsl-dns.py
	systemd-run --unit nsl-dns --description "nsl lab authoritative responder" \
		--setenv=NSL_IP_DNS="$NSL_IP_DNS" \
		--setenv=NSL_IP_TARGET="$NSL_IP_TARGET" \
		--setenv=NSL_HOST="$NSL_HOST" \
		--setenv=NSL_DOMAIN="$NSL_DOMAIN" \
		--setenv=NSL_TTL="$NSL_TTL" \
		/usr/bin/python3 /usr/local/sbin/nsl-dns.py

	for _ in $(seq 1 30); do
		if [ "$(dig +time=1 +tries=1 +short "@$NSL_IP_DNS" "$NSL_HOST.$NSL_DOMAIN" A)" = "$NSL_IP_TARGET" ]; then
			return 0
		fi
		sleep 0.5
	done
	echo "the responder on $NSL_NODE never answered for $NSL_HOST.$NSL_DOMAIN" >&2
	return 1
}

break_resolver() {
	cat >/etc/netplan/60-nsl-dns.yaml <<EOF
network:
  version: 2
  ethernets:
    eth1:
      nameservers:
        addresses: [$NSL_WRONG_DNS]
        search: [$NSL_DOMAIN]
EOF
	chmod 0600 /etc/netplan/60-nsl-dns.yaml
	netplan apply

	if [ "$NSL_BREAK_STUB" = "1" ]; then
		rm -f /etc/resolv.conf
		printf 'nameserver %s\nsearch %s\n' "$NSL_WRONG_DNS" "$NSL_DOMAIN" >/etc/resolv.conf
	fi

	for _ in $(seq 1 30); do
		if resolvectl dns eth1 | grep -Fqw "$NSL_WRONG_DNS"; then
			return 0
		fi
		sleep 0.5
	done
	echo "eth1 on $NSL_NODE never picked up the nameserver $NSL_WRONG_DNS" >&2
	return 1
}

case "$NSL_NODE" in
	dns01) install_responder ;;
	web01) break_resolver ;;
esac
