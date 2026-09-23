# Phase 0 風險驗證結果

> 日期：2026-09-21。Spec：`docs/specs/phase0.html`。腳本：`spikes/phase0/`（`make -C spikes/phase0 all`），原始輸出在 `spikes/phase0/results/`。
> 環境：WSL2（kernel 6.18.33）、Docker Engine 29.8、cgroup v2、4 CPU / 5.8 GB。

## 1. 總結

| 風險 | 結論 | 關鍵數字 |
|---|---|---|
| R1 Docker 配 IP 與 networkd 衝突 | **通過，採方案 A**。netplan 改 IP 生效、Docker 不搶回；network connect / disconnect 不影響；10 分鐘 soak 零漂移 | bootstrap 1.1 s |
| R2 systemd 在 privileged 容器 | **通過**。零 failed unit；`systemctl`、`journalctl -u`、`resolvectl`、`netplan apply` 正常。前提：udev 必須啟用 | systemd 開機 0.5 到 1.2 s |
| R3 k3s 雙節點 | **通過**。完全離線起叢集、跨節點 Pod 互 ping（flannel VXLAN）、coredns 正常 | 兩節點 Ready 16 到 21 s |
| R4 斷網後 exec | **通過**。全介面 down、iptables / nft 全 DROP 後 `docker exec` 仍可用 | exec 延遲 63 ms |
| R5 image 與冷啟動 | **通過**（優化後）。Ubuntu 雙節點 6.3 s（目標 < 10）；k3s 雙節點 Ready 16 到 21 s（目標 < 30） | image 2014 MB，建置 415 s |
| D11 指令歷史 hook | **通過，有已知缺口**（§3.5） | |

方案 A 成立，不需退方案 B。Phase 0 一個工作天內完成（時間盒三天）。

## 2. 量測數字

| 項目 | 數值 | 備註 |
|---|---|---|
| image 大小 | 2014 MB | 含 k3s binary、airgap images 193 MB、預解壓 data 253 MB |
| image 建置 | 415 s | 首次完整建置，含下載 |
| `docker run` 到 systemd running | 0.4 + 0.7 s | 拿掉 `VOLUME` 後；同一 image 第一次啟動額外 5 到 8 s（一次性） |
| Ubuntu 雙節點冷啟動（run → bootstrap 完成） | 6.3 s | 含 3 次 `docker network connect` |
| k3s 雙節點 → 兩節點 Ready | 16.1 到 21.4 s | 優化前 36 到 62 s；三項優化見 §3.4 |
| k3s 系統 Pod 全部 Running | 40 到 43 s | 多的 20 多秒是 traefik 的 helm-install job |
| k3s server 記憶體（30 s 後閒置） | 665 到 773 MB | helm-install 期間量到 900 到 1270 MB |
| k3s agent 記憶體 | 308 到 352 MB | |
| Ubuntu 節點記憶體（閒置） | 21 MB | |
| exec 延遲 | 63 ms | 全介面 down 狀態下 |

**資源估算**：host 5.8 GB，扣 Docker 與控制面約 1 GB，1 server + 2 agent + 2 ubuntu 約 1.5 GB，L5 複合題不需要調 `.wslconfig`。

## 3. 各項細節

### 3.1 S0 環境檢查
WSL2 核心 vxlan、nf_tables、nf_conntrack 內建；bridge、br_netfilter、8021q、dummy、macvlan 為模組且可自動載入。privileged 容器內 `/dev/kmsg` 可讀、`/sys/fs/cgroup` 為 cgroup2 且可寫。無需自編核心。

### 3.2 S1 image
- 66 個工具全部在 PATH，man page 可用（移除了 ubuntu image 的 dpkg excludes）。
- systemd 以 PID 1 開機，零 failed unit。mask 的單元：modules-load、sys-kernel-* mount、hwdb-update、remount-fs、getty、logind、ufw.service。
- **udev 必須啟用**：`netplan apply` 會呼叫 `udevadm control --reload`，mask 掉 udevd 會讓 netplan 直接失敗。實測 udevd 在容器內無副作用。
- **不宣告 `VOLUME`**：原本 `VOLUME /var/lib/rancher` 讓 Docker 每建一個容器就複製 193 MB 進匿名 volume，`docker run` 要 4 到 5 秒；拿掉後 0.4 秒。
- k3s：airgap tarball 放 `/usr/share/nsl/`，k3s 角色由 bootstrap 建符號連結到 `agent/images/`；build 階段跑 `k3s ctr --help` 預先解壓 data 目錄（+253 MB，冷啟動省約 15 秒）。
- 使用者 `nsl`（sudo NOPASSWD）與 root 皆可用；`/var/log/nsl/commands.jsonl` 預建為 0666。

