module sigs.k8s.io/cluster-api-provider-cloudstack/hack/tools

go 1.21

require sigs.k8s.io/cluster-api/hack/tools v0.0.0-20240311182002-eeab3ceb5ecc

require (
	github.com/blang/semver v3.5.1+incompatible // indirect
	github.com/go-logr/logr v1.4.1 // indirect
	github.com/onsi/gomega v1.33.1 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	golang.org/x/net v0.25.0 // indirect
	golang.org/x/sys v0.20.0 // indirect
	golang.org/x/tools/go/vcs v0.1.0-deprecated // indirect
	k8s.io/apimachinery v0.27.7 // indirect
	k8s.io/klog/v2 v2.110.1 // indirect
	k8s.io/utils v0.0.0-20230209194617-a36077c30491 // indirect
	sigs.k8s.io/cluster-api v0.0.0-00010101000000-000000000000 // indirect
	sigs.k8s.io/kubebuilder/docs/book/utils v0.0.0-20211028165026-57688c578b5d // indirect
)

replace (
	sigs.k8s.io/cluster-api => sigs.k8s.io/cluster-api v1.5.7
	sigs.k8s.io/cluster-api/test => sigs.k8s.io/cluster-api/test v1.5.7
)
