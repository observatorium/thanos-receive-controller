EXAMPLES := examples
MANIFESTS := ${EXAMPLES}/manifests/
DASHBOARDS := ${EXAMPLES}/dashboards/
ALERTS := ${EXAMPLES}/alerts.yaml
RULES := ${EXAMPLES}/rules.yaml
SRC = $(shell find . -type f -name '*.go' -not -path './vendor/*')
JSONNET_SRC = $(shell find . -name 'vendor' -prune -o -name '*.libsonnet' -print -o -name '*.jsonnet' -print)

BIN_DIR ?= ./tmp/bin
GOJSONTOYAML ?= $(BIN_DIR)/gojsontoyaml
GOLANGCILINT ?= $(BIN_DIR)/golangci-lint
JSONNET ?= $(BIN_DIR)/jsonnet
JSONNET_FMT ?= $(BIN_DIR)/jsonnetfmt
JSONNET_BUNDLER ?= $(BIN_DIR)/jb
PROMTOOL ?= $(BIN_DIR)/promtool

all: generate fmt thanos-receive-controller

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
jsonnet-vendor: jsonnet/jsonnetfile.json $(JSONNET_BUNDLER)
	rm -rf jsonnet/vendor
	cd jsonnet && ../$(JSONNET_BUNDLER) install

.PHONY: vendor
vendor: go-vendor jsonnet-vendor

.PHONY: fmt
fmt: jsonnet-fmt
	@fmt_res=$$(gofmt -d -s $$(find . -type f -name '*.go' -not -path './vendor/*' -not -path './jsonnet/vendor/*')); if [ -n "$$fmt_res" ]; then printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$$fmt_res"; exit 1; fi


.PHONY: jsonnet-fmt
jsonnet-fmt: $(JSONNET_FMT)
	@echo ${JSONNET_SRC} | xargs -n 1 -- $(JSONNET_FMT) -n 2 --max-blank-lines 2 --string-style s --comment-style s -i

.PHONY: lint
lint: fmt prom-lint $(GOLANGCILINT)
	$(GOLANGCILINT) run -v --enable-all

.PHONY: prom-lint
prom-lint: ${ALERTS} ${RULES}  $(PROMTOOL)
	$(PROMTOOL) check rules ${ALERTS} ${RULES}

.PHONY: test
test: prom-test
	CGO_ENABLED=1 GO111MODULE=on GOPROXY=https://proxy.golang.org go test -v -race ./...

.PHONY: prom-test
prom-test: ${ALERTS} ${RULES}
	$(PROMTOOL) test rules tests.yaml

.PHONY: clean
clean:
	-rm thanos-receive-controller
	rm -rf ${DASHBOARDS}
	rm -rf ${ALERTS}
	rm -rf ${RULES}

$(BIN_DIR):
	mkdir -p $(BIN_DIR)

$(GOJSONTOYAML): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/brancz/gojsontoyaml

$(GOLANGCILINT): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/golangci/golangci-lint/cmd/golangci-lint

$(JSONNET_BUNDLER): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/jsonnet-bundler/jsonnet-bundler/cmd/jb

$(JSONNET): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/google/go-jsonnet/cmd/jsonnet

$(JSONNET_FMT): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/google/go-jsonnet/cmd/jsonnetfmt

$(PROMTOOL): vendor $(BIN_DIR)
	go build -mod=vendor -o $@ github.com/prometheus/prometheus/cmd/promtool
