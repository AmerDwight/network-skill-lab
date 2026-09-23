# Interfaces and addresses on Ubuntu 24.04 (netplan + systemd-networkd)

On Ubuntu 24.04 **netplan writes the configuration and systemd-networkd applies it**. The `ip` command shows what the kernel is doing right now; netplan shows what the machine is supposed to look like after boot. When the two disagree, that gap is where the fault is.

## Read the state first

```
ip -br link          # one line per interface: name, state, MAC
ip -br addr          # one line per interface: name, state, addresses
ip -br addr show eth1
```

The `-br` (brief) output looks like this:

```
eth0             UP             172.18.0.2/16
eth1             DOWN
```

Three states are worth telling apart:

| State | Meaning | Usual cause |
|---|---|---|
| `UP` | Administratively up and has carrier | Healthy |
| `DOWN` | Administratively down | Someone ran `ip link set ... down`, or the config never enabled it |
| `LOWERLAYERDOWN` | Administratively up but no carrier | Cable unplugged, peer powered off, virtual peer missing |

"Carrier" means the physical layer has a signal. The kernel exposes it under `/sys`:

```
cat /sys/class/net/eth1/carrier      # 1 = carrier, 0 = none
cat /sys/class/net/eth1/operstate
```

`DOWN` is yours to fix. `LOWERLAYERDOWN` means you have to look at the other end.

## Bring interfaces up and down

```
ip link set eth1 up
ip link set eth1 down
```

**Important:** networkd flushes the addresses of a link that loses carrier. So after `ip link set eth1 down`, `ip addr` shows nothing on it. The configuration is not gone — bring the link back `up` and networkd reapplies the netplan address. Resist the urge to `ip addr add` by hand; that only adds an address no config file knows about.

## MTU

```
ip link show eth1                    # mtu <n> on the first line
cat /sys/class/net/eth1/mtu
ip link set eth1 mtu 1500
```

1500 is the Ethernet default. A lowered MTU has a distinctive symptom: ping and an SSH login work fine, but a large file transfer or a long paste hangs. Prove it with a full-size, do-not-fragment packet:

```
ping -M do -s 1472 -c 2 10.0.5.20
```

1472 = 1500 − 20 (IP header) − 8 (ICMP header). A reply of `Frag needed and DF set (mtu = 1200)` means something on the path shrank the MTU.

## Addresses

```
ip addr add 10.0.5.10/24 dev eth1
ip addr del 10.0.5.10/24 dev eth1
```

Both only touch kernel state and disappear on reboot or on the next `netplan apply`. Fine for a quick test, but a permanent address belongs in netplan.

## Routes

```
ip route                    # the main routing table
ip route get 8.8.8.8        # which route this destination actually takes
ip -br addr show eth0
```

`ip route get` is faster than reading the whole table because it reports the kernel's decision directly:

```
8.8.8.8 via 172.18.0.1 dev eth0 src 172.18.0.2
```

With no default route it answers `Network is unreachable`.

## What networkd thinks

```
networkctl                       # SETUP state per interface
networkctl status eth1           # addresses, routes, which file configured it
journalctl -u systemd-networkd -n 50 --no-pager
```

In the SETUP column, `configured` is healthy. A stuck `configuring` usually means it is waiting for DHCP or for carrier. `unmanaged` means networkd leaves the interface alone — that is deliberately the case for the `eth0` management interface here, so that `netplan apply` never disturbs the default route.

## netplan and where the config lives

Configuration lives in `/etc/netplan/*.yaml` and should be mode `600`:

```
ls -l /etc/netplan/
netplan get                       # the merged, effective configuration
netplan get ethernets.eth1        # just one interface
```

After editing:

```
netplan generate                  # render networkd units without applying
netplan apply                     # render and make networkd reload
```

The rendered units land in `/run/systemd/network/`. Read them when you want to know what netplan actually translated your YAML into.

## Order of investigation

1. `ip -br link` — does the interface exist and what state is it in?
2. `ip -br addr` — is the address and prefix right?
3. `ip route get <destination>` — can traffic leave at all?
4. `networkctl status <iface>` — does networkd consider it configured?
5. `netplan get` — how does the config differ from reality?

The first four only read state and have no side effects. Finish them before you change anything.
