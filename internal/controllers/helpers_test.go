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
	. "github.com/onsi/gomega"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// setClusterReady patches the CAPI and CloudStack cluster with ready status true.
func setClusterReady(g *WithT, client client.Client) {
	setCAPIClusterReady(g, client)
	setCloudStackClusterReady(g, client)
}

// setCAPIClusterReady patches the cluster with ready status true.
func setCAPIClusterReady(g *WithT, client client.Client) {
	g.Eventually(func() error {
		ph, err := patch.NewHelper(dummies.CAPICluster, client)
		g.Expect(err).To(BeNil())
		dummies.CAPICluster.Status.InfrastructureReady = true

		return ph.Patch(ctx, dummies.CAPICluster, patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())
}

// setCloudStackClusterReady patches the cluster with ready status true.
func setCloudStackClusterReady(g *WithT, client client.Client) {
	g.Eventually(func() error {
		ph, err := patch.NewHelper(dummies.CSCluster, client)
		g.Expect(err).To(BeNil())
		dummies.CSCluster.Status.Ready = true

		return ph.Patch(ctx, dummies.CSCluster, patch.WithStatusObservedGeneration{})
	}, timeout).Should(Succeed())
}
