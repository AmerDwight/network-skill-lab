# Phase 4 遠端 runner 與 VM：驗收結果

> 日期：2026-10-16 起。Spec：`docs/specs/phase4.html`。Task：issue #74 到 #79（T0 到 T5）。
> 驗收環境：同一台 WSL2 主機；控制面（`NSL_INSTANCE=accept-cp`、`NSL_MAX_SANDBOXES=1`、port 18080）、一個 Docker 遠端 runner（`remote-a`，容量 2，instance 標籤 = runner 名稱）與一個 VM runner（`vm-01`，容量 2，`--provider vm`）都是獨立的 `nsl` 程序，透過 `ws://127.0.0.1:18080/ws/runner` 連線。驅動腳本以 cookie jar 走 API 與 terminal WebSocket，與 Phase 3 相同。

## 1. §01 完成定義逐條

| # | 條目 | 結果 | 實測 |
|---|---|---|---|
| 1 | 遠端執行 | 通過 | bob 的 attempt 被排到 `remote-a`（`runner: "remote-a"`），沙箱容器帶 `nsl.instance=remote-a` 標籤、由 runner 程序建立；terminal 經控制面轉發修好 `eth1` 後 15.1 s 進 passed；控制面 instance（`accept-cp`）名下只有 alice 那題的容器。錄影、指令歷史、checkpoint 皆照常 |
| 2 | 斷線有交代 | 通過（有一項修正） | `kill -9` remote-a 程序後 carol 的 attempt **0.5 s** 進 `error`，`error_message` 為 `runner disconnected`；同時在 `local` 上的 bob 不受影響。runner 程序存活的斷線（T2/T3 測試以 TCP proxy 切線）重連後殘留沙箱被控制面銷毀；但**程序整個重啟**時新程序的 hello 回報 `sandboxes: 0`，4 個容器與 4 個網路留在主機上直到手動 `NSL_INSTANCE=remote-a nsl gc` → 修正為 runner 啟動時先 GC 自己的 instance（T5b） |
| 3 | 多 runner 並存 | 通過 | alice → `local`、bob → `remote-a` 同時 running、各自 passed；admin runners 列出 `local 0/1`、`remote-a 0/2`、`vm-01 0/2` 含能力、連線狀態、版本；三題佔滿後第四題 429 `runner_busy`，body 帶 `runners: [{local 1/1}, {remote-a 2/2}]`，`sandboxes_max` 為兩者容量和 3；同一時間 `environment: vm` 的題仍能排到 `vm-01`（容量分開計） |
| 4 | VM spike 有結論 | 通過 | 見上節，V1 到 V5 全過，T4 已做 |
| 5 | VM lab 可跑 | 通過 | `net-ip-01-vm` 由 alice 從 API 開題 → `vm-01`，**40.8 s** 進 running（兩台 VM 冷啟動含 cloud-init、bootstrap、setup），SSH pty 下 `ip link set eth1 up` 後 5.2 s 三個 checkpoint 全過進 passed；結束後 libvirt 無殘留 domain / network |
| 6 | 保留策略 | 通過 | `nsl prune --older-than 1s --dry-run` 列出 12 筆、4218 bytes；admin stats 有 `oldest_attempt_at`；#71 由 #80 修好（Destroy 等容器消失再刪網路，`nsl gc` 回報數量） |
| 7 | 回歸 | 通過 | §2：lint、unit、content、content-remote、vm 全綠；integration 只剩一個 Phase 2 起的時序 flake，另開 PR 修 |

其他驗過的點：撤銷 `remote-a` 的 token 後 **3.3 s** 內該 runner 程序收到 `runner token revoked` 並結束，admin 列表 `connected: false` 且有 `revoked_at`；重複名稱 409、不合法名稱 400、非 admin 讀 runners 端點 403、匿名 401；`/api/runners/summary` 給一般使用者看 runner 能力與連線狀態（前端據此在沒有 vm runner 時停用 Start）。

## 2. 套件

在最終 main（含 #80 到 #86）依序跑：

| 套件 | 結果 |
|---|---|
| `make lint` | 0 issues（golangci-lint、eslint、prettier、tsc） |
| `make test` | Go 17 個 package、前端 33 個檔案 290 個測試全過，52 s |
| `make test-integration` | 除 `TestK3sSandbox` 外全過（154 s）；該測試是 Phase 2 起就有的時序競態（見 §3），修正另開 PR |
| `make test-content` | 五個 lab 全過，171 s |
| `make test-content-remote` | 同一套 content 測試改走 runner 協定（in-process hub + runnerclient + Docker provider），五個 lab 全過，164 s；每個 attempt 的 `runner` 都是遠端名稱 |
| `make test-vm` | VM provider 單元 + 整合（兩台 VM 冷啟動 37.3 s）與 runnerclient 經協定跑 VM provider 全過，88 s |

