module github.com/observatorium/thanos-receive-controller

go 1.14

require (
	github.com/go-kit/kit v0.9.0
	github.com/google/go-cmp v0.4.0
	github.com/oklog/run v1.0.0
	github.com/onsi/ginkgo v1.11.0 // indirect
	github.com/onsi/gomega v1.8.1 // indirect
	github.com/pkg/errors v0.9.1
	github.com/prometheus/client_golang v1.5.0
	github.com/thanos-io/thanos v0.12.0
	golang.org/x/crypto v0.0.0-20191206172530-e9b2fee46413 // indirect
	k8s.io/api v0.0.0-20191115095533-47f6de673b26
	k8s.io/apimachinery v0.0.0-20191115015347-3c7067801da2
	k8s.io/client-go v12.0.0+incompatible
)

replace k8s.io/client-go => k8s.io/client-go v0.0.0-20190620085101-78d2af792bab
