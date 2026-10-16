# 解答：介面被關閉

1. 在 `web01` 上列出所有介面狀態：

   ```
   ip -br link show
   ```

2. 找到狀態為 `DOWN` 的資料介面（`eth1`），`eth0` 是管理網路，不要動它。

3. 把它拉起來：

   ```
   ip link set eth1 up
   ```

4. 確認位址還在，然後測試與 `db01` 的連線：

   ```
   ip -br addr show eth1
   ping -c 1 10.0.5.20
   ```