## 3. 驗收與開發過程抓到的問題

| 問題 | 處理 |
|---|---|
| runner 程序被 kill 後重啟，前一個程序留下的沙箱沒人清 | T5b：`nsl runner` 啟動時先對自己的 instance 跑 GC |
| 前端 provisioning 面板不認得 VM provider 的 `nodes` 步驟 | T5a |
| content 套件只能跑 in-process runner，§01 第 7 條要求也能指向遠端 runner | T5a：`make test-content-remote` |
| `TestK3sSandbox` 一天內失敗四次：`check passed although the pod of broken is crashlooping` | 不是 Phase 4 回歸。`waitForCrashLoop` 把「restartCount ≥ 1」也當成 crash loop，但第一次重啟後容器會短暫 Running、deployment 的 `availableReplicas` 瞬間為 1，check 在那個視窗內合法地通過。修正：只在 pod 進入 `CrashLoopBackOff` 後才斷言（測試層修正，另開 PR） |
| T1 的 k3s 整合測試在 VM image 建置期間（load 12 到 18）逾時 | 負載問題：主機空檔重跑 49.5 s 通過 |
| #81 與 #80 在 `cmd/nsl/main.go` 的子指令 case 衝突 | planner 做機械性 rebase（兩邊都保留）後重跑 CI 合併 |
| Haiku 把 chi 巢狀 Group 的 admin 路由判成沒掛 session 驗證 | 誤報：巢狀 Group 繼承外層中介層，httptest 亦驗證 401；駁回 |

## VM spike（T0，spec §08）

驗證環境：WSL2（kernel 6.18、nestedVirtualization 開啟）、`/dev/kvm` 可用、libvirt 10.0 + QEMU 8.2.2（socket 啟動，`systemctl is-active libvirtd` 平常顯示 inactive 是正常的）、4 CPU / 5.8 GB、Ubuntu 24.04 cloud image。腳本在 `spikes/phase4/`，原始輸出在 `spikes/phase4/results/`。

| # | 驗證 | 結果 | 實測 |
|---|---|---|---|
| V1 | KVM 可用 | 通過 | `virsh capabilities` 列出 `domain type='kvm'`；host CPU Broadwell-noTSX-IBRS；`virt-host-validate` 只有 IOMMU 與 cgroup devices 警告，兩者 VM 練習不需要 |
| V2 | cloud-init 開機時間 | 通過 | 以 base qcow2 的 overlay 開一台 1 vCPU / 1 GiB VM：`virsh start` 到 guest-agent 可 ping **19.4 s**（門檻 45 s）、到 SSH 可登入 **25.0 s**（門檻 60 s） |
| V3 | guest-agent exec 不受題目網路影響 | 通過 | 關掉 mgmt 介面後 SSH 逾時、guest-exec 0.11 s 回覆並正確傳回 exit code 7；再加上所有介面 down + nftables input/output `policy drop`，guest-exec 仍成功（3.2 s，耗時來自 sudo 解析主機名，bootstrap 會寫 /etc/hosts） |
| V4 | 隔離 link 網路 + mgmt NAT | 通過 | `<network>` 只有 `<bridge>`、無 `<ip>`（無 DHCP 無 NAT）；兩台 VM 以 cloud-init network-config 設 10.10.0.1/2，互 ping 0% loss；host 端 bridge `nslbr-a` 沒有位址，VM 從 link 打 host 不通；mgmt 走 `default` NAT 網路，DNS 與對外正常 |
| V5 | 資源 | 通過 | 兩台 1 GiB VM 同時冷啟動：兩邊 guest-agent 都就緒 **45.9 s**、SSH 都就緒 **60.0 s**（門檻 90 s；量測時主機同時在跑兩個開發 agent 的 go build，load 12）；host 已用記憶體 +921 MiB，每個 qemu 程序 RSS 約 615 MiB；兩個 overlay 合計 34 MB |

### 過程中抓到的問題

