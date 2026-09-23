# 解答：Service 選不到 Pod（或選到了卻打錯埠）

這題有兩種變化，排查路線一樣：從 endpoint 有沒有出現開始往回推。

## 1. 先確認 Pod 本身是好的

在 `k3s01` 上操作。

```
kubectl -n <namespace> get pods -o wide
kubectl -n <namespace> logs -l app=<app>
```

Pod 是 `Running`、跑在 `k3s02` 上。**不要把 Pod 搬到 k3s01**，跨節點本來就該通，搬走只是把問題藏起來（這題有一個隱藏檢查點在看這件事）。

## 2. 看 Service 有沒有 endpoint

這是 Service 排查的分水嶺：

```
kubectl -n <namespace> get endpointslices -l kubernetes.io/service-name=<app>
kubectl -n <namespace> describe svc <app>
```

- **沒有 endpoint**：selector 選不到任何 Pod（第一種變化）。
- **有 endpoint 但連不上**：selector 沒問題，往埠號查（第二種變化）。

## 3a. 沒有 endpoint：selector 對不上標籤

把兩邊的字串擺在一起看：

```
kubectl -n <namespace> get svc <app> -o jsonpath='{.spec.selector}'
kubectl -n <namespace> get pods --show-labels
```

Service 選的是 `app: <app>-web`，Pod 的標籤是 `app: <app>`。多了 `-web` 就什麼都選不到。endpoint 是 controller 依 selector 比對出來的，比對不到就不會有 endpoint，連線在 kube-proxy 那層直接被拒絕，所以看到的是 connection refused。

```
kubectl -n <namespace> patch svc <app> --type=merge \
  -p='{"spec":{"selector":{"app":"<app>"}}}'
```

selector 一改好，endpointslice 幾秒內就會出現。

## 3b. 有 endpoint 卻連不上：targetPort 差一號

```
kubectl -n <namespace> get svc <app> -o jsonpath='{.spec.ports}'
kubectl -n <namespace> get pods -o jsonpath='{.items[0].spec.containers[0].ports}'
```

`port` 是客戶端連的埠，`targetPort` 是封包真正送到容器的埠。`targetPort` 比容器實際監聽的埠多一號，kube-proxy 照樣把流量送進 Pod，但那個埠沒有人在聽，於是 connection refused。endpoint 存在不代表通。

```
kubectl -n <namespace> patch svc <app> --type=json \
  -p='[{"op":"replace","path":"/spec/ports/0/targetPort","value":<port>}]'
```

## 4. 驗證

```
kubectl -n <namespace> get svc <app>
curl -s --max-time 3 http://<clusterIP>:<port>/
```

回 `ok` 就完成了。也可以用名稱驗證叢集 DNS：`curl http://<app>.<namespace>.svc.cluster.local:<port>/`。

## 常見的岔路

- 改 Pod 的標籤去配合 Service：能通，但把命名慣例搞亂了，正解是改 Service 的 selector。
- 把 Service 改成 `NodePort` 或 `type: LoadBalancer`：ClusterIP 那層還是壞的。
- 刪掉 Pod 重建：標籤與埠都來自同一份 template，重建沒有任何幫助。
