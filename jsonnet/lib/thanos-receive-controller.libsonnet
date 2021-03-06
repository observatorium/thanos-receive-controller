// These are the defaults for this components configuration.
// When calling the function to generate the component's manifest,
// you can pass an object structured like the default to overwrite default values.
local defaults = {
  local defaults = self,
  name: error 'must provide name',
  namespace: error 'must provide namespace',
  version: error 'must provide version',
  image: error 'must provide image',
  replicas: error 'must provide replicas',
  hashrings: error 'must provide hashring configuration',
  resources: {},
  serviceMonitor: false,
  ports: { http: 8080 },

  commonLabels:: {
    'app.kubernetes.io/name': 'thanos-receive-controller',
    'app.kubernetes.io/instance': defaults.name,
    'app.kubernetes.io/version': defaults.version,
    'app.kubernetes.io/component': 'kubernetes-controller',
  },

  podLabelSelector:: {
    [labelName]: defaults.commonLabels[labelName]
    for labelName in std.objectFields(defaults.commonLabels)
    if !std.setMember(labelName, ['app.kubernetes.io/version'])
  },
};

function(params) {
  local trc = self,

  // Combine the defaults and the passed params to make the component's config.
  config:: defaults + params,
  // Safety checks for combined config of defaults and params
  assert std.isNumber(trc.config.replicas) && trc.config.replicas >= 0 : 'thanos receive controller replicas has to be number >= 0',
  assert std.isObject(trc.config.resources),
  assert std.isBoolean(trc.config.serviceMonitor),
  assert std.isArray(trc.config.hashrings),

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

  configmap: {
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
        {
          assert std.isString(name),
          assert std.isNumber(trc.config.ports[name]),

          name: name,
          port: trc.config.ports[name],
          targetPort: trc.config.ports[name],
        }
        for name in std.objectFields(trc.config.ports)
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
      env: [
        { name: 'NAMESPACE', valueFrom: { fieldRef: { fieldPath: 'metadata.namespace' } } },
      ],
      ports: [
        { name: port.name, containerPort: port.port }
        for port in trc.service.spec.ports
      ],
      securityContext: {
        runAsUser: 65534,
      },
      resources: if trc.config.resources != {} then trc.config.resources else {},
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
            securityContext: {
              fsGroup: 65534,
            },
            serviceAccount: trc.serviceAccount.metadata.name,
          },
        },
      },
    },


  serviceMonitor: if trc.config.serviceMonitor == true then {
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
}
