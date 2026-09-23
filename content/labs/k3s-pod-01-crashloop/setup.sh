#!/usr/bin/env bash
set -euo pipefail

if [ "$NSL_NODE" != k3s01 ]; then
	exit 0
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
  replicas: $NSL_REPLICAS
  minReadySeconds: 10
  selector:
    matchLabels:
      app: $NSL_APP
  template:
    metadata:
      labels:
        app: $NSL_APP
    spec:
      containers:
        - name: app
          image: $NSL_IMAGE
          imagePullPolicy: Never
          command: ["sh", "-c", "echo booting; exit 1"]
EOF
