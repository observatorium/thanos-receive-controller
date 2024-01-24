# Thanos Receive Controller

The Thanos Receive Controller configures multiple hashrings of Thanos receivers running as StatefulSets on Kubernetes.  
Based on an initial mapping of tenants to hashrings, the controller identifies the Pods in each hashring and generates a complete configuration file as a ConfigMap.

[![Build Status](https://github.com/observatorium/thanos-receive-controller/actions/workflows/checks.yaml/badge.svg?branch=main)](https://github.com/observatorium/thanos-receive-controller/actions/workflows/checks.yaml)

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

## About the `--allow-only-ready-replicas` flag
By default, upon a scale up, the controller adds all new receiver replicas into the hashring as soon as they are in a _running_ state.
However, this means the new replicas will be receiving requests from other replicas in the hashring before they are ready to accept them.
Due to the nature of how receiver works, it can take some time until receiver's storage is ready.
Depending on your roll out strategy, you might see an increased failure rate in your hashring until enough replicas are in a ready state.

An alternative is to use the `--allow-only-ready-replicas`, which modifies this behavior.
Instead, upon a scale-up, new replicas are added only after it is confirmed they are ready.
This means:
- Old replicas keep operating with the old hashring, until all new replicas are ready. Once this is true, the hashring is updated to include all replicas in the stateful set
- New replicas will initially come up with the old hashring configuration. This means they will serve only as a "router" and any requests that they receive will be forwarded to replicas in the old hashring. Once _all_ new receiver replicas are ready, the hashring will be updated to include both old and new replicas.


## About the `--allow-dynamic-scaling` flag
By default, the controller does not react to voluntary/involuntary disruptions to receiver replicas in the StatefulSet.
This flag allows the user to enable this behavior.
When enabled, the controller will react to voluntary/involuntary disruptions to receiver replicas in the StatefulSet.
When a Pod is marked for termination, the controller will remove it from the hashring and the replica essentially becomes a "router" for the hashring.
When a Pod is deleted, the controller will remove it from the hashring.
When a Pod becomes unready, the controller will remove it from the hashring.
This behaviour can be considered for use alongside the [Ketama hashing algorithm](https://thanos.io/tip/components/receive.md/#ketama-recommended).

## About the `--use-az-aware-hashring` flag
By default, the controller does not support
