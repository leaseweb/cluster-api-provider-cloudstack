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
	"fmt"
	"testing"

	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/test/helpers"
)

func TestCloudStackClusterReconcilerIntegrationTests(t *testing.T) {
	var (
		reconciler      CloudStackClusterReconciler
		mockCtrl        *gomock.Controller
		mockCloudClient *mocks.MockClient
		recorder        *record.FakeRecorder
		ctx             context.Context
	)

	setup := func(t *testing.T) {
		t.Helper()
		mockCtrl = gomock.NewController(t)
		mockCloudClient = mocks.NewMockClient(mockCtrl)
		recorder = record.NewFakeRecorder(fakeEventBufferSize)
		reconciler = CloudStackClusterReconciler{
			Client:           testEnv.Client,
			Recorder:         recorder,
			WatchFilterValue: "",
		}
		ctx = context.TODO()
		ctx = logr.NewContext(ctx, ctrl.LoggerFrom(ctx))
	}

	teardown := func() {
		mockCtrl.Finish()
	}

	t.Run("Should patch back the CloudStackCluster as ready.", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.GetOrCreateAffinityGroup(gomock.Any()).AnyTimes()
		}
		expectClient(mockCloudClient.EXPECT())

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		// Create test objects
		g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
		// Set CAPI cluster as owner of the CloudStackCluster.
		dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
			Name:       dummies.CAPICluster.Name,
			UID:        types.UID("cluster-uid"),
		})
		g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())

		defer teardown()
		defer t.Cleanup(func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret1, ns)).To(Succeed())
		})

		result, err := reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dummies.CSCluster.Name, Namespace: ns.Name}})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).NotTo(BeZero())

		// Simulate the CloudStackFailureDomain controller setting the status of the failure domains to ready.
		g.Eventually(func() error {
			fds := &infrav1.CloudStackFailureDomainList{}
			getFailureDomains(ctx, g, testEnv, fds)
			markFailureDomainsAsReady(ctx, g, testEnv, fds)
			return nil
		}, timeout).Should(Succeed())

		// Reconcile again to check if the CloudStackCluster controller sets Status.Ready to true.
		result, err = reconciler.Reconcile(ctx, ctrl.Request{NamespacedName: types.NamespacedName{Name: dummies.CSCluster.Name, Namespace: ns.Name}})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Test that the CloudStackCluster controller sets Status.Ready to true.
		clusterKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSCluster.Name}
		cluster := &infrav1.CloudStackCluster{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, clusterKey, cluster)
			return err == nil && cluster.Status.Ready
		}, timeout).Should(BeTrue())

		g.Expect(cluster.GetFinalizers()).To(ContainElement(infrav1.ClusterFinalizer))
	})
}

func getFailureDomains(ctx context.Context, g *WithT, testEnv *helpers.TestEnvironment, fds *infrav1.CloudStackFailureDomainList) {
	g.Expect(testEnv.List(ctx, fds, client.InNamespace(dummies.CSCluster.Namespace), client.MatchingLabels(map[string]string{clusterv1.ClusterNameLabel: dummies.CAPICluster.Name}))).To(Succeed())
}

func markFailureDomainsAsReady(ctx context.Context, g *WithT, testEnv *helpers.TestEnvironment, fds *infrav1.CloudStackFailureDomainList) {
	for _, fd := range fds.Items {
		fdPatch := client.MergeFrom(fd.DeepCopy())
		fd.Status.Ready = true
		g.Expect(testEnv.Status().Patch(ctx, &fd, fdPatch)).To(Succeed())
	}
}
