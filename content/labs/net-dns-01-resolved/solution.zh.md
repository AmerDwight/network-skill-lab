# 解答：eth1 的 nameserver 被設成錯的（有時 /etc/resolv.conf 也被換掉）

這題有兩種變化：第一種只有 netplan 的 nameserver 設錯；第二種還多一個 `/etc/resolv.conf` 被換成普通檔案。

## 1. 先確認是解析壞了，不是網路壞了

在 `web01` 上：

```
getent hosts <要解的名稱>
ping -c 2 <dns01 的位址>
```

名稱解不出來、但 DNS 伺服器 ping 得到，代表 L3 是通的，問題在解析路徑上。

## 2. 直接問 DNS 伺服器，把伺服器排除嫌疑

`dig @server` 會略過本機的整套解析設定，直接對指定伺服器送查詢：

```
dig @<dns01 的位址> <要解的名稱> A +short
```

有答案回來，就證明伺服器是好的、`web01` 自己的設定才是問題。這一步是 DNS 排查最重要的分水嶺。

## 3. 看 resolved 現在用什麼

```
resolvectl status
resolvectl status eth1
resolvectl dns
resolvectl dns eth1
```

`resolvectl status` 會分別列出 Global 與每個 Link 的 `DNS Servers` 和 `DNS Domain`。看 `Link 2 (eth1)` 那段：它的 DNS Server 不是工單上的 `dns01`，而是同網段裡另一個位址——那台根本沒有在跑 DNS。

也可以直接問 resolved 一次，看它自己怎麼說：

```
resolvectl query <要解的名稱>
```

## 4. 找出這個設定是誰寫的，改掉它

per-link 的 DNS 由 netplan 交給 networkd 設定：

```
netplan get ethernets.eth1
ls /etc/netplan
```

把 `nameservers.addresses` 改成 `dns01` 的位址，`search` 保持原本的網域：

```
network:
  version: 2
  ethernets:
    eth1:
      nameservers:
        addresses: [<dns01 的位址>]
        search: [<網域>]
```

存檔後套用，再確認：

```
netplan apply
resolvectl dns eth1
resolvectl status eth1
```

`eth1` 的 DNS Server 應該換成 `dns01` 了。

## 5. 再解一次

```
resolvectl flush-caches
getent hosts <要解的名稱>
```

- **一般情況**：這時就解得出來，題目結束。
- **另一種情況**：`dig @<dns01>` 有答案、`resolvectl query` 也有答案，但 `getent hosts` 還是空的。往下看。

## 6. 解不出來就看 /etc/resolv.conf

`getent` 走的是 glibc 的 NSS。這台機器的 `/etc/nsswitch.conf` 是 `hosts: files dns`，也就是走傳統 resolver，讀 `/etc/resolv.conf` 找 nameserver。正常情況下那是一個指向 resolved stub 的符號連結：

```
ls -l /etc/resolv.conf
cat /etc/resolv.conf
```

正常長這樣（指向 stub，內容只有 `nameserver 127.0.0.53`）：

```
/etc/resolv.conf -> /run/systemd/resolve/stub-resolv.conf
```

壞掉的情況是它變成一個普通檔案，裡面直接寫死了錯的 nameserver。這時候不管 `resolvectl` 設得多正確都沒用，因為 glibc 根本沒問 resolved。

接回去：

```
ln -sf /run/systemd/resolve/stub-resolv.conf /etc/resolv.conf
ls -l /etc/resolv.conf
```

resolved 提供兩個檔案，差別要分清楚：

| 檔案 | 內容 | 什麼時候用 |
|---|---|---|
| `/run/systemd/resolve/stub-resolv.conf` | `nameserver 127.0.0.53` | 預設。查詢交給 resolved，per-link DNS、快取、search domain 全部有效 |
| `/run/systemd/resolve/resolv.conf` | 上游伺服器的位址 | 程式要繞過 resolved 時才用；per-link 路由會失效 |

## 7. 驗證

```
resolvectl flush-caches
getent hosts <要解的名稱>
resolvectl query <要解的名稱>
resolvectl status eth1
```

`getent hosts` 回傳工單上那台機器的位址就完成了。
