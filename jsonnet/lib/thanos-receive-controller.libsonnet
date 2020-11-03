{
  local trc = self,

  config:: {
    name: error 'must provide name',
    namespace: error 'must provide namespace',
    version: error 'must provide version',
    image: error 'must provide image',
    replicas: error 'must provide replicas',
    hashrings: error 'must provide hashring configuration',
    resources: {},

    commonLabels:: {
      'app.kubernetes.io/name': 'thanos-receive-controller',
      'app.kubernetes.io/instance': trc.config.name,
      'app.kubernetes.io/version': trc.config.version,
      'app.kubernetes.io/component': 'kubernetes-controller',
    },

    podLabelSelector:: {
      [labelName]: trc.config.commonLabels[labelName]
      for labelName in std.objectFields(trc.config.commonLabels)
      if !std.setMember(labelName, ['app.kubernetes.io/version'])
    },
  },

  serviceAccount: {
    apiVersion: 'v1',
    kind: 'ServiceAccount',
    metadata: {
      name: trc.config.name,
      namespace: trc.config.namespace,
      labels: trc.config.commonLabels,
    },
  },

  role: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'Role',
    metadata: {
      name: trc.config.name,
      namespace: trc.config.namespace,
      labels: trc.config.commonLabels,
    },

    rules: [
      {
        apiGroups: [''],
        resources: ['configmaps'],
        verbs: ['list', 'watch', 'get', 'create', 'update'],
      },
      {
        apiGroups: ['apps'],
        resources: ['statefulsets'],
        verbs: ['list', 'watch', 'get'],
      },
    ],
  },

  roleBinding: {
    apiVersion: 'rbac.authorization.k8s.io/v1',
    kind: 'RoleBinding',
    metadata: {
      name: trc.config.name,
      namespace: trc.config.namespace,
      labels: trc.config.commonLabels,
    },

    roleRef: {
      apiGroup: 'rbac.authorization.k8s.io',
      kind: 'Role',
      name: trc.role.metadata.name,
    },
    subjects: [{
      kind: 'ServiceAccount',
      name: trc.serviceAccount.metadata.name,
      namespace: trc.serviceAccount.metadata.namespace,
    }],
  },

  configmap:
    {
      apiVersion: 'v1',
      kind: 'ConfigMap',
      metadata: {
        name: trc.config.name + '-tenants',
        namespace: trc.config.namespace,
        labels: trc.config.commonLabels,
      },
      data: { 'hashrings.json': std.manifestJsonEx(trc.config.hashrings, '  ') },
    },

  service: {
    apiVersion: 'v1',
    kind: 'Service',
    metadata: {
      name: trc.config.name,
      namespace: trc.config.namespace,
      labels: trc.config.commonLabels,
    },
    spec: {
      ports: [
        { name: 'http', targetPort: 8080, port: 8080 },
      ],
      selector: trc.config.podLabelSelector,
    },
  },

  deployment:
    local c = {
      name: 'thanos-receive-controller',
      image: trc.config.image,
      args: [
        '--configmap-name=%s' % trc.configmap.metadata.name,
        '--configmap-generated-name=%s-generated' % trc.configmap.metadata.name,
        '--file-name=hashrings.json',
        '--namespace=$(NAMESPACE)',
      ],
      env: [{
        name: 'NAMESPACE',
        valueFrom: { fieldRef: { fieldPath: 'metadata.namespace' } },
      }],
      ports: [
        { name: port.name, containerPort: port.port }
        for port in trc.service.spec.ports
      ],
      resources: trc.config.resources,
    };

    {
      apiVersion: 'apps/v1',
      kind: 'Deployment',
      metadata: {
        name: trc.config.name,
        namespace: trc.config.namespace,
        labels: trc.config.commonLabels,
      },
      spec: {
        replicas: trc.config.replicas,
        selector: { matchLabels: trc.config.podLabelSelector },
        template: {
          metadata: {
            labels: trc.config.commonLabels,
          },
          spec: {
            containers: [c],
            serviceAccount: trc.serviceAccount.metadata.name,
          },
        },
      },
    },

  withServiceMonitor:: {
    local trc = self,
    serviceMonitor: {
      apiVersion: 'monitoring.coreos.com/v1',
      kind: 'ServiceMonitor',
      metadata+: {
        name: trc.config.name,
        namespace: trc.config.namespace,
      },
      spec: {
        selector: {
          matchLabels: trc.config.commonLabels,
        },
        endpoints: [
          { port: 'http' },
        ],
      },
    },
  },

  withResources:: {
    local trc = self,
    config+:: {
      resources: error 'must provide resources',
    },

    deployment+: {
      spec+: {
        template+: {
          spec+: {
            containers: [
              if c.name == 'thanos-receive-controller' then c {
                resources: trc.config.resources,
              } else c
              for c in super.containers
            ],
          },
        },
      },
    },
  },
}
