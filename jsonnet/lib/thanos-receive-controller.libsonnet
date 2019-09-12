local k = import 'ksonnet/ksonnet.beta.4/k.libsonnet';

{
  _config+:: {
    namespace:: 'observatorium',
    version:: 'latest',
    imageRepo:: 'quay.io/observatorium/thanos-receive-controller',
  },
  thanos+:: {
    receiveController: {
      serviceAccount:
        local sa = k.core.v1.serviceAccount;

        sa.new() +
        sa.mixin.metadata.withName('thanos-receive-controller') +
        sa.mixin.metadata.withNamespace($._config.namespace),

      role:
        local role = k.rbac.v1.role;
        local rules = role.rulesType;

        role.new() +
        role.mixin.metadata.withName('thanos-receive-controller') +
        role.mixin.metadata.withNamespace($._config.namespace) +
        role.withRules([
          rules.new() +
          rules.withApiGroups(['']) +
          rules.withResources([
            'configmaps',
          ]) +
          rules.withVerbs(['list', 'watch', 'get', 'create', 'update']),
          rules.new() +
          rules.withApiGroups(['apps']) +
          rules.withResources([
            'statefulsets',
          ]) +
          rules.withVerbs(['list', 'watch', 'get']),
        ]),

      roleBinding:
        local rb = k.rbac.v1.roleBinding;

        rb.new() +
        rb.mixin.metadata.withName('thanos-receive-controller') +
        rb.mixin.metadata.withNamespace($._config.namespace) +
        rb.mixin.roleRef.withApiGroup('rbac.authorization.k8s.io') +
        rb.mixin.roleRef.withName($.thanos.receiveController.role.metadata.name) +
        rb.mixin.roleRef.mixinInstance({ kind: 'Role' }) +
        rb.withSubjects([{
          kind: 'ServiceAccount',
          name: $.thanos.receiveController.serviceAccount.metadata.name,
          namespace: $.thanos.receiveController.serviceAccount.metadata.namespace,
        }]),

      configmap:
        local configmap = k.core.v1.configMap;

        configmap.new() +
        configmap.mixin.metadata.withName('observatorium-tenants') +
        configmap.mixin.metadata.withNamespace($._config.namespace) +
        configmap.mixin.metadata.withLabels({ 'app.kubernetes.io/name': $.thanos.receiveController.deployment.metadata.name }) +
        configmap.withData({
          'hashrings.json': std.manifestJsonEx((import '../tenants.libsonnet'), '  '),
        }),

      service:
        local service = k.core.v1.service;
        local ports = service.mixin.spec.portsType;

        service.new(
          'thanos-receive-controller',
          $.thanos.receiveController.deployment.metadata.labels,
          [
            ports.newNamed('http', 8080, 8080),
          ],
        ) +
        service.mixin.metadata.withNamespace($._config.namespace) +
        service.mixin.metadata.withLabels({ 'app.kubernetes.io/name': $.thanos.receiveController.deployment.metadata.name }),

      deployment:
        local deployment = k.apps.v1.deployment;
        local container = deployment.mixin.spec.template.spec.containersType;
        local containerPort = container.portsType;
        local env = container.envType;

        local c =
          container.new($.thanos.receiveController.deployment.metadata.name, '%s:%s' % [$._config.imageRepo, $._config.version]) +
          container.withArgs([
            '--configmap-name=%s' % $.thanos.receiveController.configmap.metadata.name,
            '--configmap-generated-name=%s-generated' % $.thanos.receiveController.configmap.metadata.name,
            '--file-name=hashrings.json',
            '--namespace=$(NAMESPACE)',
          ]) + container.withEnv([
            env.fromFieldPath('NAMESPACE', 'metadata.namespace'),
          ]) + container.withPorts(
            containerPort.newNamed(8080, 'http')
          );

        deployment.new('thanos-receive-controller', 1, c, $.thanos.receiveController.deployment.metadata.labels) +
        deployment.mixin.metadata.withNamespace($._config.namespace) +
        deployment.mixin.metadata.withLabels({ 'app.kubernetes.io/name': $.thanos.receiveController.deployment.metadata.name }) +
        deployment.mixin.spec.template.spec.withServiceAccount($.thanos.receiveController.serviceAccount.metadata.name) +
        deployment.mixin.spec.selector.withMatchLabels($.thanos.receiveController.deployment.metadata.labels),
    },
  },
}
