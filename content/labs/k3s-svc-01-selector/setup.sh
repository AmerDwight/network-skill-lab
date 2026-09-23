#!/usr/bin/env bash
set -euo pipefail

if [ "$NSL_NODE" != k3s01 ]; then
	exit 0
fi

# internet: false leaves the node without a default route, so the cluster
# service CIDR has nowhere to go until it is routed at the lab link.
ip route replace "$NSL_SERVICE_CIDR" dev eth1

selector="$NSL_APP-web"
target_port="$NSL_PORT"
if [ "${NSL_SVC_TARGET_OFF:-}" = "1" ]; then
	selector="$NSL_APP"
	target_port=$((NSL_PORT + 1))
fi

kubectl create namespace "$NSL_NAMESPACE"

kubectl -n "$NSL_NAMESPACE" apply -f - <<EOF
apiVersion: apps/v1
kind: Deployment
metadata:
  name: $NSL_APP
  labels:
    app: $NSL_APP
spec:
  replicas: 1
  selector:
    matchLabels:
      app: $NSL_APP
  template:
    metadata:
      labels:
        app: $NSL_APP
    spec:
      nodeSelector:
        kubernetes.io/hostname: k3s02
      containers:
        - name: web
          image: $NSL_IMAGE
          imagePullPolicy: Never
          command: ["sh", "-c", "mkdir -p /www && echo ok > /www/index.html && busybox httpd -f -p $NSL_PORT -h /www"]
          ports:
            - containerPort: $NSL_PORT
---
apiVersion: v1
kind: Service
metadata:
  name: $NSL_APP
spec:
  type: ClusterIP
  selector:
    app: $selector
  ports:
    - port: $NSL_PORT
      targetPort: $target_port
EOF
