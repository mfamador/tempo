(import 'ksonnet-util/kausal.libsonnet') +
(import 'configmap.libsonnet') +
(import 'config.libsonnet') + 
(import 'compactor.libsonnet') + 
(import 'distributor.libsonnet') + 
(import 'ingester.libsonnet') + 
(import 'querier.libsonnet') +
{
  namespace:
    $.core.v1.namespace.new($._config.namespace),
}