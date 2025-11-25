module sigs.k8s.io/cluster-api-provider-cloudstack/hack/tools

go 1.24.0

require sigs.k8s.io/cluster-api/hack/tools v0.0.0-20250805173327-a7b9f27af519

require (
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	k8s.io/apimachinery v0.33.3 // indirect
	k8s.io/klog/v2 v2.130.1 // indirect
	k8s.io/utils v0.0.0-20241104100929-3ea5e8cea738 // indirect
	sigs.k8s.io/cluster-api v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/kubebuilder/docs/book/utils v0.0.0-20211028165026-57688c578b5d // indirect
)

replace (
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v1.11.3
	sigs.k8s.io/cluster-api/test => sigs.k8s.io/cluster-api/test v1.11.3
)
