# Playbook: two machines cannot reach each other

Work upwards from the physical layer. Each step asks one question and runs one command. Good answer, move on; bad answer, apply that step's fix and go back to step 5 to retest.

Write down two facts before you start: the local egress interface (say `eth1`) and the peer address (say `10.0.5.20`).

## 1. Link: is the interface alive?

```
ip -br link
```

- **Good:** the interface reads `UP`.
- **Bad:** `DOWN` (administratively shut) or `LOWERLAYERDOWN` (no carrier).
- **Fix:** for `DOWN`, run `ip link set eth1 up`. `LOWERLAYERDOWN` is a cable or peer problem — bringing it up locally changes nothing, go look at the other end.

`eth0` is usually the management interface. Leave it alone.

## 2. Address: is there one, and is it right?

```
ip -br addr show eth1
```

- **Good:** one address whose prefix puts the peer on the same subnet, e.g. `10.0.5.10/24` facing `10.0.5.20`.
- **Bad:** no address at all, or a wrong prefix (`/32` or `/25` pushes the peer into a different subnet).
- **Fix:** right after a link comes up the address returns by itself — networkd flushes addresses on carrier loss and reapplies them on carrier up — so wait a couple of seconds and look again. If it is genuinely missing, compare against the config with `netplan get ethernets.eth1`, edit the file and `netplan apply`. For a throwaway test, `ip addr add 10.0.5.10/24 dev eth1`.

## 3. Route: does the packet know where to go?

```
ip route get 10.0.5.20
```

- **Good:** `10.0.5.20 dev eth1 src 10.0.5.10` (same subnet, straight out of the interface) or `via <gateway> dev <iface>`.
- **Bad:** `Network is unreachable`, or an egress interface you did not expect.
- **Fix:** same-subnet with no route almost always means the address or prefix from step 2 is wrong. For a missing default route, `ip route add default via <gateway>`, and put it in netplan for the permanent fix.

## 4. Neighbour: did we learn the peer's MAC?

```
ip neigh
ip neigh show 10.0.5.20
```

- **Good:** `10.0.5.20 dev eth1 lladdr 02:42:0a:00:05:14 REACHABLE` (`STALE` is fine too).
- **Bad:** `FAILED`, or no entry at all.
- **Fix:** `FAILED` means ARP asked and nobody answered — the peer is down, its interface is down, or the two of you are not on the same broadcast domain after all. Confirm with `arping -I eth1 10.0.5.20`, then run steps 1 and 2 on the peer. Clear stale state before retrying: `ip neigh flush dev eth1`.

## 5. Connectivity: does it actually work?

```
ping -c 3 10.0.5.20
```

- **Good:** three replies, `0% packet loss`.
- **Bad:** `Destination Host Unreachable` (ARP failed, back to step 4) or timeouts (replies are being dropped or the peer stays silent, continue to steps 6 and 7).
- **Fix:** follow whichever step the symptom points at.

If small packets now work but users still report hangs, keep going.

## 6. MTU: do large packets get through?

```
ping -M do -s 1472 -c 2 10.0.5.20
```

`-M do` forbids fragmentation and `-s 1472` fills a 1500-byte MTU (1500 − 20 IP − 8 ICMP).

- **Good:** two replies.
- **Bad:** `Frag needed and DF set (mtu = 1200)`, or silence here while a plain `-s 56` ping works.
- **Fix:** check the local MTU with `ip link show eth1` and restore it with `ip link set eth1 mtu 1500`. If the local value is fine, bisect the path: try `-s 1372`, `-s 1272` and so on; the first size that gets through plus 28 is the path MTU.

## 7. Firewall: is something dropping it?

```
nft list ruleset
iptables -S
```

- **Good:** `nft list ruleset` prints nothing, or every chain has policy `accept` and no drop rules.
- **Bad:** a `policy drop`, or a `drop` / `reject` rule matching the peer address or ICMP.
- **Fix:** find the rule that is doing it — rerunning the test with counters is the most reliable way:

  ```
  nft list ruleset -a          # prints each rule's handle
  nft delete rule inet filter input handle 7
  ```

  On an iptables system use `iptables -D` on the matching rule. Check both ends: the sender's `output` chain and the peer's `input` chain. ufw is a front end to the same nftables, so `ufw status verbose` gives a friendlier view.

## Cheat sheet

| Layer | Command | What broken looks like |
|---|---|---|
| link | `ip -br link` | `DOWN` / `LOWERLAYERDOWN` |
| address | `ip -br addr show eth1` | no address, wrong prefix |
| route | `ip route get <peer>` | `Network is unreachable` |
| neighbour | `ip neigh show <peer>` | `FAILED` / no entry |
| ping | `ping -c 3 <peer>` | timeouts, `Host Unreachable` |
| MTU | `ping -M do -s 1472 <peer>` | `Frag needed and DF set` |
| firewall | `nft list ruleset` | `policy drop`, drop rules |

The first five steps only read state (apart from `neigh flush`), so it is safe to walk the whole list once before you change anything.
