module sigs.k8s.io/cluster-api-provider-cloudstack/hack/tools

go 1.26.0

require sigs.k8s.io/cluster-api/hack/tools v0.0.0-20260912202113-4e137920d8cf

require (
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/go-logr/logr v1.4.4 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	k8s.io/apimachinery v0.36.3 // indirect
	k8s.io/klog/v2 v2.140.0 // indirect
	k8s.io/utils v0.0.0-20260319190234-28399d86e0b5 // indirect
	sigs.k8s.io/cluster-api v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/kubebuilder/docs/book/utils v0.0.0-20260611053758-c72b289c1ec8 // indirect
)

replace (
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v1.14.2
	sigs.k8s.io/cluster-api/test => sigs.k8s.io/cluster-api/test v1.14.2
)
