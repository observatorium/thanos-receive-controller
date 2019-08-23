EXAMPLES := ../examples
DASHBOARDS := ${EXAMPLES}/dashboards/
ALERTS := ${EXAMPLES}/alerts.yaml
RULES := ${EXAMPLES}/rules.yaml

JSONNET_FMT := jsonnetfmt -n 2 --max-blank-lines 2 --string-style s --comment-style s

CONTAINER_CMD:=docker run --rm \
		-u="$(shell id -u):$(shell id -g)" \
		-v "$(shell go env GOCACHE):/.cache/go-build" \
		-v "$(PWD):/go/src/github.com/observatorium/thanos-receive-controller:Z" \
		-w "/go/src/github.com/observatorium/thanos-receive-controller" \
		-e USER=deadbeef \
		-e GO111MODULE=on \
		quay.io/coreos/jsonnet-ci

all: generate fmt

.PHONY: generate-in-docker
generate-in-docker:
	@echo ">> Compiling assets and generating Kubernetes manifests"
	$(CONTAINER_CMD) make $(MFLAGS) generate

.PHONY: generate
generate: ${ALERTS} ${RULES} ${DASHBOARDS}

${DASHBOARDS}: thanos-receive-controller-mixin/mixin.libsonnet thanos-receive-controller-mixin/config.libsonnet thanos-receive-controller-mixin/dashboards/*
	@mkdir -p ${DASHBOARDS}
	jsonnet -J vendor -m ${DASHBOARDS} thanos-receive-controller-mixin/dashboards.jsonnet

${ALERTS}: thanos-receive-controller-mixin/mixin.libsonnet thanos-receive-controller-mixin/config.libsonnet thanos-receive-controller-mixin/alerts/*
	jsonnet thanos-receive-controller-mixin/alerts.jsonnet | gojsontoyaml > $@

${RULES}: thanos-receive-controller-mixin/mixin.libsonnet thanos-receive-controller-mixin/config.libsonnet thanos-receive-controller-mixin/rules/*
	jsonnet thanos-receive-controller-mixin/rules.jsonnet | gojsontoyaml > $@

vendor: jsonnetfile.json jsonnetfile.lock.json
	-rm -rf vendor
	jb install

.PHONY: fmt
fmt:
	find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print | \
		xargs -n 1 -- $(JSONNET_FMT) -i

.PHONY: lint
lint: fmt alerts.yaml rules.yaml
	promtool check rules alerts.yaml rules.yaml

.PHONY: test
test: alerts.yaml rules.yaml
	promtool test rules tests.yaml

.PHONY: clean
clean:
	rm -rf ${DASHBOARDS}
	rm -rf ${ALERTS}
	rm -rf ${RULES}
