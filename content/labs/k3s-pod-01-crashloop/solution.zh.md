# 解答：容器的 command 執行完就結束

Deployment 的容器被設成 `sh -c "echo booting; exit 1"`。容器的主行程印完一行就退出，Pod 跟著結束，kubelet 依退避時間不斷重建，於是 `RESTARTS` 一直加、`STATUS` 停在 `CrashLoopBackOff`。

## 1. 分清楚是哪一種「起不來」

在 `k3s01` 上操作，kubeconfig 已經設好。

```
kubectl -n <namespace> get pods
```

- `CrashLoopBackOff`：容器有起來但一直退出 —— 看日誌與 command。
- `ImagePullBackOff` / `ErrImagePull`：映像拉不到 —— 看映像名稱與 `imagePullPolicy`。
- `Pending`：排不進任何節點 —— 看 `describe pod` 的排程事件、資源與 nodeSelector。

## 2. 看事件確認容器真的執行過

```
kubectl -n <namespace> describe pod -l app=<app>
```

`Events` 出現 `Started container` 接著 `Back-off restarting failed container`，代表問題在容器裡面，不是排程或映像。

## 3. 看上一次的日誌

容器當下已經死了，`logs` 會抓不到東西，要加 `--previous`：

```
kubectl -n <namespace> logs -l app=<app> --previous
```

只有一行 `booting`，沒有錯誤訊息 —— 程式是自己正常結束的，不是被殺掉的。

## 4. 看 command

```
kubectl -n <namespace> get deploy <app> -o jsonpath='{.spec.template.spec.containers[0].command}'
```

`["sh","-c","echo booting; exit 1"]`。把一次性指令當成常駐服務跑，就會得到這個結果。

## 5. 修好

```
kubectl -n <namespace> patch deploy <app> --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]'
```

或用 `kubectl -n <namespace> edit deploy <app>` 改同一個欄位。改 Deployment 而不是直接改 Pod：Pod 是 ReplicaSet 生出來的，直接改 Pod 下次重建就沒了。

## 6. 驗證

```
kubectl -n <namespace> rollout status deploy/<app>
kubectl -n <namespace> get deploy <app>
```

這個 Deployment 設了 `minReadySeconds: 10`，Pod 要連續 Running 十秒才算可用，所以 `READY` 補滿會比 Pod 變成 Running 晚十幾秒。看到 `successfully rolled out`、`READY` 等於期望副本數就完成了。

## 常見的岔路

- `kubectl delete pod` 重建：新 Pod 用的還是同一份 template，一樣會 crash。
- 改 `restartPolicy`：Deployment 的 Pod template 只允許 `Always`，改不動。
- 加 `replicas`：多開幾個一樣壞的 Pod，`availableReplicas` 還是 0。
