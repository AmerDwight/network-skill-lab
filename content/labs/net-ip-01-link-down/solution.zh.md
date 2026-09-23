# 解答：介面被關閉（有時還被改了 MTU）

這題有兩種變化，排查步驟一樣，只是第二種多一個故障。

## 1. 先看介面狀態

在 `web01` 上：

```
ip -br link
```

`eth0` 是管理網路，不要動它。資料介面 `eth1` 的狀態是 `DOWN`，代表被行政性關閉。

位址這時候查不到是正常的：networkd 在介面失去載波時會把位址清掉，介面回到 UP 之後會依 netplan 重新配上。所以不必自己 `ip addr add`。

## 2. 把介面拉起來

```
ip link set eth1 up
ip -br link
ip -br addr show eth1
```

`eth1` 應該變成 `UP`，位址也自己回來了。

## 3. 檢查 MTU

只把介面拉起來不一定就結束了。再看一次完整輸出：

```
ip link show eth1
```

- **一般情況**：`mtu 1500`，這一關直接通過。
- **另一種情況**：`mtu 1200`。有人把 MTU 調小了，小封包會通、大封包被丟掉，症狀很像「時好時壞」。

MTU 不對就改回乙太網路的標準值：

```
ip link set eth1 mtu 1500
```

想確認大封包真的過得去，可以送一個不分片的滿載封包（1500 − 20 IP header − 8 ICMP header = 1472）：

```
ping -M do -s 1472 -c 2 <對端位址>
```

## 4. 驗證

```
ping -c 3 <對端位址>
```

三個回應、0% packet loss 就完成了。對端位址在工單上，也可以用 `ip -br addr show eth1` 反推同網段的另一台。

## 為什麼不用改 netplan

故障是用 `ip link set` 直接下在核心上的，netplan 的設定檔從頭到尾沒被動過。`ip link set ... up` 與 `ip link set ... mtu 1500` 就夠了。想確認設定檔沒問題可以跑 `netplan get` 或 `networkctl status eth1` 看一眼，但不需要 `netplan apply`。
