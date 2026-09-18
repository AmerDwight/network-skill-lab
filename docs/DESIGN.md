# network-skill-lab 設計文件

> 狀態：v1，2026-09-18 已確認。§10 四項判斷皆採納。
> 日期：2026-09-18

## 0. 一句話

一個放在網路上的 Web terminal 練習場：使用者拿到一張「工單」，在真實的 Ubuntu Server 沙箱裡用 CLI 排查並修復網路 / k3s 問題，系統記錄時間、指令與過程。內容從教學一路延伸到擬真面試題，並可由團隊持續擴充。

## 1. 已確認的決策（不再重開）

| # | 決策 |
|---|---|
| D1 | 真沙箱，不做假 shell。Web terminal 只是 pty 轉發 |
| D2 | 後端 Go，前端 React + TypeScript + xterm.js |
| D3 | 控制面 / Runner 分離的架構，v1 只實作單一 binary 全包模式 + in-process runner |
| D4 | 環境提供者可插拔：v1 只做 Docker 容器；VM（libvirt + QEMU/KVM + cloud-init）留 v2。內容格式從 v1 就有 `environment` 欄位 |
| D5 | 多節點 = 同一 host 上多個容器 / VM，不需多實體機 |
| D6 | 帳號簡單化：本地帳密，兩種角色 admin / user。一個帳號同時只能有一個進行中的練習 |
| D7 | 瀏覽器斷線 15 分鐘後回收沙箱 |
| D8 | 不計分。記錄總時間、指令數、指令歷史、pty 錄製 |
| D9 | Checkpoint 帶 `visible` 旗標；背景服務偵測通過狀態。另有「真實模式」不顯示任何 checkpoint |
| D10 | 情境隨機化：一個 lab 含多個 case，參數產生 + `precheck.sh` 自檢，失敗重生最多 N 次 |
| D11 | 指令歷史用 bash `PROMPT_COMMAND` hook；pty 原始輸出另存 asciicast 供日後回放 |
| D12 | UI 與內容雙語（zh-TW / en） |
| D13 | 預期同時一人使用，規格不為並行最佳化，但不封死 |
| D14 | 面試功能（時限、即時觀看、回放 UI、成績頁）延後；只有錄製先做進資料模型 |
| D15 | 命名：repo `network-skill-lab`（2026-09-18 由 aiwaxis_skill_quiz 改名）、Go module `github.com/AmerDwight/network-skill-lab`、binary `nsl`、image `nsl/node`、環境變數前綴 `NSL_` |

## 2. 系統架構

### 2.1 元件

```
┌─────────────────────────────── Browser ────────────────────────────────┐
│  React + TS                                                            │
│  ┌──────────────┐ ┌──────────────────────────────┐ ┌────────────────┐  │
│  │ Task Panel   │ │ Terminal Workspace (dockview) │ │ Status Bar     │  │
│  │ 工單 / 文件  │ │ xterm.js × N，每個 tab 綁一個 │ │ timer, nodes,  │  │
│  │ checkpoints  │ │ node，可分割                  │ │ check / submit │  │
│  └──────────────┘ └──────────────────────────────┘ └────────────────┘  │
└──────────────┬──────────────────────────┬──────────────────────────────┘
               │ REST /api/*              │ WS /ws/attempts/{id}/term/{node}/{tab}
               │                          │ WS /ws/attempts/{id}/events
┌──────────────▼──────────────────────────▼──────────────────────────────┐
│  nsl (single Go binary, mode = all-in-one)                             │
│                                                                        │
│  Control Plane                          Runner (in-process, v1)        │
│  ├─ auth        本地帳密, admin/user     ├─ provider/docker             │
│  ├─ content     載入 content/ 目錄       ├─ provider/vm (v2, stub)      │
│  ├─ attempt     練習生命週期 + 計時       ├─ sandbox   建拓樸, 跑 setup   │
│  ├─ checker     背景跑 checkpoint 腳本   ├─ pty       docker exec 轉 WS │
│  ├─ recorder    指令歷史 + asciicast     └─ heartbeat 回收閒置沙箱       │
│  └─ store       SQLite                                                 │
│         ▲ Runner interface (Go interface, v2 換成 gRPC/WS 遠端實作)      │
└─────────────────────────────────┬──────────────────────────────────────┘
                                  │ Docker Engine API
┌─────────────────────────────────▼──────────────────────────────────────┐
│  一次練習 = 一個 sandbox                                                │
│    docker network per link（獨立 bridge，固定 subnet）                  │
│    node container × N：nsl/node image，privileged，systemd PID 1        │
│    k3s 節點 = 同一 image 內啟動 k3s service                             │
└────────────────────────────────────────────────────────────────────────┘
```

