local g = (import 'grafana-builder/grafana.libsonnet');

{
  local thanos = self,
  grafanaDashboards+:: {
    local selector = std.join(', ', thanos.dashboard.commonSelector + ['job="$job"']),
    local aggregator = std.join(', ', thanos.dashboard.commonAggregator + ['job']),

    'receive-controller.json':
      g.dashboard(thanos.receiveController.title)
      .addRow(
        g.row('Reconcile Attempts')
        .addPanel(
          g.panel('Rate') +
          g.queryPanel(
            'thanos_receive_controller_reconcile_attempts_total{%(selector)s}' % thanos.receiveController,
            'rate'
          )
        )
        .addPanel(
          g.panel('Errors') +
          g.queryPanel(
            'sum by (%(aggregator)s, type) (rate(thanos_receive_controller_reconcile_errors_total{%(selector)s}[$interval]))' % thanos.receiveController,
            '{{type}}'
          ) +
          { yaxes: g.yaxes('percentunit') } +
          g.stack
        )
      )
      .addRow(
        g.row('Configmap Changes')
        .addPanel(
          g.panel('Rate') +
          g.queryPanel(
            'thanos_receive_controller_configmap_change_attempts_total{%(selector)s}' % thanos.receiveController,
            'rate',
          )
        )
        .addPanel(
          g.panel('Errors') +
          g.queryPanel(
            'sum sum by (%(aggregator)s, type) (rate(thanos_receive_controller_configmap_change_errors_total{%(selector)s}[$interval]))' % thanos.receiveController,
            '{{type}}'
          ) +
          { yaxes: g.yaxes('percentunit') } +
          g.stack
        )
      )
      .addRow(
        g.row('(Receive) Hashring Config Refresh')
        .addPanel(
          g.panel('Rate') +
          g.queryPanel(
            'sum by (%(aggregator)s, type) (rate(thanos_receive_hashrings_file_changes_total{%(receiveSelector)s}[$interval]))' % thanos.receiveController,
            'all'
          )
        )
        .addPanel(
          g.panel('Errors') +
          {
            local expr(selector) = 'sum by (%s) (rate(%s[$interval]))' % [aggregator, selector],

            aliasColors: {
              'error': '#E24D42',
            },
            targets: [
              {
                expr: '%s / %s' % [
                  expr('thanos_receive_hashrings_file_errors_total{%(receiveSelector)s}' % thanos.receiveController),
                  expr('thanos_receive_hashrings_file_changes_total{%(receiveSelector)s}' % thanos.receiveController),
                ],
                format: 'time_series',
                intervalFactor: 2,
                legendFormat: 'error',
                refId: 'A',
                step: 10,
              },
            ],
            yaxes: g.yaxes({ format: 'percentunit' }),
          } + g.stack,
        )
      )
      .addRow(
        g.row('Hashring Status')
        .addPanel(
          g.panel('Nodes per Hashring') +
          g.queryPanel(
            [
              'avg by (%(aggregator)s, name) (thanos_receive_controller_hashring_nodes{%(selector)s})' % thanos.receiveController,
              'avg by (%(aggregator)s, name) (thanos_receive_hashring_nodes{%(receiveSelector)s})' % thanos.receiveController,
            ],
            [
              'receive controller {{name}}',
              'receive {{name}}',
            ]
          )
        )
        .addPanel(
          g.panel('Tenants per Hashring') +
          g.queryPanel(
            [
              'avg by (%(aggregator)s, name) (thanos_receive_controller_hashring_tenants{%(selector)s})' % thanos.receiveController,
              'avg by (%(aggregator)s, name) (thanos_receive_hashring_tenants{%(receiveSelector)s})' % thanos.receiveController,
            ],
            [
              'receive controller {{name}}',
              'receive {{name}}',
            ],
          )
        )
      )
      .addRow(
        g.row('Hashring Config')
        .addPanel(
          g.panel('Last Updated') +
          g.statPanel(
            'time() - max by (%(aggregator)s, name) (thanos_receive_controller_configmap_last_reload_success_timestamp_seconds{%(selector)s})' % thanos.receiveController,
            's'
          ) +
          {
            postfix: 'ago',
            decimals: 0,
          }
        )
        .addPanel(
          g.panel('Last Updated') +
          g.statPanel(
            'time() - max by (%(aggregator)s, name) (thanos_receive_config_last_reload_success_timestamp_seconds{%(selector)s})' % thanos.receiveController,
            's'
          ) +
          {
            postfix: 'ago',
            decimals: 0,
          }
        )
      )
      {
        local template = (import 'grafonnet/grafana.libsonnet').template,
        templating+: {
          list+: [
            template.new(
              level,
              '$datasource',
              'label_values(%s, %s)' % [thanos.hierarcies[level], level],
              label=level,
              refresh=1,
              sort=2,
            )
            for level in std.objectFields(thanos.hierarcies)
          ] + [
            template.new(
              'job',
              '$datasource',
              'label_values(up{%s, %s}, job)' % [aggregator, selector],
              label='job',
              refresh=1,
              sort=2,
              current='all',
              allValues=null,
              includeAll=true
            ),
          ],
        },
      },
  },
} +
(import 'defaults.libsonnet')
