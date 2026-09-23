# Solution: web01 routes through a gateway that does not exist (and sometimes the gateway does not forward)

This lab has two variants. The troubleshooting path is the same; the second one hides one extra fault on the gateway.

## 1. Bound the problem first

On `web01`:

```
ping -c 2 <db01 address>
ping -c 2 <gw01 address on this subnet>
```

The gateway on the same subnet answers and db01 across the gateway does not, so the break is **after the packet leaves this subnet** — not the interface and not the address.

## 2. Read the route web01 actually picks

```
ip -4 route show
ip route get <db01 address>
```

`ip route get` prints the exact entry the kernel will use:

```
10.9.3.10 via 10.4.7.254 dev eth1 src 10.4.7.10
```

Whatever follows `via` is the next hop. Compare it with the gw01 address from the ticket: they differ. The next hop points at a host on this subnet that simply is not there.

Confirm the next hop is dead:

```
ping -c 2 <the via address printed above>
ip neigh show dev eth1
```

`ip neigh` shows that address as `FAILED` or `INCOMPLETE`: ARP gets no answer, so the packets never leave the node.

## 3. Repair the route

Use `replace`: it overwrites an existing route and adds a missing one, so there is no need to `del` first.

```
ip route replace <db01 subnet> via <gw01 address on this subnet>
ip route get <db01 address>
```

The `via` should now be the gw01 address.

## 4. Ping again

```
ping -c 3 <db01 address>
```

- **Common variant:** traffic flows and the lab is done.
- **Other variant:** the route is right and it still does not work. Read on.

## 5. When it still fails, check forwarding on the gateway

A correct route with a healthy next hop that still gets no replies usually means the gateway is not forwarding at all. On `gw01`:

```
sysctl net.ipv4.ip_forward
cat /proc/sys/net/ipv4/ip_forward
```

`0` means the kernel drops every packet that is not addressed to itself. Turn it on:

```
sysctl -w net.ipv4.ip_forward=1
```

To survive a reboot, write it to a config file as well (not needed for this lab, but expected in production):

```
echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-forward.conf
```

While you are there, confirm both of the gateway's addresses are present and that it has a connected route into each subnet:

```
ip -br addr
ip -4 route show
```

## 6. Verify

Back on `web01`:

```
ping -c 3 <db01 address>
traceroute -n <db01 address>
```

The first hop should be `gw01` and the second one `db01`. Three replies with 0% packet loss and you are done.

## Why db01 needs no change

The return route (db01 reaching the web01 subnet through gw01) is already in place when the lab is built. It is still worth a look during troubleshooting, because a missing return route produces almost the same symptom:

```
ip -4 route show <web01 subnet>
```

## Why netplan is not involved

The fault was applied straight to the kernel with `ip route`; the netplan files were never touched. `ip route replace` and `sysctl -w` are enough, and no `netplan apply` is needed.
