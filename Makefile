EXAMPLES := examples
DASHBOARDS := ${EXAMPLES}/dashboards/
ALERTS := ${EXAMPLES}/alerts.yaml
RULES := ${EXAMPLES}/rules.yaml
SRC = $(shell find . -type f -name '*.go' -not -path './vendor/*')
JSONNET_SRC = $(shell find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print)

JSONNET_FMT := jsonnetfmt -n 2 --max-blank-lines 2 --string-style s --comment-style s

CONTAINER_CMD:=docker run --rm \
		-u="$(shell id -u):$(shell id -g)" \
		-v "$(shell go env GOCACHE):/.cache/go-build" \
		-v "$(PWD):/go/src/github.com/observatorium/thanos-receive-controller:Z" \
		-w "/go/src/github.com/observatorium/thanos-receive-controller" \
		-e USER=deadbeef \
		-e GO111MODULE=on \
		quay.io/coreos/jsonnet-ci

all: generate fmt thanos-receive-controller

thanos-receive-controller: main.go
	CGO_ENABLED=0 GO111MODULE=on GOPROXY=https://proxy.golang.org go build -v

.PHONY: generate
generate: ${ALERTS} ${RULES} ${DASHBOARDS}

.PHONY: generate-in-docker
generate-in-docker:
	@echo ">> Compiling assets and generating Kubernetes manifests"
	$(CONTAINER_CMD) make $(MFLAGS) generate

.PHONY: ${DASHBOARDS}
${DASHBOARDS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/dashboards/*
	@rm -rf ${DASHBOARDS}
	@mkdir -p ${DASHBOARDS}
	jsonnet -J jsonnet/vendor -m ${DASHBOARDS} jsonnet/thanos-receive-controller-mixin/dashboards.jsonnet

${ALERTS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/alerts/*
	jsonnet jsonnet/thanos-receive-controller-mixin/alerts.jsonnet | gojsontoyaml > $@

${RULES}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/rules/*
	jsonnet jsonnet/thanos-receive-controller-mixin/rules.jsonnet | gojsontoyaml > $@

.PHONY: vendor
vendor: go.mod go.sum jsonnet/jsonnetfile.json jsonnet/jsonnetfile.lock.json
	rm -rf jsonnet/vendor
	cd jsonnet && jb install
	go mod vendor

.PHONY: fmt
fmt:
	@test -z $(shell fmt_res=$(gofmt -d -s ${SRC})) || printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$(fmt_res)"
	echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNET_FMT) -i

.PHONY: lint
lint: fmt ${ALERTS} ${RULES}
	golangci-lint run -v --enable-all -D lll -D errcheck
	promtool check rules ${ALERTS} ${RULES}

.PHONY: test ${ALERTS} ${RULES}
test:
	CGO_ENABLED=1 GO111MODULE=on GOPROXY=https://proxy.golang.org go test -v -race ./...
	promtool test rules tests.yaml

.PHONY: clean
clean:
	-rm thanos-receive-controller
	rm -rf ${DASHBOARDS}
	rm -rf ${ALERTS}
	rm -rf ${RULES}
