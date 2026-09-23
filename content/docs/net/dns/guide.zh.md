# DNS 與 resolved：Ubuntu 24.04

Ubuntu 24.04 的名稱解析由 **systemd-resolved** 負責。應用程式看到的是 `127.0.0.53` 這個 stub，真正的伺服器清單、快取與路由規則都在 resolved 裡面。`/etc/resolv.conf` 只是通往它的入口，不是設定檔。

## 一眼看完現況

```
resolvectl status              # Global 與每個 Link 的伺服器、網域、DNSSEC 模式
resolvectl status eth1         # 只看一個介面
resolvectl dns                 # 只列伺服器
resolvectl dns eth1
resolvectl domain eth1         # search / routing 網域
```

`resolvectl status` 的輸出分兩層：

```
Global
       Protocols: -LLMNR -mDNS ...
    DNS Servers: 127.0.0.11

Link 2 (eth1)
    DNS Servers: 10.4.7.53
     DNS Domain: lab.internal
```

Global 是沒有任何介面認領時的後備；Link 那段才是這個介面的設定。**排查時先看名稱該由哪個 Link 回答。**

## 查詢：三個層次，三個工具

| 指令 | 走哪條路 | 用來確認什麼 |
|---|---|---|
| `dig @<server> <name>` | 直接對指定伺服器送 UDP 查詢 | 伺服器本身有沒有答案 |
| `resolvectl query <name>` | 問 resolved | resolved 的路由與快取是否正確 |
| `getent hosts <name>` | 走 glibc NSS（`/etc/nsswitch.conf`） | 應用程式真正會拿到什麼 |

由下往上壞，症狀完全不同，所以三個都要會：

```
dig @10.4.7.53 app.lab.internal A +short
dig @10.4.7.53 app.lab.internal ANY
resolvectl query app.lab.internal
getent hosts app.lab.internal
```

`dig` 常用旗標：`+short` 只印答案、`+time=2 +tries=1` 不要等太久、`+trace` 從 root 一路追、`-x` 反解。

## stub 與 /etc/resolv.conf 的三種模式

```
ls -l /etc/resolv.conf
cat /etc/resolv.conf
```

resolved 會準備三個檔案，`/etc/resolv.conf` 指向哪一個決定了整台機器的行為：

| 目標 | 內容 | 效果 |
|---|---|---|
| `/run/systemd/resolve/stub-resolv.conf` | `nameserver 127.0.0.53` | **預設**。查詢進 resolved，per-link DNS、快取、search domain 全部有效 |
| `/run/systemd/resolve/resolv.conf` | 上游伺服器位址 | 繞過 resolved，直接問上游；per-link 路由與快取失效 |
| 普通檔案（非連結） | 手寫的 nameserver | 完全脫離 resolved，`resolvectl` 怎麼改都沒用 |

第三種是最難抓的：`resolvectl query` 成功、`getent hosts` 失敗，就要往這裡看。接回預設：

```
ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
```

## netplan 的 nameservers

per-link 的 DNS 設定由 netplan 寫給 networkd，不要直接改 `/etc/systemd/network/`：

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
netplan get ethernets.eth1     # 目前合併後的設定
netplan apply                  # 套用
resolvectl status eth1         # 驗證 resolved 收到了
```

`/etc/netplan/` 下多個檔案會依檔名順序合併，同一個 key 後面的蓋前面的。`chmod 0600` 是必要的，否則 netplan 會警告。

### search 與 routing 網域

`search:` 裡的網域有兩個作用：短名稱自動補全，以及**把該網域的查詢路由到這個 Link 的伺服器**。`resolvectl domain eth1` 會顯示它們；前面有 `~` 的是「只路由、不補全」的 routing-only 網域。

想手動加一個只路由的網域：

```
resolvectl domain eth1 lab.internal '~corp.test'
```

（`resolvectl` 的直接設定在下次 `netplan apply` 時會被蓋掉，正式設定還是要寫進 netplan。）

## global DNS 與 per-link DNS

resolved 決定把查詢送去哪裡的順序是：

1. 有哪個 Link 的網域與查詢名稱相符 → 用那個 Link 的伺服器。
2. 沒有相符的 → 用 Global 伺服器，以及所有沒有網域限制的 Link。

所以「內部網域走內部 DNS、其他走公用 DNS」不需要 split-horizon，只要把內部網域放進那個介面的 `search` 即可。Global 伺服器寫在：

```
/etc/systemd/resolved.conf
/etc/systemd/resolved.conf.d/*.conf      # 建議用 drop-in
systemctl restart systemd-resolved
```

## 快取

```
resolvectl statistics          # 命中率、目前快取筆數
resolvectl flush-caches        # 清掉，包含否定快取
```

改完伺服器設定後**一定要清快取**再測，否則你很可能還在看上一個伺服器留下的否定答案（NXDOMAIN 也會被快取）。

## 常見故障樣態

| 症狀 | 多半是 |
|---|---|
| `dig @server` 有答案，`resolvectl query` 沒有 | Link 的 DNS 或網域設錯，查詢被送去別的伺服器 |
| `resolvectl query` 有答案，`getent hosts` 沒有 | `/etc/resolv.conf` 不是 stub 連結，或 `nsswitch.conf` 的 `hosts:` 被改過 |
| 短名稱不通、FQDN 通 | `search` 網域沒設或設錯 |
| 改對了還是解不出來 | 否定快取沒清，`resolvectl flush-caches` |
| 查詢每次都等好幾秒才失敗 | 伺服器位址指到沒有人在聽的主機，resolved 在等逾時 |
| `ping` 通但名稱不通 | L3 沒問題，問題全在解析路徑上 |

抓封包確認查詢真的送出去了：

```
tcpdump -ni eth1 port 53
resolvectl log-level debug
journalctl -u systemd-resolved -f
```
