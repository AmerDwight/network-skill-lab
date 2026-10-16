# Phase 4 遠端 runner 與 VM：驗收結果

> 日期：2026-10-16 起。Spec：`docs/specs/phase4.html`。Task：issue #74 到 #79（T0 到 T5）。
> 本檔先記 T0 VM spike 的結果；§01 七條驗收由 T5 補齊。

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
| 建置時間 | provisioning 約 11 分鐘（apt 下載為主）+ 壓縮；輸出 659 MiB、虛擬 10 GiB |
| 以輸出 image 的 overlay 開機 | guest-agent 20.8 s、SSH 24.5 s |
| 契約檢查 | 介面 `eth0`；`nft`/`iptables`/`tcpdump`/`netplan`/`nmcli`/`snmpwalk` 都在；`/usr/local/sbin/nsl-bootstrap`、`/etc/nsl` 存在；`nsl` 使用者 NOPASSWD sudo 可用；machine-id 每台不同；`qemu-guest-agent`、`ssh`、`systemd-networkd` active，`NetworkManager`、`nginx` 停用 |
| 選項 | `WITH_K3S=1` 另裝 k3s binary（Phase 4 不驗證）；`NSL_VM_IMAGE_DIR`、`CLOUD_IMAGE_URL`、`DISK_SIZE`、`BUILD_MEM_MIB`、`NETWORK` 可覆寫 |

建置過程修掉的兩個錯誤：cloud-init `runcmd` 的某一項展開成 YAML 布林值 `true`，整個 runcmd 模組被拒絕（packages 照裝、其餘全跳過，只在 journal 留一行 warning）；rootfs tarball 以一般使用者打包，root 解開後 `/etc`、`/usr` 變成 uid 1000，sudo 失效，改以 `--owner=0 --group=0` 打包。
