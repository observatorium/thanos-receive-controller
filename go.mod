module github.com/observatorium/thanos-receive-controller

go 1.12

replace (
	github.com/coreos/prometheus-operator => github.com/coreos/prometheus-operator v0.30.1
	k8s.io/api => k8s.io/api v0.0.0-20190606204050-af9c91bd2759
	k8s.io/client-go => k8s.io/client-go v11.0.1-0.20190606204521-b8faab9c5193+incompatible
)

require (
	github.com/google/gofuzz v1.0.0 // indirect
	github.com/imdario/mergo v0.3.7 // indirect
	github.com/oklog/run v1.0.0
	github.com/spf13/pflag v1.0.3 // indirect
	golang.org/x/crypto v0.0.0-20190611184440-5c40567a22f8 // indirect
	golang.org/x/net v0.0.0-20190613194153-d28f0bde5980 // indirect
	golang.org/x/oauth2 v0.0.0-20190604053449-0f29369cfe45 // indirect
	golang.org/x/time v0.0.0-20190308202827-9d24e82272b4 // indirect
	k8s.io/api v0.0.0-00010101000000-000000000000 // indirect
	k8s.io/apimachinery v0.0.0-20190612125636-6a5db36e93ad // indirect
	k8s.io/client-go v0.0.0-00010101000000-000000000000
	k8s.io/klog v0.3.3 // indirect
	k8s.io/utils v0.0.0-20190607212802-c55fbcfc754a // indirect
)
