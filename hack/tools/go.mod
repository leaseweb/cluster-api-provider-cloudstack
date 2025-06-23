module sigs.k8s.io/cluster-api-provider-cloudstack/hack/tools

go 1.23

require sigs.k8s.io/cluster-api/hack/tools v0.0.0-20240812172430-21d2a534190f

require (
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/go-logr/logr v1.4.2 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	k8s.io/apimachinery v0.31.10 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/utils v0.0.0-20240711033017-18e509b52bc8 // indirect
	sigs.k8s.io/cluster-api v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/kubebuilder/docs/book/utils v0.0.0-20211028165026-57688c578b5d // indirect
)

replace (
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v1.9.7
	sigs.k8s.io/cluster-api/test => sigs.k8s.io/cluster-api/test v1.9.7
)
