/*
Copyright 2024 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"

	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	v1beta1patch "sigs.k8s.io/cluster-api/util/deprecated/v1beta1/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

// markClustersReady patches Cluster & CloudStackCluster status to satisfy controller readiness checks (v1beta2 semantics).
func markClustersReady(ctx context.Context, g *WithT, c client.Client, cluster *clusterv1.Cluster, csCluster *infrav1.CloudStackCluster) {
	g.Eventually(func() error {
		ph, err := v1beta1patch.NewHelper(cluster, c)
		if err != nil {
			return err
		}
		if cluster.Status.Initialization.InfrastructureProvisioned == nil || !*cluster.Status.Initialization.InfrastructureProvisioned {
			cluster.Status.Initialization.InfrastructureProvisioned = ptr.To(true)
		}
		return ph.Patch(ctx, cluster, v1beta1patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())

	g.Eventually(func() error {
		ph, err := v1beta1patch.NewHelper(csCluster, c)
		if err != nil {
			return err
		}
		if !csCluster.Status.Ready {
			csCluster.Status.Ready = true
		}
		return ph.Patch(ctx, csCluster, v1beta1patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())
}
