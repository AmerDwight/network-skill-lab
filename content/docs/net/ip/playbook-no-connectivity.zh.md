# Playbook：兩台機器之間連不通

從實體層往上一層一層排除。每一步只有一個問題、一個指令；答案是「好」就往下一步，是「壞」就照該步的修法處理，修完回到第 5 步重測。

先把兩個事實寫下來再開始：本機出口介面（例如 `eth1`）、對端位址（例如 `10.0.5.20`）。

## 1. 連線層：介面是不是活的

```
ip -br link
```

- **好**：目標介面顯示 `UP`。
- **壞**：顯示 `DOWN`（行政關閉）或 `LOWERLAYERDOWN`（沒有載波）。
- **修**：`DOWN` 就 `ip link set eth1 up`；`LOWERLAYERDOWN` 是對端或線路問題，本機拉 up 沒用，去看對端。

`eth0` 通常是管理介面，不要動它。

## 2. 位址：有沒有、對不對

```
ip -br addr show eth1
```

- **好**：有一個位址，且遮罩與對端同網段，例如 `10.0.5.10/24` 對 `10.0.5.20`。
- **壞**：沒有位址；或遮罩錯了（`/32`、`/25` 會讓對端落在別的網段）。
- **修**：介面剛拉起來時位址會自己回來（networkd 失去載波會清位址，回來再配上），等兩秒再看一次。真的缺，先用 `netplan get ethernets.eth1` 對照設定檔，改設定檔後 `netplan apply`；只要臨時驗證就 `ip addr add 10.0.5.10/24 dev eth1`。

## 3. 路由：封包知道往哪走嗎

```
ip route get 10.0.5.20
```

- **好**：回 `10.0.5.20 dev eth1 src 10.0.5.10`（同網段，直接走介面），或 `via <gateway> dev <iface>`。
- **壞**：`Network is unreachable`，或出口介面不是你預期的那個。
- **修**：同網段卻無路由，通常是第 2 步的位址或遮罩錯。跨網段缺 default route 就 `ip route add default via <gateway>`，長期解還是寫進 netplan。

## 4. 鄰居：對端的 MAC 學到了嗎

```
ip neigh
ip neigh show 10.0.5.20
```

- **好**：`10.0.5.20 dev eth1 lladdr 02:42:0a:00:05:14 REACHABLE`（或 `STALE`，也算正常）。
- **壞**：`FAILED`，或整張表沒有這筆。
- **修**：`FAILED` 代表 ARP 問到底沒人回——對端關機、對端介面 down、或兩邊其實不在同一個廣播域。用 `arping -I eth1 10.0.5.20` 再確認一次，然後去對端跑第 1、2 步。先清掉舊紀錄再重試：`ip neigh flush dev eth1`。

## 5. 連通性：真的通了嗎

```
ping -c 3 10.0.5.20
```

- **好**：三個回應、`0% packet loss`。
- **壞**：`Destination Host Unreachable`（ARP 失敗，回第 4 步）、逾時（回應被擋掉或對端不回，往第 6、7 步）。
- **修**：依上面的指向回到對應步驟。

到這裡小封包通了，但使用者還是抱怨慢或卡住，就繼續。

## 6. MTU：大封包過得去嗎

```
ping -M do -s 1472 -c 2 10.0.5.20
```

`-M do` 禁止分片，`-s 1472` 是 1500 MTU 下的滿載（1500 − 20 IP − 8 ICMP）。

- **好**：兩個回應。
- **壞**：`Frag needed and DF set (mtu = 1200)`，或完全沒回應但 `-s 56` 的一般 ping 正常。
- **修**：`ip link show eth1` 看本機 MTU，不對就 `ip link set eth1 mtu 1500`。本機正常的話，用二分法找路徑上的瓶頸：`ping -M do -s 1372`、`-s 1272`⋯⋯ 第一個能通的大小加 28 就是路徑 MTU。

## 7. 防火牆：是不是被擋了

```
nft list ruleset
iptables -S
```

- **好**：`nft list ruleset` 沒有輸出，或所有 chain 的 policy 是 `accept` 且沒有 drop 規則。
- **壞**：看到 `policy drop`，或針對對端位址、ICMP 的 `drop` / `reject` 規則。
- **修**：先找出是哪一條在擋——加上計數器重跑測試最準：

  ```
  nft list ruleset -a          # 顯示每條規則的 handle
  nft delete rule inet filter input handle 7
  ```

  用 iptables 的系統就用 `iptables -D` 刪對應規則。兩邊都要看：出去的機器看 `output`，對端看 `input`。ufw 也是 nftables 的前端，`ufw status verbose` 可以印出比較好讀的版本。

## 速查

| 層 | 指令 | 壞掉的樣子 |
|---|---|---|
| link | `ip -br link` | `DOWN` / `LOWERLAYERDOWN` |
| address | `ip -br addr show eth1` | 沒位址、遮罩錯 |
| route | `ip route get <對端>` | `Network is unreachable` |
| neighbour | `ip neigh show <對端>` | `FAILED` / 沒有這筆 |
| ping | `ping -c 3 <對端>` | 逾時、`Host Unreachable` |
| MTU | `ping -M do -s 1472 <對端>` | `Frag needed and DF set` |
| firewall | `nft list ruleset` | `policy drop`、drop 規則 |

前五步只讀狀態（除了 `neigh flush`），可以放心先跑一輪再動手。
