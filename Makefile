EXAMPLES := examples
MANIFESTS := ${EXAMPLES}/manifests/
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

thanos-receive-controller: go-vendor main.go
	CGO_ENABLED=0 GO111MODULE=on GOPROXY=https://proxy.golang.org go build -mod vendor -v

.PHONY: generate
generate: jsonnet-vendor ${ALERTS} ${RULES} ${DASHBOARDS} ${MANIFESTS}

.PHONY: generate-in-docker
generate-in-docker:
	@echo ">> Compiling assets and generating Kubernetes manifests"
	$(CONTAINER_CMD) make $(MFLAGS) generate

.PHONY: ${MANIFESTS}
${MANIFESTS}: jsonnet/main.jsonnet jsonnet/tenants.libsonnet jsonnet/lib/*
	@rm -rf ${MANIFESTS}
	@mkdir -p ${MANIFESTS}
	jsonnet -J jsonnet/vendor -m ${MANIFESTS} jsonnet/main.jsonnet | xargs -I{} sh -c 'cat {} | gojsontoyaml > {}.yaml && rm -f {}' -- {}

.PHONY: ${DASHBOARDS}
${DASHBOARDS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/dashboards/*
	@rm -rf ${DASHBOARDS}
	@mkdir -p ${DASHBOARDS}
	jsonnet -J jsonnet/vendor -m ${DASHBOARDS} jsonnet/thanos-receive-controller-mixin/dashboards.jsonnet

${ALERTS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/alerts/*
	jsonnet jsonnet/thanos-receive-controller-mixin/alerts.jsonnet | gojsontoyaml > $@

${RULES}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/rules/*
	jsonnet jsonnet/thanos-receive-controller-mixin/rules.jsonnet | gojsontoyaml > $@

.PHONY: go-vendor
go-vendor: go.mod go.sum
	go mod vendor

.PHONY: jsonnet-vendor
jsonnet-vendor: jsonnet/jsonnetfile.json
	rm -rf jsonnet/vendor
	cd jsonnet && jb install

.PHONY: fmt
fmt:
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi
	@echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNET_FMT) -i

.PHONY: lint
lint: fmt ${ALERTS} ${RULES}
	golangci-lint run -v --enable-all
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
