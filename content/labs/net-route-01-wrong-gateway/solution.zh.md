# 解答：web01 的路由指向不存在的閘道（有時閘道還沒開轉送）

這題有兩種變化，排查步驟一樣，第二種在閘道上多一個故障。

## 1. 先確認問題範圍

在 `web01` 上：

```
ping -c 2 <db01 的位址>
ping -c 2 <gw01 在本網段的位址>
```

同網段的閘道 ping 得到、跨網段的 db01 ping 不到，代表問題出在**離開本網段之後**的那一段，不是介面或位址。

## 2. 看 web01 挑了哪條路

```
ip -4 route show
ip route get <db01 的位址>
```

`ip route get` 會把核心真正會用的那條路印出來：

```
10.9.3.10 via 10.4.7.254 dev eth1 src 10.4.7.10
```

`via` 後面那個位址就是下一跳。把它跟工單上 `gw01` 在本網段的位址比一比，兩者不同——下一跳被指到同網段裡一台根本不存在的主機。

再確認一次這個下一跳是死的：

```
ping -c 2 <ip route get 印出來的 via 位址>
ip neigh show dev eth1
```

`ip neigh` 會看到那個位址是 `FAILED` 或 `INCOMPLETE`：ARP 問不到人，封包連送都送不出去。

## 3. 改掉那條路由

用 `replace`，它在路由存在時覆寫、不存在時新增，不必先 `del`：

```
ip route replace <db01 的網段> via <gw01 在本網段的位址>
ip route get <db01 的位址>
```

`via` 應該換成 `gw01` 的位址了。

## 4. 再 ping 一次

```
ping -c 3 <db01 的位址>
```

- **一般情況**：這時就通了，題目結束。
- **另一種情況**：路由對了還是不通。往下看。

## 5. 通不了就去閘道上看轉送

路由對、下一跳 ARP 也正常，封包就是回不來，最常見的原因是閘道根本不幫你轉送。在 `gw01` 上：

```
sysctl net.ipv4.ip_forward
cat /proc/sys/net/ipv4/ip_forward
```

`0` 代表核心收到不是給自己的封包就直接丟掉。打開它：

```
sysctl -w net.ipv4.ip_forward=1
```

要開機後仍然有效，就寫進設定檔（本題不需要，但正式環境要）：

```
echo 'net.ipv4.ip_forward=1' > /etc/sysctl.d/99-forward.conf
```

順便確認 `gw01` 兩側的位址都在、而且兩個網段的路由都是直連的：

```
ip -br addr
ip -4 route show
```

## 6. 驗證

回到 `web01`：

```
ping -c 3 <db01 的位址>
traceroute -n <db01 的位址>
```

`traceroute` 第一跳應該是 `gw01`、第二跳就是 `db01`。三個回應、0% packet loss 就完成了。

## 為什麼不用改 db01

回程路由（`db01` 往 `web01` 的網段走 `gw01`）在題目建好時就設好了。排查時還是值得看一眼，因為「去得了、回不來」的症狀跟這題很像：

```
ip -4 route show <web01 的網段>
```

## 為什麼不用改 netplan

故障是用 `ip route` 直接下在核心上的，netplan 的設定檔沒被動過。`ip route replace` 與 `sysctl -w` 就夠了，不需要 `netplan apply`。
