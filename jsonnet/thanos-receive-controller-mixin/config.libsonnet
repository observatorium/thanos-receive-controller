{
  local thanos = self,
  // Hierarcies is a way to help mixin users to add high level hierarcies to their alerts and dashboards.
  // The key in the key-value pair will be used as label name in the alerts and variable name in the dashboards.
  // The value in the key-value pair will be used as a query to fetch available values for the given label name.
  hierarcies+:: {
    // For example:
    // cluster: 'find_mi_cluster_bitte',
    namespace: 'kube_pod_info',
  },
  receiveController+:: {
    selector: 'job=~"thanos-receive-controller.*"',
    receiveSelector: 'job=~"thanos-receive-default.*"',
    title: '%(prefix)sReceive Controller' % $.dashboard.prefix,
  },
  dashboard+:: {
    prefix: 'Thanos / ',
    tags: ['thanos-receive-controller-mixin', 'observatorium'],
    commonSelector: ['%s="$%s"' % [level, level] for level in std.objectFields(thanos.hierarcies)],
  },
}
