# Phase 2 內容系統：驗收結果

> 日期：2026-09-23。Spec：`docs/specs/phase2.html`。Task：issue #26 到 #35（T1 到 T10）與修補 #39、#43、#44、#52、#55。PR #36 到 #58。
> 驗收環境：WSL2、Docker Engine 29.8、image `nsl/node`（含 Phase 2 的 bootstrap 更新）。

## 1. §01 完成定義逐條

| # | 條目 | 結果 | 實測 |
|---|---|---|---|
| 1 | 不改程式碼新增內容 | 通過 | 現場寫最小 lab `net-ip-02-no-address`（lab.yaml、topology.yaml、setup、一個 check、雙語 solution）放進 `content/labs/`，lint 通過，重啟後出現在 `/api/labs?topic=net/ip`，guided 模式跑到 passed；驗收後移除 |
| 2 | 三種模式都實測 | 通過 | tutorial：`tutorial_steps` 三步、checkpoint 依序打勾、7.7 s passed。real：attempt payload `checkpoints_hidden: true`、提交揭露 3 個 visible 的結果、修好後第二次提交 passed、`submit_count` 2。guided：五個 lab 都在 `make test-content` 跑到 passed |
| 3 | 隨機化與 precheck 重生 | 通過 | 連開三次 net-ip-01，網段各不同（例如 10.226.31.0/24、10.80.190.0/24）、case 加權抽取（3:1）。故意永遠失敗的 precheck：三次重生（事件 precheck 2、3 之間重新建網路與容器），最後 `error`，訊息含節點與 stderr，`/api/attempts/current` 回 204 |
| 4 | lint 守門 | 通過 | 故意弄壞的內容目錄（缺 solution、topic 不在樹、check 語法錯、requires 指向不存在、track mode 不合法）一次列出全部錯誤並 exit 1；CI 的 `lint-content` job 是 main 的必要檢查 |
| 5 | docs 與 tracks | 通過 | `/api/topics` 計數含子孫、`/api/docs` 三篇雙語、`/api/tracks` 兩條；標記文件已讀回 204；network-basics 在 tutorial 通過與文件已讀後顯示 3/6 完成 |
| 6 | k3s 可用 | 通過 | k3s-pod-01 guided：provisioning 顯示 `k3s` 步驟，Node Ready 8 s，進 running 33 s（含 precheck 等 crashloop 出現），修好後 15 s 判 passed。k3s-svc-01 在內容套件中 Ready 14 到 18 s（雙節點） |
| 7 | Phase 1 回歸、#24 修掉 | 通過 | Phase 1 的 e2e 套件持續綠；#24 找到根因（取代 terminal 連線時 `CloseNow` 等待舊連線的關閉交握）真修，`-count=50` 穩定 |

## 2. 交付內容

- 引擎：params 產生器（cidr / ip_in / choice / int / const，seed 可重現）、cases 加權、precheck 重生迴圈、隱藏 checkpoint、requires、tutorial 步驟、`internet` 旗標、三種模式語意、submit、progress、docs / topics / tracks 載入、`nsl content lint` 與 CI job、k3s role 選項與 Node Ready 進度、KUBECONFIG 注入。
- API：§09 補充的全部端點與 JSON 形狀。
- Web：主題樹列表、lab 詳情、docs 頁、tracks 頁、練習頁的文件面板、tutorial 步驟、real 提交、provisioning 新步驟、結果頁的提交次數與隱藏標記。
- 內容：五個 lab（net-ip-01 升級、net-route-01、net-dns-01、k3s-pod-01、k3s-svc-01）、四份雙語 guide、兩條 track、`topics.yaml`。
- 測試：引擎測試改用 `internal/content/testdata` 凍結 fixture（#44）；出貨內容由 `make test-content`（build tag `content_integration`）逐一實跑。

## 3. 驗收過程抓到的問題

| 問題 | 處理 |
|---|---|
| 引擎測試拿出貨內容當 fixture，內容一升級倒 50 個測試 | #44：凍結 testdata + `contenttest` helper，規則寫進 CLAUDE.md |
| Docker `--internal` 網路的 `DOCKER-INTERNAL` 規則加上 br_netfilter 會丟掉經 gateway 節點轉發的封包；改非 internal 後又有 raw 表反欺騙規則與主機路由外洩兩層問題 | #53：link 網路用 `inhibit_ipv4` + `gateway_mode_ipv4=routed` + 關 masquerade，`GwPriority` 固定預設路由在 mgmt |
| `internet: false` 時 k3s 節點沒有預設路由，ClusterIP 不可達 | k3s-svc-01 的 setup 加 service CIDR 路由；#50 追蹤移進 bootstrap |
| 高負載下 udevd 三個單元進 failed，`netplan apply` 在 bootstrap 失敗 | #58：bootstrap 救回 udevd、等 `udevadm control --ping`、netplan 重試三次；provider 在 degraded 時記錄 failed unit。根本原因疑為主機 `fs.inotify.max_user_instances=128`（#59） |
| 高負載下 systemd 30 秒內未就緒 | #56：放寬並回報最後狀態 |
| 啟動 GC 遇到「網路仍有 endpoint」就讓 `nsl serve` 退出；GC 也會清掉其他 nsl 行程的沙箱 | #57：GC 不致命、依 instance 標籤限定範圍 |
| lint 把驗證失敗的 lab 整個排除，錯誤只間接透過 track 顯示 | #54 |
| k3s 單節點測試偶發「修好前 check 就通過」 | #48 |
| ticket 模板不能引用 case 新增的 param | #39（已修） |

## 4. 與 spec 的偏離（planner 採納）

- k3s lab 至少要一條 link（沒有 link 的 k3s 節點會等 networkd-wait-online 逾時），單節點題用一個 ops 節點陪跑。
- k3s lab 的 subnet 從 10.128.0.0/9 取，避開 k3s 的 10.42 / 10.43。
- 內容 precheck 內部等待上限 24 s（引擎的 precheck 逾時 30 s）。
- `k3s: {}` 表示不關任何元件；只有完全沒有 `k3s` key 才套用預設關閉 traefik 與 metrics-server。
- Docker 保留每個網路的 .1，lab 的 gateway 位址用 index 2。
- `topics.yaml` 只接受完整 mapping 形式（DESIGN §3.8 的字串簡寫不支援）。
- 引擎測試與內容測試分成兩個 build tag：`integration`（凍結 fixture）與 `content_integration`（出貨內容）。

## 5. 流程觀察

- 10 個 task 加 6 個修補，Opus 開發、Haiku 審查；審查抓到 1 個真問題（500 外洩）與多個正確的驗證，誤判率比 Phase 1 低（嚴重度校準有效）。
- 兩個內容 task 撞到 Opus 額度上限中斷，worktree 保留了全部工作，重置後續跑無損。
- 最大的教訓：同一台 Docker daemon 上多個 nsl 行程（驗收伺服器與平行測試）會互相 GC，#57 修好前驗收要序列化。
- 時間：Phase 2 從 spec 批准到驗收完成約半個工作天。

## 6. 已知限制與後續

- #48、#50、#54、#56、#57、#59 如上。
- 前端 bundle 超過 500 kB，未做 code splitting。
- 沒有指令歷史端點與回放（Phase 3）。
- `ubuntu-nm` role 仍未被任何 lab 使用。