### 3.3 S2 networkd 接管（R1、R2、R4）
- **介面順序**：建容器只接 mgmt，再依 topology 順序 `docker network connect`，實測 eth1 / eth2 順序等於 connect 順序、與網路名稱無關（P4a 成立）。
- **`docker restart` 不支援**：重啟後 Docker 重新接網路，兩次觀察順序不同（一次 eth1 變成另一條 link，一次維持）。v1 的 runner 對節點只做建立與銷毀。若未來需要 restart，用 netplan `match: {macaddress}` + `set-name` 依 MAC 綁名字即可徹底解決，Phase 1 可順手做。
- **Docker 的 bind mount**：`/etc/resolv.conf`、`/etc/hosts`、`/etc/hostname` 在 privileged 容器內可 `umount`，之後 resolved 以 symlink 接管、hostname 可寫。restart 後會被 Docker 蓋回，需重跑 bootstrap（同上，v1 不 restart）。
- **DNS**：resolved 的 Global DNS 設為 Docker embedded DNS `127.0.0.11`（走 mgmt）。Docker 的 search domain 不繼承。
- eth0（mgmt）保持 networkd unmanaged，default route 不受 `netplan apply` 影響。
- 全介面 down + iptables / nft DROP 後 exec 正常，checker 用 exec 不受題目影響。

### 3.4 S3 k3s（R3、R5）
- 兩個網路都 `--internal`（無 internet），叢集正常起來，8 個 image 全由 airgap 匯入，journal 無任何 registry pull 成功紀錄。
- agent 偶發在 pause image 匯入完成前收到建 sandbox 的要求，失敗約 8 次後自動重試成功（離線下無法 pull，屬暫時性）。不影響結果，但會稍微拉長系統 Pod 就緒時間。
- containerd 在 volume 上用 overlayfs snapshotter。volume 只掛 `/var/lib/rancher/k3s/agent/containerd`，其餘 k3s 目錄留在 image overlay（含預解壓的 data）。
- `node-ip` / `flannel-iface: eth1` 有效，Node InternalIP 為 link 位址而非 mgmt。
- 兩個 k3s unit 都是 `Type=notify`。server 約 6 秒送 READY，但 agent 的 READY 固定在啟動後約 43 秒才送（節點 12 秒就已註冊）。bootstrap 若用阻塞式 `enable --now` 會把冷啟動拖到 36 到 49 秒且抖動很大；改成 `--no-block` 後 bootstrap 0.8 秒回來，由 runner 以 kubectl 輪詢 Node Ready，冷啟動穩定在 16 到 21 秒。
- 冷啟動三項優化各自的效果：拿掉 VOLUME 省 4 到 5 秒、預解壓 k3s data 省約 15 秒、非阻塞啟動省 15 到 30 秒的抖動。
- coredns 上游：k3s 偵測到 resolved 的 127.0.0.53 會改用 8.8.8.8，在 `internet: false` 時外部名稱 SERVFAIL，叢集內名稱正常。這符合真實行為，DNS 題可利用。
- 時間線（server）：unit 啟動 → k3s 起 0.1 s → kubeconfig 1.7 s → 匯入 images 2.7 s → up and running 5.7 s。agent 匯入 images 需約 9 秒，是 Ready 前的最大單項。

### 3.5 S4 指令歷史 hook（D11）
- 記錄格式：`{"ts","user","cwd","cmd","exit"}` 每行一筆 JSON。
- 涵蓋：login shell、非 login 互動 shell、`sudo -i` 內層 shell（記為 root）、pipeline 整條、引號原樣。
- 第一個 prompt 不記錄，避免把 `.bash_history` 殘留的上一筆誤記。
- **已知缺口**：非互動子 shell（`bash -c "..."`、腳本內部）只記外層那一行；使用者可以編輯或清空 0666 的 log 檔。面試模式的可信來源必須是 pty 錄影（recorder），command log 只是輔助索引。

## 4. 對設計的影響

已回寫 `docs/DESIGN.md` §3.6、§4、§7。摘要：

1. udev 啟用；image 不宣告 VOLUME；k3s 角色由 provider 掛 volume 到 `/var/lib/rancher/k3s/agent/containerd`。
2. airgap tarball 在 `/usr/share/nsl/`，k3s data 於 build 預解壓。
3. Runner 建容器只接 mgmt，依 topology 順序 connect；節點不 restart。
4. bootstrap 定稿為 image 內的 `/usr/local/sbin/nsl-bootstrap`（spec 原寫 `spikes/phase0/bootstrap.sh`，因為它是正式產物而改放 image）。契約見 DESIGN.md §4。
5. 冷啟動定義維持 spec P5；k3s lab 的 precheck 需自行決定要等「Node Ready」還是「系統 Pod 全 Running」。

## 5. 未做與後續

- `ubuntu-nm` role（NetworkManager）只寫了設定檔，未實測；Phase 2 第一個用到的 lab 再驗。
- 系統 Pod 就緒的 20 秒可用 `--disable traefik` 換取，是否預設關閉交 Phase 1 spec 決定（可做成 topology role 選項）。
- 首次啟動新 image 的 5 到 8 秒一次性成本，runner 可在 image 更新後預熱一次。