| 問題 | 處理 |
|---|---|
| 兩台 VM 從同一個 base 開機拿到同一個 DHCP 位址：systemd-networkd 的 DUID 由 `/etc/machine-id` 推導，base image 裡的 machine-id 相同，dnsmasq 把同一租約給兩台 | `build.sh` 收尾 `truncate -s 0 /etc/machine-id` 並刪 `/var/lib/dbus/machine-id`，首次開機重新產生；netplan 另加 `dhcp-identifier: mac` 雙保險 |
| 介面名稱是 `enp1s0`、`enp2s0`，與容器的 `eth0`/`eth1` 契約不同，bootstrap 的 `NSL_IFACES` 會對不上 | `build.sh` 設 kernel 參數 `net.ifnames=0 biosdevname=0`，VM 內一樣是 eth0 = mgmt、eth1 起 = link |
| 需要 libvirt 能讀 image 目錄：使用者家目錄下 qemu（libvirt-qemu 使用者）讀不到 | image 與 overlay 放在 `/var/tmp` 或專用目錄，`build.sh` 對輸出目錄 `chmod o+rx`；T4 的 provider 要用可設定的 image 目錄並在啟動時檢查權限 |
| stock cloud image 沒有 qemu-guest-agent；靠 cloud-init `packages` 每次裝會把開機拖到分鐘級 | 預裝進 base image（`build.sh`），開機時 cloud-init 只做 hostname / 金鑰 / network-config |
| 開發主機沒有 `jq`，`virsh qemu-agent-command` 的 JSON 要另外解 | spike 用 python3；T4 在 Go 內直接 unmarshal，無此問題 |

### T4 決定

**做。** 門檻全部達標，libvirt 走 `virsh` CLI 包裝（純 Go 建置不需 cgo），實作依 spec §09：

- image：`images/vm/build.sh` 產生扁平化的 `nsl-node.qcow2`（見下節）；每個 node 開 backing overlay。
- cloud-init：NoCloud ISO（`genisoimage -volid cidata`）帶 user-data（nsl 使用者的一次性 SSH 公鑰）、meta-data（instance-id 每次不同）、network-config（eth0 dhcp、link 介面由 bootstrap 以 netplan 設定）與 `/etc/nsl/bootstrap.env`。
- Exec：`virsh qemu-agent-command` 的 `guest-exec` / `guest-exec-status`（輪詢，輸出 base64），exit code 原樣傳回。
- PTY：`golang.org/x/crypto/ssh` 連 mgmt IP（`virsh domifaddr --source lease`，或 agent 來源作後備）。
- 網路：每條 link 一個暫態 `<network>`（`net-create`），只有 `<bridge>`；mgmt 用 libvirt `default` NAT 網路或自建一個 instance 專屬的 NAT 網路。
- GC：domain `<metadata>` 與網路名稱帶 instance 標籤，與 Docker provider 的 `nsl.instance` 相同語意。
- 首次開機時間以 spike 數字為基準：單台 25 s、兩台 60 s 內 SSH 就緒。

### `images/vm/build.sh` 初版

從 Ubuntu 24.04 cloud image 開一台暫態 VM，用 cloud-init 裝 `nsl/node` 同一份套件清單加 `qemu-guest-agent`，把 `images/node/rootfs` 以 NoCloud ISO 帶進去解開，設 `net.ifnames=0`（介面叫 eth0/eth1）、清 machine-id，關機後 `qemu-img convert -c` 壓成不依賴下載檔的 `nsl-node.qcow2`。只需要 `qemu-img`、`genisoimage`、`curl`、`virsh`。

| 項目 | 實測 |
|---|---|
| 建置時間 | provisioning 139 s 到 660 s（三次建置，差異來自 apt 下載速度）+ 壓縮約 1 分鐘；輸出 659 MiB、虛擬 10 GiB |
| 以輸出 image 的 overlay 開機 | guest-agent 20.8 s、SSH 24.5 s |
| 契約檢查 | 介面 `eth0`；`nft`/`iptables`/`tcpdump`/`netplan`/`nmcli`/`snmpwalk` 都在；`/usr/local/sbin/nsl-bootstrap`、`/etc/nsl` 存在；`nsl` 使用者 NOPASSWD sudo 可用；machine-id 每台不同；`qemu-guest-agent`、`ssh`、`systemd-networkd` active，`NetworkManager`、`nginx` 停用 |
| 選項 | `WITH_K3S=1` 另裝 k3s binary（Phase 4 不驗證）；`NSL_VM_IMAGE_DIR`、`CLOUD_IMAGE_URL`、`DISK_SIZE`、`BUILD_MEM_MIB`、`NETWORK` 可覆寫 |

建置過程修掉的兩個錯誤：cloud-init `runcmd` 的某一項展開成 YAML 布林值 `true`，整個 runcmd 模組被拒絕（packages 照裝、其餘全跳過，只在 journal 留一行 warning）；rootfs tarball 以一般使用者打包，root 解開後 `/etc`、`/usr` 變成 uid 1000，sudo 失效，改以 `--owner=0 --group=0` 打包。
