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
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

// setClusterReady patches the CAPI and CloudStack cluster with ready status true.
func setClusterReady(g *WithT, client client.Client) {
	setCAPIClusterReady(g, client)
	setCloudStackClusterReady(g, client)
}

// checkClusterReady checks if the CAPI and CloudStack cluster are ready.
func checkClusterReady(ctx context.Context, g *WithT, client client.Client) {
	checkCAPIClusterReady(ctx, g, client)
	checkCloudStackClusterReady(ctx, g, client)
}

// checkCAPIClusterReady checks if the CAPI cluster is ready.
func checkCAPIClusterReady(ctx context.Context, g *WithT, client client.Client) {
	g.Eventually(func() bool {
		capiCluster := &clusterv1.Cluster{}
		if err := client.Get(ctx, types.NamespacedName{Namespace: dummies.CAPICluster.Namespace, Name: dummies.CAPICluster.Name}, capiCluster); err == nil {
			if *capiCluster.Status.Initialization.InfrastructureProvisioned {
				return true
			}
		}

		return false
	}, timeout).Should(BeTrue())
}

// checkCloudStackClusterReady checks if the CloudStack cluster is ready.
func checkCloudStackClusterReady(ctx context.Context, g *WithT, client client.Client) {
	g.Eventually(func() bool {
		csCluster := &infrav1.CloudStackCluster{}
		if err := client.Get(ctx, types.NamespacedName{Namespace: dummies.CSCluster.Namespace, Name: dummies.CSCluster.Name}, csCluster); err == nil {
			if csCluster.Status.Ready {
				return true
			}
		}

		return false
	}, timeout).Should(BeTrue())
}

// setCAPIClusterReady patches the cluster with ready status true.
func setCAPIClusterReady(g *WithT, client client.Client) {
	g.Eventually(func() error {
		ph, err := patch.NewHelper(dummies.CAPICluster, client)
		g.Expect(err).ToNot(HaveOccurred())
		dummies.CAPICluster.Status.InfrastructureReady = true

		return ph.Patch(ctx, dummies.CAPICluster, patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())
}

// setCloudStackClusterReady patches the cluster with ready status true.
func setCloudStackClusterReady(g *WithT, client client.Client) {
	g.Eventually(func() error {
		ph, err := patch.NewHelper(dummies.CSCluster, client)
		g.Expect(err).ToNot(HaveOccurred())
		dummies.CSCluster.Status.Ready = true

		return ph.Patch(ctx, dummies.CSCluster, patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())
}
