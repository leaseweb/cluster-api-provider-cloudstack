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
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

func TestCloudStackAffinityGroupReconcilerIntegrationTests(t *testing.T) {
	var (
		reconciler             CloudStackAffinityGroupReconciler
		mockCtrl               *gomock.Controller
		mockClientScopeFactory *scope.MockClientScopeFactory
		mockCSClient           *mocks.MockClient
		mockCSUser             *mocks.MockClient
		recorder               *record.FakeRecorder
		ctx                    context.Context
	)

	setup := func(t *testing.T) {
		t.Helper()
		mockCtrl = gomock.NewController(t)
		mockClientScopeFactory = scope.NewMockClientScopeFactory(mockCtrl, "")
		mockCSClient = mockClientScopeFactory.MockCSClients().MockCSClient()
		mockCSUser = mockClientScopeFactory.MockCSClients().MockCSUser()
		recorder = record.NewFakeRecorder(fakeEventBufferSize)
		reconciler = CloudStackAffinityGroupReconciler{
			Client:           testEnv.Client,
			Recorder:         recorder,
			ScopeFactory:     mockClientScopeFactory,
			WatchFilterValue: "",
		}
		ctx = context.TODO()
		ctx = logr.NewContext(ctx, ctrl.LoggerFrom(ctx))
	}

	teardown := func() {
		mockCtrl.Finish()
	}
	t.Run("Should patch back the affinity group as ready after calling GetOrCreateAffinityGroup", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.GetOrCreateAffinityGroup(gomock.Any()).AnyTimes()
		}
		expectClient(mockCSUser.EXPECT())

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		// Modify failure domain name the same way the cluster controller would.
		dummies.CSAffinityGroup.Spec.FailureDomainName = dummies.CSFailureDomain1.Spec.Name

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
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSAffinityGroup)).To(Succeed())

		defer teardown()
		defer t.Cleanup(func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.CSFailureDomain1, dummies.ACSEndpointSecret1, dummies.CSAffinityGroup, ns)).To(Succeed())
		})

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dummies.CSAffinityGroup.Name,
				Namespace: ns.Name,
			},
		}
		_, err = reconciler.Reconcile(ctx, req)
		g.Expect(err).ToNot(HaveOccurred())

		// Test that the AffinityGroup controller sets Status.Ready to true.
		affinityGroupKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSAffinityGroup.Name}
		affinityGroup := &infrav1.CloudStackAffinityGroup{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, affinityGroupKey, affinityGroup)
			return err == nil && affinityGroup.Status.Ready
		}, timeout).Should(BeTrue())

		g.Expect(affinityGroup.GetFinalizers()).To(ContainElement(infrav1.AffinityGroupFinalizer))
	})

	t.Run("Should remove affinity group finalizer if corresponding affinity group is not present on Cloudstack", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.GetOrCreateAffinityGroup(gomock.Any()).AnyTimes()
		}
		expectClient(mockCSClient.EXPECT())
		expectClient(mockCSUser.EXPECT())

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		// Modify failure domain name the same way the cluster controller would.
		dummies.CSAffinityGroup.Spec.FailureDomainName = dummies.CSFailureDomain1.Spec.Name

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
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSAffinityGroup)).To(Succeed())

		defer teardown()
		defer t.Cleanup(func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.CSFailureDomain1, dummies.ACSEndpointSecret1, dummies.CSAffinityGroup, ns)).To(Succeed())
		})

		// Check that the affinity group was created correctly before reconciling.
		affinityGroupKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSAffinityGroup.Name}
		affinityGroup := &infrav1.CloudStackAffinityGroup{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, affinityGroupKey, affinityGroup)
			return err == nil
		}, timeout).Should(BeTrue())

		req := ctrl.Request{
			NamespacedName: types.NamespacedName{
				Name:      dummies.CSAffinityGroup.Name,
				Namespace: ns.Name,
			},
		}
		_, err = reconciler.Reconcile(ctx, req)
		g.Expect(err).ToNot(HaveOccurred())

		// Test that the AffinityGroup controller sets Status.Ready to true.
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, affinityGroupKey, affinityGroup)
			return err == nil && affinityGroup.Status.Ready
		}, timeout).Should(BeTrue())

		mockCSClient.EXPECT().FetchAffinityGroup(gomock.Any()).Do(func(arg1 interface{}) {
			arg1.(*cloud.AffinityGroup).ID = ""
		}).AnyTimes().Return(nil)
		g.Expect(testEnv.Delete(ctx, dummies.CSAffinityGroup)).To(Succeed())
		// Wait till the affinity group is marked for deletion
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, affinityGroupKey, affinityGroup); err != nil {
				return false
			}
			return !affinityGroup.DeletionTimestamp.IsZero()
		}, timeout).Should(BeTrue())
		_, err = reconciler.Reconcile(ctx, req)
		g.Expect(err).ToNot(HaveOccurred())

		// Once the affinity group id was set to "" the controller should remove the finalizer and unblock deleting affinity group resource
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, affinityGroupKey, affinityGroup); err != nil {
				return errors.IsNotFound(err)
			}
			return false
		}, timeout).Should(BeTrue())
	})
}
