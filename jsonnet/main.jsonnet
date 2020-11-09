local trc = (import 'lib/thanos-receive-controller.libsonnet')({
  local cfg = self,
  name: 'thanos-receive-controller',
  namespace: 'thanos',
  version: 'master-2019-10-18-d55fee2',
  image: 'quay.io/observatorium/thanos-receive-controller:' + cfg.version,
  replicas: 1,
  hashrings: (import 'hashrings.jsonnet'),
});

{ [name]: trc[name] for name in std.objectFields(trc) if trc[name] != null }