### 2.2 Runner 介面（v1 定介面，v2 才做遠端）

```go
type Runner interface {
    Capabilities() []Env               // {container, vm}
    Provision(ctx, spec SandboxSpec) (SandboxID, error)   // 建拓樸 + 跑 setup + precheck
    OpenTerminal(ctx, sb SandboxID, node string) (PTY, error)
    Exec(ctx, sb SandboxID, node string, cmd []string) (ExecResult, error)  // checker 用
    Destroy(ctx, sb SandboxID) error
    Health() RunnerHealth
}
```

控制面只透過這個介面碰沙箱。v2 的遠端 runner 會以 outbound WebSocket 連回控制面並實作同一組方法；成員自架的 VM runner 宣告 `Capabilities() = {container, vm}`。

### 2.3 為什麼 terminal 不受題目影響

pty 走 `docker exec`，不經過節點的網路堆疊。題目可以把節點所有介面關掉、DNS 全壞、iptables 全 DROP，使用者仍連得進去。Checker 同樣用 exec，所以驗證也不依賴節點網路。

## 3. 內容架構（本專案的核心）

### 3.1 兩個原語 + 一個組合

內容只有兩種基本單位，其他都是組合：

| 原語 | 是什麼 | 需要沙箱 |
|---|---|---|
| **Doc** | Markdown 文件。guide（概念教學）、reference（指令速查）、playbook（排查手冊，決策樹式） | 否 |
| **Lab** | 一個可執行的練習：拓樸 + setup + checkpoints + cases。從「手把手教學」到「面試題」都是 Lab，只是模式不同 | 是 |
| **Track** | 有序的學習路徑，把 Doc 與 Lab 串起來 | 組合 |

教學（teach / walkthrough）和練習題（practice case）**共用同一個 Lab 引擎**，差別在 `mode` 與 checkpoint 的揭露程度。這樣不用維護兩套執行邏輯。

### 3.2 難度階梯

| Level | 名稱 | Lab 模式 | 使用者看到什麼 |
|---|---|---|---|
| L1 | Tutorial 手把手 | `tutorial` | 每一步的說明 + 建議指令，做完自動打勾進下一步 |
| L2 | Guided 引導 | `guided` | 工單 + 所有 visible checkpoint 清單，背景偵測即時打勾 |
| L3 | Real 擬真 | `real` | 只有工單。無 checkpoint 顯示，按「提交」才驗證 |
| L4 | Randomized 隨機 | `real` + 多 case | 同 L3，但每次參數、故障點不同 |
| L5 | Compound 複合 | `real` + 多節點多故障 | 同 L3，多個故障疊加，需跨節點排查 |

L1 到 L3 是「同一份 Lab 用不同 mode 跑」也可以，作者可在 lab.yaml 宣告 `modes: [guided, real]` 允許哪些模式。

### 3.3 Checkpoint 偵測規則（D9 的具體解讀）

- `checker` 服務在沙箱存活期間每 `interval`（預設 5 秒）對每個 checkpoint 跑一次 `check` 腳本。
- **tutorial / guided**：通過即時顯示；全部通過自動結束並停錶。
- **real**：背景仍在偵測（用於記錄「其實何時修好的」），但 UI 不顯示。使用者按「提交」才揭露結果並停錶。提交後未全過可繼續，時間繼續累計。
- `visible: false` 的 checkpoint 在任何模式都不顯示描述，只在最終結果頁出現。

### 3.4 目錄結構

