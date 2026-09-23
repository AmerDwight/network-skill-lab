# Troubleshooting pods and services with kubectl (k3s)

k3s writes its kubeconfig to `/etc/rancher/k3s/k3s.yaml` on the server node at boot. Interactive shells get `KUBECONFIG` from `/etc/profile.d/nsl-kubeconfig.sh`; in a script that does not source the profile, set it yourself:

```
export KUBECONFIG=/etc/rancher/k3s/k3s.yaml
kubectl get nodes -o wide
```

Agent nodes have no kubeconfig and should not have one: run every `kubectl` on the server.

## Start with the wide picture

```
kubectl get nodes
kubectl get pods -A
kubectl -n <ns> get pods -o wide          # NODE and pod IP columns
kubectl -n <ns> get all
```

The `NODE` column of `-o wide` is the first clue in any cross-node problem: it says where the pod actually landed.

## Reading pod status

| STATUS | Meaning | Where to look |
|---|---|---|
| `Running` | Containers are up | Not necessarily healthy — read READY (`1/1` or `0/1`) |
| `CrashLoopBackOff` | Starts and keeps exiting | `logs --previous`, the container command |
| `ImagePullBackOff` / `ErrImagePull` | The image cannot be fetched | Image name, `imagePullPolicy`, whether the image exists offline |
| `Pending` | No node takes it | Scheduling events in `describe pod`, nodeSelector, resources |
| `Completed` | The main process exited cleanly | A one-shot command run as a service |
| `Terminating` | Shutting down | Finalizers, `terminationGracePeriodSeconds` |

A climbing `RESTARTS` means the container keeps exiting; kubelet retries after 10s, 20s, 40s and so on, up to five minutes.

## describe and events

```
kubectl -n <ns> describe pod <pod>
kubectl -n <ns> describe pod -l app=<app>
kubectl -n <ns> get events --sort-by=.lastTimestamp
```

The `Events` block at the bottom of `describe` is the most useful part: whether `Scheduled` appeared at all, `Pulled` or `Failed to pull` for images, and `Started container` followed by `Back-off restarting failed container` when the container dies on its own.

## Logs

```
kubectl -n <ns> logs <pod>
kubectl -n <ns> logs -l app=<app> --tail=50
kubectl -n <ns> logs <pod> -c <container>     # multi-container pods
kubectl -n <ns> logs <pod> --previous         # the container that just exited
kubectl -n <ns> logs -f <pod>
```

**`--previous` is the one that matters for CrashLoopBackOff**: the current container is already gone, so without it there is nothing to read.

## Changing things: patch and edit

```
kubectl -n <ns> edit deploy <app>

kubectl -n <ns> patch deploy <app> --type=merge \
  -p='{"spec":{"replicas":2}}'

kubectl -n <ns> patch deploy <app> --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]'
```

`--type=merge` suits whole fields, `--type=json` suits one element inside an array. **Edit the Deployment, not the pod**: pods are produced by a ReplicaSet from the template, so an edit on a pod is lost the moment it is recreated.

## Rollouts

```
kubectl -n <ns> rollout status deploy/<app>
kubectl -n <ns> rollout status deploy/<app> --timeout=10s
kubectl -n <ns> rollout history deploy/<app>
kubectl -n <ns> rollout undo deploy/<app>
kubectl -n <ns> get deploy <app> -o jsonpath='{.status.availableReplicas}'
```

`READY` is ready replicas over desired replicas; `availableReplicas` additionally requires each pod to stay ready for `minReadySeconds`. With `minReadySeconds` set, those seconds pass between a pod turning Running and the rollout completing — that is not a stall.

## Services and endpoints

A service builds its endpoints by **matching its selector against pod labels**; with no endpoint it has no backend at all:

```
kubectl -n <ns> get svc
kubectl -n <ns> describe svc <app>
kubectl -n <ns> get svc <app> -o jsonpath='{.spec.selector}'
kubectl -n <ns> get pods --show-labels
kubectl -n <ns> get endpointslices -l kubernetes.io/service-name=<app>
kubectl -n <ns> get endpointslices -l kubernetes.io/service-name=<app> -o yaml
```

`endpointslices` is the current endpoint table (`kubectl get endpoints` still works but is superseded). The decision is always the same:

1. **No endpoint** → the selector does not match the labels, or the pod is not Ready yet.
2. **An endpoint but no answer** → `targetPort` does not match the port the container listens on, or the application never listened.

`port` is what clients connect to and `targetPort` is the port the packet reaches inside the container. They may differ, but `targetPort` must match what the container is actually listening on.

## Testing connectivity from a node

```
kubectl -n <ns> get svc <app> -o jsonpath='{.spec.clusterIP}'
curl -s --max-time 3 http://<clusterIP>:<port>/
curl -s --max-time 3 http://<app>.<ns>.svc.cluster.local:<port>/
kubectl -n <ns> get pods -o wide                    # pod IP
curl -s --max-time 3 http://<podIP>:<port>/
```

Hitting the pod IP before the ClusterIP separates "the application is not up" from "the service is misconfigured": if the pod IP answers and the ClusterIP does not, the fault is in the service.

## Other commands worth knowing

```
kubectl -n <ns> get deploy <app> -o yaml
kubectl -n <ns> exec -it <pod> -- sh
kubectl -n <ns> delete pod <pod>                    # recreate one, same template
kubectl api-resources
kubectl -n kube-system get pods                     # CoreDNS and other system components
```
