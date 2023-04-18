{
  local thanos = self,
  receiveController+:: {
    selector: error 'must provide selector for Thanos Receive Controller alerting rules',
    aggregator: std.join(', ', std.objectFields(thanos.hierarchies) + ['job']),
  },

  prometheusAlerts+:: {
    local location = if std.length(std.objectFields(thanos.hierarchies)) > 0 then ' from ' + std.join('/', ['{{$labels.%s}}' % level for level in std.objectFields(thanos.hierarchies)]) else '',
    groups+: [
      {
        name: 'thanos-receive-controller',
        rules: [
          {
            alert: 'ThanosReceiveControllerIsDown',
            expr: |||
              absent(up{%(selector)s} == 1)
            ||| % thanos.receiveController,
            'for': '5m',
            labels: {
              severity: 'critical',
            },
            annotations: {
              message: 'Thanos Receive Controller has disappeared%s. Prometheus target for the component cannot be discovered.' % location,
            },
          },
          {
            alert: 'ThanosReceiveControllerReconcileErrorRate',
            annotations: {
              message: 'Thanos Receive Controller%s is failing to reconcile changes, {{ $value | humanize }}%% of attempts failed.' % location,
            },
            expr: |||
              (
                sum by (%(aggregator)s) (rate(thanos_receive_controller_reconcile_errors_total{%(selector)s}[5m]))
              /
                sum by (%(aggregator)s) (rate(thanos_receive_controller_reconcile_attempts_total{%(selector)s}[5m]))
              ) * 100 >= 10
            ||| % thanos.receiveController,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveControllerConfigmapChangeErrorRate',
            annotations: {
              message: 'Thanos Receive Controller%s is failing to refresh configmap, {{ $value | humanize }}%% of attempts failed.' % location,
            },
            expr: |||
              (
                sum by (%(aggregator)s) (rate(thanos_receive_controller_configmap_change_errors_total{%(selector)s}[5m]))
              /
                sum by (%(aggregator)s) (rate(thanos_receive_controller_configmap_change_attempts_total{%(selector)s}[5m]))
              ) * 100 >= 10
            ||| % thanos.receiveController,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveConfigInconsistent',
            annotations: {
              message: 'The configuration of the instances of Thanos Receive `{{$labels.job}}`%s are out of sync.' % location,
            },
            expr: |||
              (
                avg by (%(aggregator)s) (thanos_receive_config_hash{%(receiveSelector)s})
              / on (%(on)s) group_left
                thanos_receive_controller_configmap_hash{%(selector)s} != 1
              )
            ||| % thanos.receiveController { on: std.join(', ', std.objectFields(thanos.hierarchies)) },
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
