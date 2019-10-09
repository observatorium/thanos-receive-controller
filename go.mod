module github.com/observatorium/thanos-receive-controller

go 1.12

replace github.com/prometheus/prometheus => github.com/prometheus/prometheus v0.0.0-20190710134608-e5b22494857d

require (
	github.com/go-kit/kit v0.9.0
	github.com/google/go-cmp v0.3.1
	github.com/google/gofuzz v1.0.0 // indirect
	github.com/googleapis/gnostic v0.3.0 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/improbable-eng/thanos v0.6.0
	github.com/oklog/run v1.0.0
	github.com/pkg/errors v0.8.1
	github.com/prometheus/client_golang v1.0.0
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	google.golang.org/appengine v1.5.0 // indirect
	k8s.io/api v0.0.0-20190409021203-6e4e0e4f393b
	k8s.io/apimachinery v0.0.0-20190404173353-6a84e37a896d
	k8s.io/client-go v11.0.1-0.20190409021438-1a26190bd76a+incompatible
)
