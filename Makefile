SRC = $(shell find . -type f -name '*.go' -not -path './vendor/*')

all: fmt thanos-receive-controller

thanos-receive-controller: main.go
	go build -v

.PHONY: generate
generate: alerts.yaml rules.yaml dashboards

vendor: go.mod go.sum
	go mod vendor

.PHONY: fmt
fmt:
	@test -z $(shell gofmt -d -s ${SRC}) || printf '\nGofmt found style issues. Please check the reported issues\nand fix them if necessary before submitting the code for review:\n\n%s' "$(fmt_res)"

.PHONY: lint
lint: fmt
	golangci-lint run -v --enable-all

.PHONY: test
test:
	go test -v -race ./...

.PHONY: clean
clean:
	rm thanos-receive-controller
