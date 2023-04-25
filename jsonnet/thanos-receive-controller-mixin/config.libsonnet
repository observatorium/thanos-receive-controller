{
  local thanos = self,
  // Hierarchies is a way to help mixin users to add high level hierarchies to their alerts and dashboards.
  // The key in the key-value pair will be used as label name in the alerts and variable name in the dashboards.
  // The value in the key-value pair will be used as a query to fetch available values for the given label name.
  hierarchies+:: {
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
    selector: ['%s="$%s"' % [level, level] for level in std.objectFields(thanos.hierarchies)],
    aggregator: ['%s' % level for level in std.objectFields(thanos.hierarchies)],
    instance_name_filter: '',
  },
}
