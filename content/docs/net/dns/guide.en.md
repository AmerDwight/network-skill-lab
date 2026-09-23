# DNS and resolved on Ubuntu 24.04

Name resolution on Ubuntu 24.04 is handled by **systemd-resolved**. Applications see the stub at `127.0.0.53`; the real server list, the cache and the routing rules all live inside resolved. `/etc/resolv.conf` is the doorway to it, not a configuration file.

## Read the current state

```
resolvectl status              # servers, domains and DNSSEC mode, global and per link
resolvectl status eth1         # one interface only
resolvectl dns                 # servers only
resolvectl dns eth1
resolvectl domain eth1         # search / routing domains
```

`resolvectl status` prints two layers:

```
Global
       Protocols: -LLMNR -mDNS ...
    DNS Servers: 127.0.0.11

Link 2 (eth1)
    DNS Servers: 10.4.7.53
     DNS Domain: lab.internal
```

Global is the fallback used when no link claims the name; the Link block is what that interface was configured with. **When troubleshooting, first work out which link should answer the name.**

## Querying: three levels, three tools

| Command | Path it takes | What it proves |
|---|---|---|
| `dig @<server> <name>` | A UDP query straight to the named server | Whether the server has the answer |
| `resolvectl query <name>` | Asks resolved | Whether resolved's routing and cache are right |
| `getent hosts <name>` | glibc NSS (`/etc/nsswitch.conf`) | What an application actually gets |

Each layer fails differently, so learn all three:

```
dig @10.4.7.53 app.lab.internal A +short
dig @10.4.7.53 app.lab.internal ANY
resolvectl query app.lab.internal
getent hosts app.lab.internal
```

Useful `dig` flags: `+short` prints answers only, `+time=2 +tries=1` fails fast, `+trace` walks down from the root, `-x` does reverse lookups.

## The stub and the three modes of /etc/resolv.conf

```
ls -l /etc/resolv.conf
cat /etc/resolv.conf
```

resolved maintains several files, and what `/etc/resolv.conf` points at decides how the whole machine behaves:

| Target | Contents | Effect |
|---|---|---|
| `/run/systemd/resolve/stub-resolv.conf` | `nameserver 127.0.0.53` | **The default.** Queries enter resolved, so per-link DNS, caching and search domains all work |
| `/run/systemd/resolve/resolv.conf` | The upstream server addresses | Bypasses resolved and queries upstream directly; per-link routing and caching are lost |
| A plain file (not a symlink) | A hand-written nameserver | Completely detached from resolved, so `resolvectl` changes have no effect |

The third one is the hardest to spot: `resolvectl query` succeeds while `getent hosts` fails. Put the default back with:

```
ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
```

## netplan nameservers

Per-link DNS is written by netplan and handed to networkd. Do not edit `/etc/systemd/network/` directly.

```yaml
network:
  version: 2
  ethernets:
    eth1:
      nameservers:
        addresses: [10.4.7.53, 10.4.7.54]
        search: [lab.internal, corp.test]
```

```
netplan get ethernets.eth1     # the merged configuration as it stands
netplan apply                  # apply it
resolvectl status eth1         # confirm resolved picked it up
```

Files under `/etc/netplan/` are merged in filename order and a later file wins for the same key. `chmod 0600` is required or netplan warns about permissions.

### Search domains and routing domains

Entries in `search:` do two jobs: they complete short names, and they **route queries for that domain to this link's servers**. `resolvectl domain eth1` lists them; a leading `~` marks a routing-only domain that is not used for completion.

To add a routing-only domain by hand:

```
resolvectl domain eth1 lab.internal '~corp.test'
```

(Settings made straight through `resolvectl` are overwritten by the next `netplan apply`, so permanent configuration belongs in netplan.)

## Global DNS versus per-link DNS

resolved decides where to send a query like this:

1. If some link has a domain that matches the queried name, use that link's servers.
2. If none matches, use the global servers plus every link with no domain restriction.

So "internal domains to the internal resolver, everything else to the public one" needs no split-horizon setup: put the internal domain in that interface's `search`. Global servers are configured in:

```
/etc/systemd/resolved.conf
/etc/systemd/resolved.conf.d/*.conf      # prefer a drop-in
systemctl restart systemd-resolved
```

## Caching

```
resolvectl statistics          # hit rate and current cache size
resolvectl flush-caches        # clear it, negative entries included
```

Always flush after changing server settings before you retest, or you are likely still reading a negative answer left over from the previous server — NXDOMAIN is cached too.

## Common failure patterns

| Symptom | Usually means |
|---|---|
| `dig @server` answers, `resolvectl query` does not | The link's DNS or domain is wrong, so the query goes to a different server |
| `resolvectl query` answers, `getent hosts` does not | `/etc/resolv.conf` is not the stub symlink, or `hosts:` in `nsswitch.conf` was changed |
| Short names fail, FQDNs work | The `search` domain is missing or wrong |
| The configuration is correct but nothing resolves | Stale negative cache; run `resolvectl flush-caches` |
| Every query hangs for seconds before failing | The server address points at a host where nothing is listening and resolved waits for the timeout |
| `ping` works but names do not | Layer 3 is fine, the fault is entirely on the resolution path |

Prove the query actually leaves the node:

```
tcpdump -ni eth1 port 53
resolvectl log-level debug
journalctl -u systemd-resolved -f
```
