{
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'thanos-receive-controller.rules',
        rules: [
          {
            alert: 'ThanosReceiveControllerReconcileErrorRate',
            annotations: {
              message: 'Thanos Receive Controller failing to reconcile changes, {{ $value | humanize }}% of attempts failed.',
            },
            expr: |||
              sum(
                rate(thanos_receive_controller_reconcile_errors_total{%(thanosReceiveControllerSelector)s}[5m])
                /
                on (namespace)
                rate(thanos_receive_controller_reconcile_attempts_total{%(thanosReceiveControllerSelector)s}[5m])
              ) * 100 >= 10
            ||| % $._config,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveControllerConfigmapChangeErrorRate',
            annotations: {
              message: 'Thanos Receive Controller failing to refresh configmap, {{ $value | humanize }}% of attempts failed.',
            },
            expr: |||
              sum(
                rate(thanos_receive_controller_configmap_change_errors_total{%(thanosReceiveControllerSelector)s}[5m])
                /
                on (namespace)
                rate(thanos_receive_controller_configmap_change_attempts_total{%(thanosReceiveControllerSelector)s}[5m])
              ) * 100 >= 10
            ||| % $._config,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveConfigStale',
            annotations: {
              message: 'The configuration of the instances of Thanos Receive are stale compare to controller.',
            },
            expr: |||
              avg(thanos_receive_config_last_reload_success_timestamp_seconds{%(thanosReceiveSelector)s}) by (namespace, job)
                <
              on(namespace)
              thanos_receive_controller_configmap_last_reload_success_timestamp_seconds{%(thanosReceiveControllerSelector)s}
            ||| % $._config,
            'for': '5m',
            labels: {
              severity: 'critical',
            },
          },
          {
            alert: 'ThanosReceiveConfigInconsistent',
            annotations: {
              message: 'The configuration of the instances of Thanos Receive `{{$labels.job}}` are out of sync.',
            },
            expr: |||
              avg(thanos_receive_config_hash{%(thanosReceiveSelector)s}) BY (namespace, job)
                /
              on (namespace)
              group_left
              thanos_receive_controller_configmap_hash{%(thanosReceiveControllerSelector)s}
              != 1
            ||| % $._config,
            'for': '5m',
            labels: {
              severity: 'critical',
            },
          },
        ],
      },
    ],
  },
}
