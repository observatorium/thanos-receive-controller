include .bingo/Variables.mk

EXAMPLES := examples
MANIFESTS := ${EXAMPLES}/manifests/
DASHBOARDS := ${EXAMPLES}/dashboards/
ALERTS := ${EXAMPLES}/alerts.yaml
RULES := ${EXAMPLES}/rules.yaml
SRC = $(shell find . -type f -name '*.go' -not -path './vendor/*')
JSONNET_SRC = $(shell find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print)

all: generate validate fmt thanos-receive-controller

thanos-receive-controller: go-vendor main.go
	CGO_ENABLED=0 GO111MODULE=on GOPROXY=https://proxy.golang.org go build -mod vendor -v

.PHONY: generate
generate: jsonnet-vendor ${ALERTS} ${RULES} ${DASHBOARDS} ${MANIFESTS}

.PHONY: ${MANIFESTS}
${MANIFESTS}: jsonnet/main.jsonnet jsonnet/hashrings.jsonnet jsonnet/lib/* $(JSONNET) $(GOJSONTOYAML)
	@rm -rf ${MANIFESTS}
	@mkdir -p ${MANIFESTS}
	$(JSONNET) -J jsonnet/vendor -m ${MANIFESTS} jsonnet/main.jsonnet | xargs -I{} sh -c 'cat {} | $(GOJSONTOYAML) > {}.yaml && rm -f {}' -- {}

.PHONY: ${DASHBOARDS}
${DASHBOARDS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/dashboards/* $(JSONNET) $(GOJSONTOYAML)
	@rm -rf ${DASHBOARDS}
	@mkdir -p ${DASHBOARDS}
	$(JSONNET) -J jsonnet/vendor -m ${DASHBOARDS} jsonnet/thanos-receive-controller-mixin/dashboards.jsonnet

${ALERTS}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/alerts/* $(JSONNET) $(GOJSONTOYAML)
	$(JSONNET) jsonnet/thanos-receive-controller-mixin/alerts.jsonnet | $(GOJSONTOYAML) > $@

${RULES}: jsonnet/thanos-receive-controller-mixin/mixin.libsonnet jsonnet/thanos-receive-controller-mixin/config.libsonnet jsonnet/thanos-receive-controller-mixin/rules/* $(JSONNET) $(GOJSONTOYAML)
	$(JSONNET) jsonnet/thanos-receive-controller-mixin/rules.jsonnet | $(GOJSONTOYAML) > $@

.PHONY: go-vendor
go-vendor: go.mod go.sum
	go mod tidy
	go mod vendor

.PHONY: jsonnet-vendor
jsonnet-vendor: jsonnet/jsonnetfile.json $(JB)
	rm -rf jsonnet/vendor
	cd jsonnet && $(JB) install

.PHONY: vendor
vendor: go-vendor jsonnet-vendor

.PHONY: fmt
fmt: jsonnet-fmt
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './jsonnet/vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi

.PHONY: jsonnet-fmt
jsonnet-fmt: $(JSONNETFMT)
	@echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNETFMT) -n 2 --max-blank-lines 2 --string-style s --comment-style s -i

.PHONY: lint
lint: fmt prom-lint $(GOLANGCI_LINT)
	$(GOLANGCI_LINT) run -v --enable-all

.PHONY: prom-lint
prom-lint: ${ALERTS} ${RULES} $(PROMTOOL)
	$(PROMTOOL) check rules ${ALERTS} ${RULES}

.PHONY: test
test: prom-test
	CGO_ENABLED=1 go test -v -race ./...

.PHONY: prom-test
prom-test: ${ALERTS} ${RULES} $(PROMTOOL)
	$(PROMTOOL) test rules tests.yaml

.PHONY: validate
validate: $(KUBEVAL) $(MANIFESTS)
	$(KUBEVAL) --ignore-missing-schemas $(MANIFESTS)/*.yaml

.PHONY: clean
clean:
	-rm thanos-receive-controller
	rm -rf ${DASHBOARDS}
	rm -rf ${ALERTS}
	rm -rf ${RULES}
