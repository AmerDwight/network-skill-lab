# 用 kubectl 排查 Pod 與 Service（k3s）

k3s 的 kubeconfig 放在 `/etc/rancher/k3s/k3s.yaml`，server 節點開機時就寫好了。互動 shell 由 `/etc/profile.d/nsl-kubeconfig.sh` 設好 `KUBECONFIG`，腳本裡若沒有就自己來一次：

```
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl get nodes -o wide
```

agent 節點沒有 kubeconfig，也不該有：所有 `kubectl` 都在 server 上下。

## 先看全貌

```
kubectl get nodes
kubectl get pods -A
kubectl -n <ns> get pods -o wide          # 多看 NODE 與 Pod IP 兩欄
kubectl -n <ns> get all
```

`-o wide` 的 `NODE` 欄是跨節點問題的第一個線索：Pod 到底排到哪台。

## Pod 的狀態怎麼讀

| STATUS | 意思 | 往哪查 |
|---|---|---|
| `Running` | 容器在跑 | 不一定健康，還要看 READY 欄（`1/1` 還是 `0/1`） |
| `CrashLoopBackOff` | 起得來但一直退出 | `logs --previous`、容器的 command |
| `ImagePullBackOff` / `ErrImagePull` | 拉不到映像 | 映像名稱、`imagePullPolicy`、離線環境有沒有這個映像 |
| `Pending` | 沒有節點收 | `describe pod` 的排程事件、nodeSelector、資源 |
| `Completed` | 主行程正常結束 | 一次性指令被當成服務跑 |
| `Terminating` | 正在收尾 | finalizer、`terminationGracePeriodSeconds` |

`RESTARTS` 一直加就是容器反覆退出，kubelet 的重試間隔是 10 秒、20 秒、40 秒……最長 5 分鐘。

## describe 與 events

```
kubectl -n <ns> describe pod <pod>
kubectl -n <ns> describe pod -l app=<app>
kubectl -n <ns> get events --sort-by=.lastTimestamp
```

`describe` 最下面的 `Events` 是最有用的一段：`Scheduled` 有沒有出現（排程過了沒）、`Pulled` 或 `Failed to pull`（映像）、`Started container` 後面跟著 `Back-off restarting failed container`（容器自己死掉）。

## 日誌

```
kubectl -n <ns> logs <pod>
kubectl -n <ns> logs -l app=<app> --tail=50
kubectl -n <ns> logs <pod> -c <container>     # 多容器的 Pod
kubectl -n <ns> logs <pod> --previous         # 上一次退出的那個容器
kubectl -n <ns> logs -f <pod>
```

**`--previous` 是 CrashLoopBackOff 的關鍵**：當下的容器已經死了，不加這個參數什麼都看不到。

## 改東西：patch 與 edit

```
kubectl -n <ns> edit deploy <app>

kubectl -n <ns> patch deploy <app> --type=merge \
  -p='{"spec":{"replicas":2}}'

kubectl -n <ns> patch deploy <app> --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]'
```

`--type=merge` 適合整塊欄位，`--type=json` 適合精準改陣列裡的某一個元素。**改 Deployment，不要改 Pod**：Pod 是 ReplicaSet 依 template 生出來的，直接改 Pod 下次重建就沒了。

## rollout

```
kubectl -n <ns> rollout status deploy/<app>
kubectl -n <ns> rollout status deploy/<app> --timeout=10s
kubectl -n <ns> rollout history deploy/<app>
kubectl -n <ns> rollout undo deploy/<app>
kubectl -n <ns> get deploy <app> -o jsonpath='{.status.availableReplicas}'
```

`READY` 是「就緒的副本 / 期望的副本」，`availableReplicas` 還多要求 Pod 連續就緒滿 `minReadySeconds` 秒。設了 `minReadySeconds` 的 Deployment，Pod 變成 Running 之後還要等那幾秒才算可用，這不是卡住。

## Service 與 endpoint

Service 靠 **selector 比對 Pod 的標籤**產生 endpoint，沒有 endpoint 就沒有任何後端：

```
kubectl -n <ns> get svc
kubectl -n <ns> describe svc <app>
kubectl -n <ns> get svc <app> -o jsonpath='{.spec.selector}'
kubectl -n <ns> get pods --show-labels
kubectl -n <ns> get endpointslices -l kubernetes.io/service-name=<app>
kubectl -n <ns> get endpointslices -l kubernetes.io/service-name=<app> -o yaml
```

`endpointslices` 是新版的 endpoint 表（`kubectl get endpoints` 仍可用，但已被取代）。判斷順序固定：

1. **沒有 endpoint** → selector 與標籤對不上，或 Pod 還沒 Ready。
2. **有 endpoint 但連不上** → `targetPort` 跟容器實際監聽的埠不一樣，或應用根本沒 listen。

`port` 是客戶端連的埠，`targetPort` 是封包送進容器的埠，兩者可以不同，但 `targetPort` 一定要對上容器真正在聽的埠。

## 從節點驗證連線

```
kubectl -n <ns> get svc <app> -o jsonpath='{.spec.clusterIP}'
curl -s --max-time 3 http://<clusterIP>:<port>/
curl -s --max-time 3 http://<app>.<ns>.svc.cluster.local:<port>/
kubectl -n <ns> get pods -o wide                    # 拿 Pod IP
curl -s --max-time 3 http://<podIP>:<port>/
```

先打 Pod IP 再打 ClusterIP，可以把「應用沒起來」跟「Service 設定錯」分開：Pod IP 通、ClusterIP 不通，問題百分之百在 Service。

## 其他常用的

```
kubectl -n <ns> get deploy <app> -o yaml
kubectl -n <ns> exec -it <pod> -- sh
kubectl -n <ns> delete pod <pod>                    # 重建一個，template 不變
kubectl api-resources
kubectl -n kube-system get pods                     # CoreDNS 等系統元件
```
