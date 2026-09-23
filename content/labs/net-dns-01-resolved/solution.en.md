# Solution: eth1 points at the wrong nameserver (and sometimes /etc/resolv.conf was replaced too)

This lab has two variants. In the first one only the netplan nameserver is wrong; the second one also replaces `/etc/resolv.conf` with a plain file.

## 1. Confirm that resolution is broken, not the network

On `web01`:

```
getent hosts <name to resolve>
ping -c 2 <dns01 address>
```

The name does not resolve but the DNS server answers ping, so layer 3 is fine and the fault is somewhere on the resolution path.

## 2. Ask the server directly to clear it of suspicion

`dig @server` bypasses the whole local resolver configuration and queries the server you name:

```
dig @<dns01 address> <name to resolve> A +short
```

An answer proves the server is healthy and the problem is web01's own configuration. This is the single most useful step in DNS troubleshooting.

## 3. Read what resolved is actually using

```
resolvectl status
resolvectl status eth1
resolvectl dns
resolvectl dns eth1
```

`resolvectl status` lists the `DNS Servers` and `DNS Domain` for the global scope and for every link separately. Look at the `Link 2 (eth1)` block: its DNS server is not the dns01 address from the ticket but another address on the same subnet, where nothing is listening.

You can also ask resolved itself:

```
resolvectl query <name to resolve>
```

## 4. Find who wrote that setting and correct it

Per-link DNS comes from netplan by way of networkd:

```
netplan get ethernets.eth1
ls /etc/netplan
```

Point `nameservers.addresses` at dns01 and keep the search domain:

```
network:
  version: 2
  ethernets:
    eth1:
      nameservers:
        addresses: [<dns01 address>]
        search: [<domain>]
```

Apply and confirm:

```
netplan apply
resolvectl dns eth1
resolvectl status eth1
```

The DNS server on `eth1` should now be dns01.

## 5. Resolve again

```
resolvectl flush-caches
getent hosts <name to resolve>
```

- **Common variant:** the name resolves and the lab is done.
- **Other variant:** `dig @<dns01>` answers and so does `resolvectl query`, but `getent hosts` still returns nothing. Read on.

## 6. When getent still fails, look at /etc/resolv.conf

`getent` goes through glibc's NSS. This node has `hosts: files dns` in `/etc/nsswitch.conf`, so it uses the classic resolver, which reads its nameserver from `/etc/resolv.conf`. Normally that is a symlink to the resolved stub:

```
ls -l /etc/resolv.conf
cat /etc/resolv.conf
```

A healthy node looks like this, and the file holds nothing but `nameserver 127.0.0.53`:

```
/etc/resolv.conf -> /run/systemd/resolve/stub-resolv.conf
```

When it has been replaced by a plain file with a hardcoded nameserver, nothing you do with `resolvectl` matters, because glibc never asks resolved at all.

Put the symlink back:

```
ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
ls -l /etc/resolv.conf
```

resolved ships two files and the difference matters:

| File | Contents | When to use it |
|---|---|---|
| `/run/systemd/resolve/stub-resolv.conf` | `nameserver 127.0.0.53` | The default. Queries go to resolved, so per-link DNS, caching and search domains all work |
| `/run/systemd/resolve/resolv.conf` | The upstream server addresses | Only when a program must bypass resolved; per-link routing is lost |

## 7. Verify

```
resolvectl flush-caches
getent hosts <name to resolve>
resolvectl query <name to resolve>
resolvectl status eth1
```

When `getent hosts` returns the address from the ticket, you are done.
