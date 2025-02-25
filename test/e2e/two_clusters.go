/*
Copyright 2021 The Kubernetes Authors.

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

package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"sigs.k8s.io/cluster-api/test/framework/clusterctl"
	"sigs.k8s.io/cluster-api/util"
)

// TwoClustersSpec implements a test that verifies two clusters can co-exist.
func TwoClustersSpec(ctx context.Context, inputGetter func() CommonSpecInput) {

	const specName = "two-clusters"

	var (
		input             CommonSpecInput
		namespace1        *corev1.Namespace
		namespace2        *corev1.Namespace
		cancelWatches1    context.CancelFunc
		cancelWatches2    context.CancelFunc
		clusterResources1 *clusterctl.ApplyClusterTemplateAndWaitResult
		clusterResources2 *clusterctl.ApplyClusterTemplateAndWaitResult
	)

	createCluster := func(flavor string, namespace *corev1.Namespace, resources *clusterctl.ApplyClusterTemplateAndWaitResult) {
		clusterctl.ApplyClusterTemplateAndWait(ctx, clusterctl.ApplyClusterTemplateAndWaitInput{
			ClusterProxy:    input.BootstrapClusterProxy,
			CNIManifestPath: input.E2EConfig.GetVariable(CNIPath),
			ConfigCluster: clusterctl.ConfigClusterInput{
				LogFolder:                filepath.Join(input.ArtifactFolder, "clusters", input.BootstrapClusterProxy.GetName()),
				ClusterctlConfigPath:     input.ClusterctlConfigPath,
				KubeconfigPath:           input.BootstrapClusterProxy.GetKubeconfigPath(),
				InfrastructureProvider:   clusterctl.DefaultInfrastructureProvider,
				Flavor:                   flavor,
				Namespace:                namespace.Name,
				ClusterName:              fmt.Sprintf("%s-%s", specName, util.RandomString(6)),
				KubernetesVersion:        input.E2EConfig.GetVariable(KubernetesVersion),
				ControlPlaneMachineCount: ptr.To[int64](1),
				WorkerMachineCount:       ptr.To[int64](1),
			},
			WaitForClusterIntervals:      input.E2EConfig.GetIntervals(specName, "wait-cluster"),
			WaitForControlPlaneIntervals: input.E2EConfig.GetIntervals(specName, "wait-control-plane"),
			WaitForMachineDeployments:    input.E2EConfig.GetIntervals(specName, "wait-worker-nodes"),
		}, resources)
	}

	BeforeEach(func() {
		Expect(ctx).NotTo(BeNil(), "ctx is required for %s spec", specName)
		input = inputGetter()
		Expect(input.E2EConfig).ToNot(BeNil(), "Invalid argument. input.E2EConfig can't be nil when calling %s spec", specName)
		Expect(input.ClusterctlConfigPath).To(BeAnExistingFile(), "Invalid argument. input.ClusterctlConfigPath must be an existing file when calling %s spec", specName)
		Expect(input.BootstrapClusterProxy).ToNot(BeNil(), "Invalid argument. input.BootstrapClusterProxy can't be nil when calling %s spec", specName)
		Expect(os.MkdirAll(input.ArtifactFolder, 0750)).To(Succeed(), "Invalid argument. input.ArtifactFolder can't be created for %s spec", specName)
		Expect(input.E2EConfig.Variables).To(HaveKey(KubernetesVersion))
		Expect(input.E2EConfig.Variables).To(HaveValidVersion(input.E2EConfig.GetVariable(KubernetesVersion)))

		// Set up namespaces to host objects for this spec and create watchers for the namespace events.
		namespace1, cancelWatches1 = setupSpecNamespace(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder)
		namespace2, cancelWatches2 = setupSpecNamespace(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder)
		clusterResources1 = new(clusterctl.ApplyClusterTemplateAndWaitResult)
		clusterResources2 = new(clusterctl.ApplyClusterTemplateAndWaitResult)
	})

	It("should successfully add and remove a second cluster without breaking the first cluster", func() {
		mgmtClient := input.BootstrapClusterProxy.GetClient()

		By("Create the first cluster and verify that it's ready")
		createCluster(clusterctl.DefaultFlavor, namespace1, clusterResources1)
		Expect(IsClusterReady(ctx, mgmtClient, clusterResources1.Cluster)).To(BeTrue())

		By("Create the second cluster and verify that it's ready")
		createCluster("second-cluster", namespace2, clusterResources2)
		Expect(IsClusterReady(ctx, mgmtClient, clusterResources2.Cluster)).To(BeTrue())

		By("Delete the second cluster")
		dumpSpecResourcesAndCleanup(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder, namespace2,
			cancelWatches2, clusterResources2.Cluster, input.E2EConfig.GetIntervals, false)

		By("Verify the second cluster is gone")
		Expect(ClusterExists(ctx, mgmtClient, clusterResources2.Cluster)).To(BeFalse())

		By("Verify the first cluster is still ready")
		Expect(IsClusterReady(ctx, mgmtClient, clusterResources1.Cluster)).To(BeTrue())

		By("PASSED!")
	})

	AfterEach(func() {
		// Dumps all the resources in the spec namespace, then cleanups the cluster object and the spec namespace itself.
		dumpSpecResourcesAndCleanup(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder, namespace1, cancelWatches1, clusterResources1.Cluster, input.E2EConfig.GetIntervals, input.SkipCleanup)
		if clusterResources2.Cluster != nil && ClusterExists(ctx, input.BootstrapClusterProxy.GetClient(), clusterResources2.Cluster) {
			dumpSpecResourcesAndCleanup(ctx, specName, input.BootstrapClusterProxy, input.ArtifactFolder, namespace2, cancelWatches2, clusterResources2.Cluster, input.E2EConfig.GetIntervals, input.SkipCleanup)
		}
	})
}