```
content/
  topics.yaml                 # 主題樹：net/ip, net/dns, net/netplan, k3s/pods, k3s/service ...
  docs/
    net/ip/guide.zh.md
    net/ip/guide.en.md
    net/ip/playbook-no-connectivity.zh.md
    ...
  labs/
    net-ip-01-link-down/
      lab.yaml
      topology.yaml
      setup.sh
      precheck.sh
      checks/
        01-link-up.sh
        02-ping-peer.sh
      solution.zh.md
      solution.en.md
      cases/
        default.yaml
        wrong-mtu.yaml
    k3s-pod-01-crashloop/
      ...
  tracks/
    network-basics.yaml
    k3s-troubleshooting.yaml
images/
  node/Dockerfile             # nsl/node 基底 image
```

全部是純文字、進 git。啟動時載入並驗證；提供 `nsl content lint` 指令給作者。

### 3.5 lab.yaml 規格

```yaml
id: net-ip-01-link-down
version: 1
title: { zh: "伺服器連不到對外", en: "Server lost connectivity" }
topic: net/ip
level: 2                      # 1..5
modes: [tutorial, guided, real]
environment: container        # container | vm
tags: [ip, link, ethtool]
estimated_minutes: 10
related_docs: [net/ip/guide, net/ip/playbook-no-connectivity]

ticket:                       # 使用者看到的工單，支援 {{param}} 模板
  zh: |
    使用者反映 {{node_a}} 無法連到 {{node_b}}（{{ip_b}}）。請找出原因並修復。
  en: |
    Users report {{node_a}} cannot reach {{node_b}} ({{ip_b}}). Find the cause and fix it.

params:                       # 依序產生，後面的可引用前面的
  subnet:  { gen: cidr, base: 10.0.0.0/8, prefix: 24 }
  ip_a:    { gen: ip_in, subnet: "{{subnet}}", index: 10 }
  ip_b:    { gen: ip_in, subnet: "{{subnet}}", index: 20 }
  node_a:  { gen: const, value: web01 }
  node_b:  { gen: const, value: db01 }
  iface:   { gen: choice, of: [eth1] }

cases:                        # 每個 case 覆寫 params 與 setup 變數，並可加權
  - { file: cases/default.yaml, weight: 3 }
  - { file: cases/wrong-mtu.yaml, weight: 1 }

precheck:
  script: precheck.sh
  retries: 3

checkpoints:
  - id: link-up
    title: { zh: "{{iface}} 已 UP", en: "{{iface}} is up" }
    node: web01
    script: checks/01-link-up.sh
    visible: true
  - id: ping-peer
    title: { zh: "web01 可 ping 到 db01", en: "web01 can ping db01" }
    node: web01
    script: checks/02-ping-peer.sh
    visible: true
    requires: [link-up]       # 可選，僅影響顯示順序與 tutorial 步驟

tutorial:                     # 只在 mode=tutorial 使用，每步對應一個 checkpoint
  - checkpoint: link-up
    instruction:
      zh: "先用 `ip link` 看介面狀態，找出 DOWN 的介面後用 `ip link set <iface> up`。"
      en: "Run `ip link` to inspect interfaces; bring the DOWN one up with `ip link set <iface> up`."
```

**腳本契約：**
- 所有腳本在指定 node 內以 root 執行，參數以環境變數注入（`NSL_SUBNET`, `NSL_IP_A`, ...）。
- `setup.sh`：把環境弄壞。冪等不強求，但失敗要 exit 非 0。
- `precheck.sh`：驗證產生出的環境符合題意（例如 ARP 題兩節點真的同網段、故障真的存在）。exit 0 通過。
- `checks/*.sh`：exit 0 = 通過。禁止有副作用。10 秒逾時。

### 3.6 topology.yaml 規格（刻意對齊 containerlab）

```yaml
nodes:
  web01: { image: nsl/node, role: ubuntu }
  db01:  { image: nsl/node, role: ubuntu }
  # k3s 範例： k3s01: { image: nsl/node, role: k3s-server }
  #           k3s02: { image: nsl/node, role: k3s-agent, server: k3s01 }
links:
  - endpoints: ["web01:eth1", "db01:eth1"]
    subnet: "{{subnet}}"
    addresses: { web01: "{{ip_a}}/24", db01: "{{ip_b}}/24" }
```

