# Phase 1 核心引擎：驗收結果

> 日期：2026-09-23。Spec：`docs/specs/phase1.html`。Task：issue #4 到 #13（T1 到 T10），PR #14 到 #25。
> 驗收環境：WSL2、Docker Engine 29.8、image `nsl/node`（Phase 0 版）。

## 1. §01 完成定義逐條

| # | 條目 | 結果 | 實測 |
|---|---|---|---|
| 1 | 單一 binary 含前端，不需登入 | 通過 | `bin/nsl` 20 MB，`nsl serve` 即用，預建 `local` 使用者 |
| 2 | 開始後 15 秒內看到兩節點 terminal | 通過 | provisioning 2.7 s（containers 0.1 s → bootstrap 2.0 s → setup 2.6 s），terminal WebSocket 即時開啟 |
| 3 | 修好後 5 秒內打勾、自動停錶、結果頁、沙箱銷毀 | 通過 | 送出修復指令後 2.8 s 兩個 checkpoint 同輪 pass → `passed`，`elapsed_ms` 5084，`command_count` 2，沙箱在 4 秒內清空 |
| 4 | 放棄、閒置逾時、伺服器重啟都回收 | 通過 | 放棄 2.3 s 回收；閒置（測試用 20 s）→ `expired` 且容器消失；SIGTERM 後重啟 → Recover 標記 1 筆 expired、GC 清掉 2 個容器 |
| 5 | CI 綠燈、`make test-integration` 通過 | 通過 | main 每個 merge 的 CI 皆綠；本機 lint 0 issues、Go 10 個 package + 90 個前端測試、整合測試 e2e 10.8 s / provider 4.9 s |
| 6 | 指令歷史與 pty 錄影落地 | 通過 | `command_log` 2 筆；`recordings/<attempt>/web01-t1.cast` 為 asciicast v2，含 resize 與輸出事件 |

**瀏覽器實測**：第 2、3 條的視覺部分（xterm.js、dockview 分頁、checkpoint 即時打勾、結果頁）由使用者在 Windows 瀏覽器確認；planner 端無瀏覽器，以 HTTP 與 WebSocket 驅動同一套契約完成上表。

## 2. 驗收過程抓到的問題

| 問題 | 處理 |
|---|---|
| `command_count` 為 0：attempt 一判 passed 就背景銷毀沙箱，recorder 的最後一次拉取跟銷毀賽跑輸掉。T7 的整合測試曾以「先等指令落地再送修復」繞過它 | PR #25：attempt 結束時先跑 before-destroy hook（5 秒上限），recorder 用 hook 做最後拉取，之後才廣播終態事件與銷毀。整合測試改成不等待，斷言 `command_count >= 2` |
| 500 回應把內部錯誤字串送給客戶端 | T7 審查抓到，改為固定訊息 `internal error`，細節只進 log |
| terminal 連線被取代時舊的 pty 沒關 | T7 的 CI 第一輪抓到，修正並加回歸測試 |
| `TestTerminalReplacesPreviousConnection` 在 CI 偶發逾時 | issue #24 追蹤，本機穩定 |

## 3. 與 spec 的偏離（planner 採納）

- 前端產物在 `internal/web/dist`（`go:embed` 不能引用上層目錄）。
- `setup.sh` 在每個 node 各執行一次、帶 `NSL_NODE`（§05 澄清）。
- `dockview-react` 與 `dockview` 兩個套件（8.x 拆包）。
- Topology 的 node 為 map，provider 以名稱排序決定建立順序。
- k3s 單元改 `--no-block` 啟動（Phase 0 結論），Phase 1 的 provider 已支援 k3s role 但未測。
- 新增的 API 錯誤碼：`attempt_finished`、`attempt_provisioning`、`unknown_node`、`method_not_allowed`。
- `attempt.View` 只含 `visible: true` 的 checkpoint；隱藏 checkpoint 出現在結果頁的需求（D9）留 Phase 2 處理。

## 4. 流程觀察

- 10 個 task、1 個修正，全部由 Opus 開發、Haiku 審查、planner 裁決；每個 PR 的 CI 都在第一或第二輪綠燈。
- Haiku 的審查在正確性上抓到 2 個真問題（500 訊息外洩、以及間接促成 pty 關閉修正），但 3 次把「顏色寫死」標成 blocker、1 次把 Go 1.22 後已無的迴圈變數問題標成 blocker。之後審查提示已加入嚴重度校準。
- 一個工作天內從 T1 走到驗收完成。

## 5. 已知限制與後續

- 沒有指令歷史的 API 端點，`/result` 只回 `command_count`；歷史頁是 Phase 3。
- terminal 分頁被取代時，若舊連線已開錄影，極短窗口內可能寫到新錄影檔（同路徑）；Phase 3 做回放前處理。
- 卡住不讀的客戶端要 5 秒才被斷線（coder/websocket 的 close 交握逾時）。
- 前端 bundle 超過 500 kB（xterm + dockview + react-markdown），未做 code splitting。
- Ubuntu 節點在 `ip link set down` 後 networkd 會清掉位址、UP 後配回，這是真實行為，checkpoint 腳本已對應。
- 這台機器的 8080 被別的服務佔用，README 建議 `NSL_LISTEN=:8081`。
