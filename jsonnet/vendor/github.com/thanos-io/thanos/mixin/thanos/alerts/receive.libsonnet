{
  local thanos = self,
  receive+:: {
    selector: error 'must provide selector for Thanos Receive alerts',
    httpErrorThreshold: 5,
    forwardErrorThreshold: 5,
    refreshErrorThreshold: 0,
    p99LatencyThreshold: 10,
  },
  prometheusAlerts+:: {
    groups+: [
      {
        name: 'thanos-receive.rules',
        rules: [
          {
            alert: 'ThanosReceiveHttpRequestErrorRateHigh',
            annotations: {
              message: 'Thanos Receive {{$labels.job}} is failing to handle {{ $value | humanize }}% of requests.',
            },
            expr: |||
              (
                sum(rate(http_requests_total{code=~"5..", %(selector)s, handler="receive"}[5m]))
              /
                sum(rate(http_requests_total{%(selector)s, handler="receive"}[5m]))
              ) * 100 > %(httpErrorThreshold)s
            ||| % thanos.receive,
            'for': '5m',
            labels: {
              severity: 'critical',
            },
          },
          {
            alert: 'ThanosReceiveHttpRequestLatencyHigh',
            annotations: {
              message: 'Thanos Receive {{$labels.job}} has a 99th percentile latency of {{ $value }} seconds for requests.',
            },
            expr: |||
              (
                histogram_quantile(0.99, sum by (job, le) (rate(http_request_duration_seconds_bucket{%(selector)s, handler="receive"}[5m]))) > %(p99LatencyThreshold)s
              and
                sum by (job) (rate(http_request_duration_seconds_count{%(selector)s, handler="receive"}[5m])) > 0
              )
            ||| % thanos.receive,
            'for': '10m',
            labels: {
              severity: 'critical',
            },
          },
          {
            alert: 'ThanosReceiveHighForwardRequestFailures',
            annotations: {
              message: 'Thanos Receive {{$labels.job}} is failing to forward {{ $value | humanize }}% of requests.',
            },
            expr: |||
              (
                sum by (job) (rate(thanos_receive_forward_requests_total{result="error", %(selector)s}[5m]))
              /
                sum by (job) (rate(thanos_receive_forward_requests_total{%(selector)s}[5m]))
              * 100 > %(forwardErrorThreshold)s
              )
            ||| % thanos.receive,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveHighHashringFileRefreshFailures',
            annotations: {
              message: 'Thanos Receive {{$labels.job}} is failing to refresh hashring file, {{ $value | humanize }} of attempts failed.',
            },
            expr: |||
              (
                sum by (job) (rate(thanos_receive_hashrings_file_errors_total{%(selector)s}[5m]))
              /
                sum by (job) (rate(thanos_receive_hashrings_file_refreshes_total{%(selector)s}[5m]))
              > %(refreshErrorThreshold)s
              )
            ||| % thanos.receive,
            'for': '15m',
            labels: {
              severity: 'warning',
            },
          },
          {
            alert: 'ThanosReceiveConfigReloadFailure',
            annotations: {
              message: 'Thanos Receive {{$labels.job}} has not been able to reload hashring configurations.',
            },
            expr: 'avg(thanos_receive_config_last_reload_successful{%(selector)s}) by (job) != 1' % thanos.receive,
            'for': '5m',
            labels: {
              severity: 'warning',
            },
          },
        ],
      },
    ],
  },
}