v1 Docker provider 的實作：每條 link = 一個 `--internal` bridge network；容器接上後由 setup 前的 bootstrap 把 Docker 配的位址交給 netplan / systemd-networkd 接管（見 §7 風險 R1）。未來 containerlab provider 可直接讀這份格式。

### 3.7 track.yaml

```yaml
id: network-basics
title: { zh: "網路基礎排查", en: "Network troubleshooting basics" }
steps:
  - { doc: net/ip/guide }
  - { lab: net-ip-01-link-down, mode: tutorial }
  - { lab: net-ip-01-link-down, mode: guided }
  - { doc: net/ip/playbook-no-connectivity }
  - { lab: net-ip-02-wrong-gateway, mode: real }
```

### 3.8 topics.yaml

```yaml
- id: net
  title: { zh: "網路", en: "Networking" }
  children:
    - { id: ip,      title: { zh: "介面與路由 (ip / ifconfig / route)", en: "Interfaces & routing" } }
    - { id: dns,     title: { zh: "DNS 與 resolved", en: "DNS & resolved" } }
    - { id: netplan, title: { zh: "netplan / networkd", en: "netplan / networkd" } }
    - { id: fw,      title: { zh: "iptables / nftables / ufw", en: "Firewall" } }
    - { id: capture, title: { zh: "tcpdump / tshark", en: "Packet capture" } }
    - { id: snmp,    title: { zh: "SNMP / LLDP", en: "SNMP / LLDP" } }
- id: k3s
  children: [pods, service, dns, node, storage, ingress]
```

主題樹只是分類與導覽用，不影響執行。

## 4. 沙箱節點 image

`nsl/node`：Ubuntu 24.04 + systemd 當 PID 1，預裝以下 66 個套件（使用者勾選定案）。

- net-core：iproute2, net-tools, ethtool, iputils-ping, iputils-arping, iputils-tracepath, traceroute, mtr-tiny, netcat-openbsd, socat, iperf3, telnet, nmap, arp-scan
- dns：bind9-dnsutils, systemd-resolved
- packet：tcpdump, tshark, iftop, conntrack
- fw：iptables, nftables, ufw
- cfg：netplan.io, systemd-networkd, network-manager, isc-dhcp-client, openssh-server, chrony, nginx, rsyslog
- snmp：snmp, snmpd, snmp-mibs-downloader, lldpd, snmptrapd
- http：curl, wget, openssl, ca-certificates, rsync
- k8s：k3s（含 kubectl / crictl / ctr）, helm, k9s, kustomize
- sys：procps, psmisc, lsof, strace, htop, sysstat, util-linux
- edit：vim, nano, less, jq, yq, tmux, tree, git, bash-completion, man-db, file, unzip, sudo, cron

Image 內建：
- `/etc/profile.d/nsl-history.sh`：`PROMPT_COMMAND` hook，把每條指令（時間、cwd、指令、exit code）寫到 `/var/log/nsl/commands.jsonl`，recorder 定期拉取。
- 預設 `network-manager` 停用、`systemd-networkd` + `netplan` 啟用；lab 可在 topology 的 role 選 `ubuntu-nm` 改用 NetworkManager。
- k3s binary 預裝但 service 不啟用；`role: k3s-server / k3s-agent` 的節點由 bootstrap 啟動。
- 預估大小 1.5 到 2 GB，k3s 相關 image 預先 `ctr images import` 進去避免練習時拉網路。

## 5. 資料模型（SQLite）

```
users            id, username, password_hash, role(admin|user), locale, created_at
attempts         id, user_id, lab_id, lab_version, case_id, mode, params_json,
                 status(provisioning|running|submitted|passed|abandoned|expired),
                 started_at, ended_at, elapsed_ms, runner_id, sandbox_id
checkpoint_runs  attempt_id, checkpoint_id, first_passed_at, last_status, last_run_at
command_log      attempt_id, node, ts, cwd, command, exit_code
recordings       attempt_id, node, tab_id, path(asciicast v2 file), started_at, ended_at
```

