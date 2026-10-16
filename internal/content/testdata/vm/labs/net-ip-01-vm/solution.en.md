# Solution: the interface was shut down

1. On `web01`, list the interface states:

   ```
   ip -br link show
   ```

2. Spot the data interface that is `DOWN` (`eth1`); `eth0` is the management network, leave it alone.

3. Bring it back up:

   ```
   ip link set eth1 up
   ```

4. Confirm the address is still configured, then reach `db01`:

   ```
   ip -br addr show eth1
   ping -c 1 10.0.5.20
   ```
