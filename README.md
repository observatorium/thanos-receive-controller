# Thanos Receive Controller

The Thanos Receive Controller configures multiple hashrings of Thanos receivers running as StatefulSets on Kubernetes.  
Based on an initial mapping of tenants to hashrings, the controller identifies the Pods in each hashring and generates a complete configuration file as a ConfigMap.

[![Build Status](https://github.com/observatorium/thanos-receive-controller/actions/workflows/checks.yaml/badge.svg?branch=master)](https://github.com/observatorium/thanos-receive-controller/actions/workflows/checks.yaml)

## Getting Started

First, provide an initial mapping of tenants to hashrings in a ConfigMap, e.g.:

```shell
cat <<'EOF' | kubectl apply -f -
apiVersion: v1
kind: ConfigMap
metadata:
  name: thanos-receive
  labels:
    app.kubernetes.io/name: thanos-receive
data:
  hashrings.json: |
    [
        {
            "hashring": "hashring0",
            "tenants": ["foo", "bar"]
        },
        {
            "hashring": "hashring1",
            "tenants": ["baz"]
        }
    ]
EOF
```

Next, deploy the controller, pointing it at the configuration file in the ConfigMap:

```shell
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: Deployment
metadata:
  name: thanos-receive-controller
  labels:
    app.kubernetes.io/name: thanos-receive-controller
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: thanos-receive-controller
  template:
    metadata:
      labels:
        app.kubernetes.io/name: thanos-receive-controller
    spec:
      containers:
      - args:
        - --configmap-name=thanos-receive
        - --configmap-generated-name=thanos-receive-generated
        - --file-name=hashrings.json
        image: quay.io/observatorium/thanos-receive-controller
        name: thanos-receive-controller
EOF
```

Finally, deploy StatefulSets of Thanos receivers labeled with `controller.receive.thanos.io=thanos-receive-controller`.
The controller lists all of the StatefulSets with that label and matches the value of their `controller.receive.thanos.io/hashring` labels to the hashring names in the configuration file.
The endpoints for each hashring will be populated automatically by the controller and the complete configuration file will be placed in a ConfigMap named `thanos-receive-generated`.
This configuration should be consumed as a ConfigMap volume by the Thanos receivers.
