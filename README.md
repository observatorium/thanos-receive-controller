# Thanos Receive Controller

The Thanos Receive Controller configures multiple hashrings of Thanos receivers running as StatefulSets on Kubernetes.  
Based on an initial mapping of tenants to hashrings, the controller identifies the Pods in each hashring and generates a complete configuration file as a ConfigMap.

[![Build Status](https://cloud.drone.io/api/badges/observatorium/thanos-receive-controller/status.svg)](https://cloud.drone.io/observatorium/thanos-receive-controller)

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

Finally, deploy StatefulSets of Thanos receivers labeled with `controller.receive.thanos.io=thanos-receive-controller`, and with the hashring name in the `controller.receive.thanos.io/hashring` label, e.g.:

```shell
cat <<'EOF' | kubectl apply -f -
apiVersion: apps/v1
kind: StatefulSet
metadata:
  labels:
    app.kubernetes.io/instance: hashring0
    app.kubernetes.io/name: thanos-receive
    controller.receive.thanos.io: thanos-receive-controller
    controller.receive.thanos.io/hashring: hashring0
  name: thanos-receive-hashring0
spec:
  replicas: 3
  selector:
    matchLabels:
      app.kubernetes.io/instance: hashring0
      app.kubernetes.io/name: thanos-receive
  serviceName: thanos-receive-hashring0
  template:
    metadata:
      labels:
        app.kubernetes.io/instance: hashring0
        app.kubernetes.io/name: thanos-receive
    spec:
      containers:
      - args:
        - receive
        - --grpc-address=0.0.0.0:10901
        - --http-address=0.0.0.0:10902
        - --remote-write.address=0.0.0.0:19291
        - --tsdb.path=/var/thanos/receive
        - --label=replica="$(NAME)"
        - --label=receive="true"
        - --tsdb.retention=6h
        - --receive.hashrings-file=/var/lib/thanos-receive/hashrings.json
        - --receive.local-endpoint=$(NAME).thanos-receive-hashring0.$(NAMESPACE).svc.cluster.local:10901
        env:
        - name: NAME
          valueFrom:
            fieldRef:
              fieldPath: metadata.name
        - name: NAMESPACE
          valueFrom:
            fieldRef:
              fieldPath: metadata.namespace
        image: quay.io/thanos/thanos
        name: thanos-receive
        ports:
        - containerPort: 10901
          name: grpc
        - containerPort: 10902
          name: http
        - containerPort: 19291
          name: remote-write
      volumes:
      - configMap:
          name: observatorium-tenants-generated
        name: observatorium-tenants
```

The controller lists all of the StatefulSets with the `controller.receive.thanos.io=thanos-receive-controller` label and matches the value of their `controller.receive.thanos.io/hashring` labels to the hashring names in the configuration file.
The endpoints for each hashring will be populated automatically by the controller and the complete configuration file will be placed in a ConfigMap named `thanos-receive-generated`.
This configuration should be consumed as a ConfigMap volume by the Thanos receivers.

## Advanced

Thanos receivers can handle potentially private data for various tenants.
When a Thanos receiver Pod is deleted, or the StatefulSet is otherwise scaled down, PersistentVolumes holding this potentially sensitive data may be left in the cluster.
In order to ensure that the PersistentVolume used by a Thanos receiver can be safely reused, the Thanos Receive Controller will automatically launch a short-lived Job that mounts these PersistentVolumes and cleans them up.
The cleanup process consists of:
1. ensuring any leftover TSDB blocks are backed-up to object storage by running a [thanos-replicate](https://github.com/observatorium/thanos-replicate/) container for each PersistentVolume;
in order to run this container, the Thanos Receive Controller expects the Thanos receiver StatefulSet to be labeled with `controller.receive.thanos.io/objstore-secret`, pointing to a Secret containing the Thanos object storage configuration file, and `controller.receive.thanos.io/objstore-secret-key`, specifying which key in the Secret holds the file;
additional environment variables for the `thanos-replicate` container, e.g. `AWS_SECRET_ACCESS_KEY`, can be provided by adding those variables as keys in a Secret and specifying that Secret's name in the `controller.receive.thanos.io/env-var-secret` label;
if any of the replication processes fails to run, the cleanup process is aborted and is retried after some backoff
1. removing all data in the PersistentVolumes by running a container that mounts all of the PersistentVolumes and runs `rm rf` on each mount.
