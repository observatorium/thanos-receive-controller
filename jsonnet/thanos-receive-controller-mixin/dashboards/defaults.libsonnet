{
  local grafanaDashboards = super.grafanaDashboards,
  local grafana = import 'grafonnet/grafana.libsonnet',
  local template = grafana.template,
  local thanos = self,

  // Automatically add a uid to each dashboard based on the base64 encoding
  // of the file name and set the timezone to be 'default'.
  grafanaDashboards:: {
    [filename]: grafanaDashboards[filename] {
      uid: std.md5(filename),
      timezone: 'UTC',
      tags: thanos.dashboard.tags,

      // Modify tooltip to only show a single value
      rows: [
        row {
          panels: [
            panel {
              tooltip+: {
                shared: false,
              },
            }
            for panel in super.panels
          ],
        }
        for row in super.rows
      ],

      templating+: {
        list: [
          if variable.name == 'datasource'
          then variable { regex: thanos.dashboard.instance_name_filter }
          else variable
          for variable in super.list
        ] + [
          template.interval(
            'interval',
            '5m,10m,30m,1h,6h,12h,auto',
            label='interval',
            current='5m',
          ),
        ],
      },
    }
    for filename in std.objectFields(grafanaDashboards)
  },
}
