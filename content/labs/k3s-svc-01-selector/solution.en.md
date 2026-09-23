# Solution: the service selects no pod (or selects it and targets the wrong port)

The lab has two variants and one route through them: start from whether the service has endpoints and work backwards.

## 1. Confirm the pod itself is healthy

Work on `k3s01`.

```
kubectl -n <namespace> get pods -o wide
kubectl -n <namespace> logs -l app=<app>
```

The pod is `Running` on `k3s02`. **Do not move it to k3s01**: cross-node traffic is supposed to work, and moving the pod only hides the fault (a hidden checkpoint watches for exactly that).

## 2. Look for endpoints

This is the fork in the road for every service problem:

```
kubectl -n <namespace> get endpointslices -l kubernetes.io/service-name=<app>
kubectl -n <namespace> describe svc <app>
```

- **No endpoint**: the selector matches no pod (first variant).
- **An endpoint, but still no answer**: the selector is fine, go and check the ports (second variant).

## 3a. No endpoint: the selector does not match the labels

Put both strings side by side:

```
kubectl -n <namespace> get svc <app> -o jsonpath='{.spec.selector}'
kubectl -n <namespace> get pods --show-labels
```

The service selects `app: <app>-web` while the pod is labelled `app: <app>`. The extra `-web` matches nothing. Endpoints are produced by the controller from that selector, so without a match there is no endpoint, kube-proxy has nowhere to send the connection and the client sees connection refused.

```
kubectl -n <namespace> patch svc <app> --type=merge \
  -p='{"spec":{"selector":{"app":"<app>"}}}'
```

The endpointslice appears within seconds of fixing the selector.

## 3b. An endpoint but no answer: targetPort is off by one

```
kubectl -n <namespace> get svc <app> -o jsonpath='{.spec.ports}'
kubectl -n <namespace> get pods -o jsonpath='{.items[0].spec.containers[0].ports}'
```

`port` is what clients connect to; `targetPort` is the port the packet is delivered to inside the container. With `targetPort` one above the port the container listens on, kube-proxy still forwards to the pod, nothing is listening there, and the answer is connection refused. An endpoint existing does not mean the path works.

```
kubectl -n <namespace> patch svc <app> --type=json \
  -p='[{"op":"replace","path":"/spec/ports/0/targetPort","value":<port>}]'
```

## 4. Verify

```
kubectl -n <namespace> get svc <app>
curl -s --max-time 3 http://<clusterIP>:<port>/
```

`ok` means you are done. Cluster DNS works as well: `curl http://<app>.<namespace>.svc.cluster.local:<port>/`.

## Dead ends

- Relabelling the pod to match the service: it works, but it breaks the naming convention; fix the selector instead.
- Switching the service to `NodePort` or `type: LoadBalancer`: the ClusterIP path stays broken.
- Deleting the pod: labels and ports come from the same template, so nothing changes.
