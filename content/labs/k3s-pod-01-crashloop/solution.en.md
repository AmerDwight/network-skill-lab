# Solution: the container command exits as soon as it runs

The Deployment runs `sh -c "echo booting; exit 1"`. The main process of the container prints one line and exits, the pod ends with it, and kubelet keeps recreating it with a growing back-off, so `RESTARTS` climbs and `STATUS` sits at `CrashLoopBackOff`.

## 1. Tell the failure modes apart

Work on `k3s01`; the kubeconfig is already in place.

```
kubectl -n <namespace> get pods
```

- `CrashLoopBackOff`: the container starts and keeps exiting — look at the logs and the command.
- `ImagePullBackOff` / `ErrImagePull`: the image cannot be fetched — look at the image name and `imagePullPolicy`.
- `Pending`: nothing schedules it — look at the scheduling events in `describe pod`, at resources and at nodeSelector.

## 2. Confirm from the events that the container really ran

```
kubectl -n <namespace> describe pod -l app=<app>
```

`Events` shows `Started container` followed by `Back-off restarting failed container`, so the problem is inside the container, not in scheduling or in the image.

## 3. Read the previous logs

The container is already dead, so plain `logs` has nothing to show; ask for the previous run:

```
kubectl -n <namespace> logs -l app=<app> --previous
```

One `booting` line and no error — the program exited by itself, it was not killed.

## 4. Read the command

```
kubectl -n <namespace> get deploy <app> -o jsonpath='{.spec.template.spec.containers[0].command}'
```

`["sh","-c","echo booting; exit 1"]`. Running a one-shot command as a long-lived service gives exactly this.

## 5. Fix it

```
kubectl -n <namespace> patch deploy <app> --type=json \
  -p='[{"op":"replace","path":"/spec/template/spec/containers/0/command","value":["sleep","3600"]}]'
```

`kubectl -n <namespace> edit deploy <app>` on the same field works too. Change the Deployment, not the pod: pods belong to a ReplicaSet, so an edit on a pod is lost the next time it is recreated.

## 6. Verify

```
kubectl -n <namespace> rollout status deploy/<app>
kubectl -n <namespace> get deploy <app>
```

The Deployment sets `minReadySeconds: 10`, so a pod counts as available only after ten uninterrupted seconds of Running; `READY` therefore fills in a dozen seconds after the pods turn Running. `successfully rolled out` and `READY` equal to the desired replica count means you are done.

## Dead ends

- `kubectl delete pod`: the new pod comes from the same template and crashes the same way.
- Changing `restartPolicy`: a Deployment pod template only accepts `Always`.
- Raising `replicas`: more broken pods, `availableReplicas` is still 0.
