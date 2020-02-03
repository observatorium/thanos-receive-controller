local trc = (import 'lib/thanos-receive-controller.libsonnet') {
  config+:: {
    name: 'thanos-receive-controller',
    namespace: 'thanos',
    image: 'quay.io/observatorium/thanos-receive-controller:master-2019-10-18-d55fee2',
    version: 'master-2019-10-18-d55fee2',
    replicas: 1,
    hashrings: (import 'hashrings.jsonnet'),
  },
};

{ [name]: trc[name] for name in std.objectFields(trc) }
