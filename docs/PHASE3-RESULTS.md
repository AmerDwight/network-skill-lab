# Phase 3 帳號與歷史：驗收結果

> 日期：2026-09-24。Spec：`docs/specs/phase3.html`。Task：issue #60 到 #65（T1 到 T6）。PR #66 到 #72。
> 驗收環境：WSL2、Docker Engine 29.8、image `nsl/node`（含 k3s 路由單元與 udev 等待）。

## 1. §01 完成定義逐條

| # | 條目 | 結果 | 實測 |
|---|---|---|---|
| 1 | 多人各自練習 | 通過 | alice 與 bob 同時登入、同時開 net-ip-01，兩個沙箱並存，各自 10 s 進 running、15.7 s passed；bob 讀 alice 的 attempt 404、歷史只有自己那筆、cast 404 |
| 2 | 未登入一律拒絕 | 通過 | 匿名 `/api/labs` 401、`/ws/attempts/*/events` 在 upgrade 前 401、`/api/health` 200；登出後 `me` 401；cookie HttpOnly + SameSite=Lax + Max-Age 7 天 |
| 3 | admin 管帳號 | 通過 | `nsl user add` 建立 root / alice / bob / carol；admin API 停用 bob 後 bob 的下一個請求 401、再登入 403 `user_disabled`；admin 改自己 → `cannot_modify_self` |
| 4 | 歷史可回看 | 通過 | 歷史列出 lab / 狀態 / 提交次數 / 指令數；錄影端點回 875 bytes 的 asciicast v2、cast 端點 text/plain；指令端點列出兩條修復指令與 exit code；前端播放器原生處理 resize 事件、獨立 lazy chunk 190 kB |
| 5 | admin 看得到所有人 | 通過 | `history?user_id=alice` 回 alice 的紀錄；admin 讀 alice 終態 attempt 的 result 與 cast 200；admin 開 alice 的 events socket 404 |
| 6 | 併發上限 | 通過 | `NSL_MAX_SANDBOXES=2` 時 carol 第三個開題 429 `runner_busy`，回應含 `sandboxes_active: 2, sandboxes_max: 2`；admin 強制放棄 carol 後 current 204 |
| 7 | 穩健性與回歸 | 通過 | #57 GC 依 instance 標籤、不致命，`nsl gc` 只清自己的；#56 systemd 60 s；套件全綠（見 §2） |

**瀏覽器實測**：登入頁、admin 頁、歷史頁與播放器的視覺部分由使用者用兩個瀏覽器（一般與無痕）確認；planner 端無瀏覽器，以 cookie jar 驅動同一套契約完成上表。

## 2. 套件

`make lint`、`make test`（Go 13 個 package、前端 270 個測試）、`make test-integration`（2 m 17 s）、`make test-content`（2 m 4 s，五個 lab）在最終 main（含 #72）全綠。

## 3. 驗收與開發過程抓到的問題

| 問題 | 處理 |
|---|---|
| 登出未清使用者範圍的快取，共用電腦會殘留上一個人的資料 | #67 修正：`resetUserScopedStores()` |
| 凍結 fixture 固定網段，兩個沙箱同時開同一題撞 Docker pool | #70：fixture 網段改隨機（仍無 cases、兩個 checkpoint） |
| 停用帳號後 session 不會自動失效 | #70：admin 停用時刪除該使用者所有 session |
| `no_users` 判斷不能用 users 總數（永遠有停用的 local） | #70：`Users.CountEnabled` |
| 沙箱拆除時 mgmt 網路偶爾殘留 | #71 追蹤 |
| T3 加認證與 415 規則後，內容套件與 provider lifecycle 測試未同步（沒登入、沒 JSON header、寫死 10.0.5.x） | 驗收時抓到，修正 PR 讓內容 harness 登入並從 fixture 推導位址；教訓：跨層 task 的驗證清單要包含 `make test-content` |
| udevd 在高負載進 failed | Phase 2 尾聲 #58 已修，本 phase 的 24 節點測試未再出現 |

## 4. 與 spec 的偏離（planner 採納）

- `GET /api/admin/attempts` 與 `me` 的 `no_users` 碼由 T4 定稿後補進 §06。
- 停用後的下一個請求回 401 `unauthorized`（session 已刪）而非 `user_disabled`；直接停用但 session 尚在的路徑另有測試。
- commands 預設 500、上限 2000；歷史 50 / 200。
- `--password-stdin` 去掉全部尾端換行。
- 進行中的 attempt 只有本人能操作；admin 只有強制放棄與終態後的讀取。
- k3s 的 cluster / service CIDR 路由由 image 內的 systemd 單元加，lab 不再自己加。

## 5. 流程觀察

- 6 個 task 加 1 個修補，Opus 開發、Haiku 審查；審查抓到 1 個值得修的（登出清快取），其餘 findings 為 nit 或誤判並駁回。
- 三個 Opus 平行時 Docker daemon 上的沙箱互不干擾，#57 的 instance 標籤讓驗收伺服器可以與測試套件同時跑。
- Phase 3 從 spec 批准到驗收完成約一個工作天。

## 6. 已知限制與後續

- #71 殘留網路；錄影清理策略與配額（Phase 4 前置）。
- 前端主 bundle 1.26 MB（未 code splitting）。
- 沒有忘記密碼與自助註冊（D6，刻意）。
- HTTPS 由反向代理終結，README 有範例。
