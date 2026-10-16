# Solution: the interface was shut down (and sometimes its MTU was changed too)

This lab has two variants. The troubleshooting path is the same; the second one hides one extra fault.

## 1. Read the interface states

On `web01`:

```
ip -br link
```

`eth0` is the management network, leave it alone. The data interface `eth1` is `DOWN`, which means it was administratively shut.

Not seeing an address at this point is expected: networkd flushes addresses when a link loses carrier and reapplies the netplan configuration once it comes back up. You do not need to `ip addr add` anything yourself.

## 2. Bring the interface up

```
ip link set eth1 up
ip -br link
ip -br addr show eth1
```

`eth1` should now read `UP` and the address comes back on its own.

## 3. Check the MTU

Bringing the link up is not always the whole fix. Look at the full output again:

```
ip link show eth1
```

- **Common variant:** `mtu 1500`, so this checkpoint passes right away.
- **Other variant:** `mtu 1200`. Someone lowered the MTU: small packets get through while large ones are dropped, which looks a lot like a flaky network.

If the MTU is wrong, put back the Ethernet default:

```
ip link set eth1 mtu 1500
```

To prove large packets survive, send a full-size frame with the do-not-fragment bit (1500 − 20 bytes of IP header − 8 bytes of ICMP header = 1472):

```
ping -M do -s 1472 -c 2 <peer address>
```

## 4. Verify

```
ping -c 3 <peer address>
```

Three replies and 0% packet loss means you are done. The peer address is in the ticket; you can also derive it from `ip -br addr show eth1`, since both nodes share the same /24.

## Why netplan is not involved

The fault was applied straight to the kernel with `ip link set`; the netplan files were never touched. `ip link set ... up` and `ip link set ... mtu 1500` are enough. You can run `netplan get` or `networkctl status eth1` to confirm the configuration is intact, but no `netplan apply` is needed.
