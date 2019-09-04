local app = import 'lib/thanos-receive-controller.libsonnet';

{ [name]: app.thanos.receiveController[name] for name in std.objectFields(app.thanos.receiveController) }