`elapsed_ms` 只計 running 狀態時間，provisioning 不算。

## 6. 練習生命週期

```
[選 lab + mode] → provisioning ──失敗→ error（顯示原因，可重試）
                      │  1. 選 case（加權隨機） 2. 產生 params
                      │  3. Provision 拓樸  4. bootstrap 節點
                      │  5. setup.sh  6. precheck.sh（失敗回 1，最多 3 次）
                      ▼
                   running ──── 瀏覽器 heartbeat 每 30s；斷 15 分鐘 → expired → Destroy
                      │  checker 每 5s 跑 checkpoints；recorder 每 5s 拉 command log
                      │
      guided/tutorial：全過 → passed        real：按提交 → submitted →（全過）passed
                      │                                    └（未全過）繼續 running
                      ▼
                   結束 → 顯示總時間、指令數、checkpoint 結果、解答 → Destroy
```

使用者可隨時「放棄」→ abandoned → Destroy。同一 user 只能有一個非終態的 attempt。

## 7. 已知風險與 Phase 0 驗證項目

| # | 風險 | 驗證方式 |
|---|---|---|
| R1 | Docker 配給容器的 IP 與 netplan / systemd-networkd 接管衝突 | 起兩個 systemd 容器，bootstrap 把 eth1 交給 networkd，確認 `netplan apply` 行為正常、Docker 不會搶回 |
| R2 | systemd 在 privileged 容器內的穩定性（journald、resolved、cgroup） | 同上，確認 `systemctl`、`journalctl -u`、`resolvectl status` 可用 |
| R3 | k3s server + agent 兩節點在容器內組叢集、flannel VXLAN 跨容器通 | 兩容器起 k3s，跨節點 Pod 互 ping |
| R4 | 練習中把介面全關會不會影響 docker exec / checker | 關掉所有介面後 exec 仍可用（理論上可，實測確認） |
| R5 | image 大小與啟動時間 | 量測：目標冷啟動 < 30 秒（含 k3s） |
| R6 | 安全：privileged 容器 = host root。v1 只在信任網路使用；正式上線時沙箱主機必須與控制面分離（v2 遠端 runner） | 設計面已隔離，記錄為上線前置條件 |

Phase 0 只做驗證腳本，不寫正式程式碼，結果回寫本文件。

## 8. 專案結構（Go + React）

```
cmd/nsl/                main：all-in-one 模式
internal/
  auth/  content/  attempt/  checker/  recorder/  store/
  runner/               Runner interface + in-process 實作
  provider/docker/      Docker provider
  provider/vm/          v2 stub
  api/                  REST + WS handlers
web/                    React + TS + Vite + xterm.js + dockview
content/                §3.4
images/node/            Dockerfile
docs/                   本文件與後續 ADR
```

## 9. Roadmap

| Phase | 內容 | 完成定義 |
|---|---|---|
| 0 | 風險驗證（§7） | R1 到 R5 有實測結果 |
| 1 | 核心：all-in-one binary、Docker provider、terminal 多分頁、timer、checker、SQLite、1 個示範 lab | 能從瀏覽器完整跑完一題 guided 模式 |
| 2 | 內容系統：docs 面板、tracks、tutorial / real 模式、params 隨機化、precheck、`content lint` | 5 個 lab（net × 3、k3s × 2）+ 對應 guide |
| 3 | 帳號 admin/user、歷史紀錄頁、錄製回放 | 多帳號可各自練習並回看 |
| 4 | 遠端 runner、VM provider | 成員自架 runner 可接上 |
| 5 | 面試模式、網路裝置（containerlab + FRR）、接真實裝置 | 另行設計 |

## 10. 待你確認的設計判斷

1. 教學（tutorial）與練習題共用 Lab 引擎，用 `mode` 區分，而不是分成兩種內容格式。
2. Checkpoint 偵測規則採 §3.3 的解讀：guided 背景偵測即時顯示，real 背景偵測但不顯示、按提交揭露。
3. Phase 0 先做風險驗證腳本再進 Phase 1。
4. params 用 YAML 內建產生器（cidr / ip_in / choice / int / const），複雜情況交給 precheck 把關，不做表達式語言。
