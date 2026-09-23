# 介面與位址：Ubuntu 24.04（netplan + systemd-networkd）

Ubuntu 24.04 的網路由 **netplan 寫設定、systemd-networkd 實際執行**。`ip` 指令看到的是核心當下的狀態，netplan 看到的是開機後應該長成的樣子。兩者不一致，就是排查的起點。

## 一眼看完狀態

```
ip -br link          # 每個介面一行：名稱、狀態、MAC
ip -br addr          # 每個介面一行：名稱、狀態、位址
ip -br addr show eth1
```

`-br`（brief）的輸出長這樣：

```
eth0             UP             172.18.0.2/16
eth1             DOWN
```

狀態欄的三種值要分清楚：

| 狀態 | 意思 | 通常的原因 |
|---|---|---|
| `UP` | 行政上開啟，且有載波 | 正常 |
| `DOWN` | 行政上關閉 | 有人下了 `ip link set ... down`，或設定裡沒啟用 |
| `LOWERLAYERDOWN` | 行政上開啟，但沒有載波 | 線沒插、對端關機、虛擬介面對側不存在 |

「載波」（carrier）就是實體層有沒有訊號。核心把它放在 `/sys/class/net/<iface>/carrier`：

```
cat /sys/class/net/eth1/carrier      # 1 = 有載波，0 = 沒有
cat /sys/class/net/eth1/operstate
```

`DOWN` 是你能直接修的；`LOWERLAYERDOWN` 要去看對端。

## 開關介面

```
ip link set eth1 up
ip link set eth1 down
```

**重要**：networkd 在介面失去載波時會把上面的位址清掉。所以 `ip link set eth1 down` 之後 `ip addr` 會看不到位址，這不是設定不見了——把介面拉回 `up`，networkd 會依 netplan 重新配上。不要急著自己 `ip addr add`，那只會多一個不在設定檔裡的位址。

## MTU

```
ip link show eth1                    # 第一行的 mtu <n>
cat /sys/class/net/eth1/mtu
ip link set eth1 mtu 1500
```

乙太網路的標準值是 1500。MTU 被改小的症狀很特別：ping、SSH 登入這種小封包都正常，一傳大檔或貼長輸出就卡住。驗證方式是送不分片的滿載封包：

```
ping -M do -s 1472 -c 2 10.0.5.20
```

1472 = 1500 − 20（IP header）− 8（ICMP header）。回 `Frag needed and DF set (mtu = 1200)` 就是路徑上有人把 MTU 調小了。

## 位址

```
ip addr add 10.0.5.10/24 dev eth1
ip addr del 10.0.5.10/24 dev eth1
```

這兩個只改核心狀態，重開機或 `netplan apply` 就沒了。臨時驗證可以用，長期設定要寫進 netplan。

## 路由

```
ip route                    # 主路由表
ip route get 8.8.8.8        # 這個目的地實際會走哪條、從哪個介面出去
ip -br addr show eth0
```

`ip route get` 比讀整張表快得多，直接告訴你核心的決策：

```
8.8.8.8 via 172.18.0.1 dev eth0 src 172.18.0.2
```

沒有 default route 時會回 `Network is unreachable`。

## networkd 怎麼看

```
networkctl                       # 每個介面的 SETUP 狀態
networkctl status eth1           # 位址、路由、來自哪個設定檔
journalctl -u systemd-networkd -n 50 --no-pager
```

`networkctl` 的 SETUP 欄位：`configured` 是好的，`configuring` 卡住通常是等 DHCP 或等載波，`unmanaged` 表示 networkd 不管它（本環境的 `eth0` 管理介面就是這樣，刻意讓 Docker 的預設路由不受 `netplan apply` 影響）。

## netplan 與設定檔位置

設定檔在 `/etc/netplan/*.yaml`，權限應為 `600`：

```
ls -l /etc/netplan/
netplan get                       # 合併後的完整設定
netplan get ethernets.eth1        # 只看一個介面
```

改完之後：

```
netplan generate                  # 只產生 networkd 設定，不套用
netplan apply                     # 產生並要求 networkd 重新載入
```

產生出來的 networkd 單元在 `/run/systemd/network/`，想知道 netplan 到底翻譯成什麼就看這裡。

## 排查順序

1. `ip -br link`：介面在不在、狀態是什麼。
2. `ip -br addr`：位址對不對、遮罩對不對。
3. `ip route get <目的地>`：走得出去嗎。
4. `networkctl status <iface>`：networkd 認為它設定好了嗎。
5. `netplan get`：設定檔和現況差在哪。

前四步只讀狀態，沒有副作用，先做完再動手改。
