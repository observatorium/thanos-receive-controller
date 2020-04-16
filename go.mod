module github.com/observatorium/thanos-receive-controller

go 1.14

require (
	github.com/Azure/go-autorest/autorest v0.10.0 // indirect
	github.com/Azure/go-autorest/autorest/adal v0.8.3 // indirect
	github.com/brancz/gojsontoyaml v0.0.0-20191212081931-bf2969bbd742
	github.com/go-kit/kit v0.9.0
	github.com/golangci/golangci-lint v1.24.0
	github.com/google/go-cmp v0.4.0
	github.com/google/go-jsonnet v0.15.1-0.20200415122941-8a0084e64395
	github.com/jsonnet-bundler/jsonnet-bundler v0.3.1
	github.com/oklog/run v1.0.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.5.0
	github.com/prometheus/prometheus v1.8.2-0.20200213233353-b90be6f32a33 // master ~ v2.15.2
	github.com/thanos-io/thanos v0.12.0
	k8s.io/api v0.0.0-20191115095533-47f6de673b26
	k8s.io/apimachinery v0.0.0-20191115015347-3c7067801da2
	k8s.io/client-go v12.0.0+incompatible
)

replace (
	// Mitigation for: https://github.com/Azure/go-autorest/issues/414
	github.com/Azure/go-autorest => github.com/Azure/go-autorest v12.3.0+incompatible
	k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
)
