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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

func TestCloudStackFailureDomainReconcilerIntegrationTests(t *testing.T) {
	var (
		reconciler             CloudStackFailureDomainReconciler
		mockCtrl               *gomock.Controller
		mockClientScopeFactory *scope.MockClientScopeFactory
		mockCSClient           *mocks.MockClient
		recorder               *record.FakeRecorder
		ctx                    context.Context
	)

	setup := func(t *testing.T) {
		t.Helper()
		mockCtrl = gomock.NewController(t)
		mockClientScopeFactory = scope.NewMockClientScopeFactory(mockCtrl, "")
		mockCSClient = mockClientScopeFactory.MockCSClients().MockCSUser()
		recorder = record.NewFakeRecorder(fakeEventBufferSize)
		reconciler = CloudStackFailureDomainReconciler{
			Client:           testEnv.Client,
			Recorder:         recorder,
			ScopeFactory:     mockClientScopeFactory,
			WatchFilterValue: "",
		}
		ctx = t.Context()
		ctx = logr.NewContext(ctx, ctrl.LoggerFrom(ctx))
	}

	teardown := func() {
		mockCtrl.Finish()
	}

	t.Run("Should add finalizer and set ready on new failure domain", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.ResolveZone(gomock.Any()).MinTimes(1)
			m.ResolveNetworkForZone(gomock.Any()).AnyTimes().Do(
				func(arg1 interface{}) {
					arg1.(*infrav1.CloudStackZoneSpec).Network.ID = "SomeID"
					arg1.(*infrav1.CloudStackZoneSpec).Network.Type = cloud.NetworkTypeShared
				}).MinTimes(1)
		}
		expectClient(mockCSClient.EXPECT())

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
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret1, dummies.CSFailureDomain1, ns)).To(Succeed())
		}()

		// Check that the failure domain was created correctly before reconciling.
		fdKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSFailureDomain1.Name}
		fd := &infrav1.CloudStackFailureDomain{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, fdKey, fd)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSFailureDomain1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the failure domain was updated correctly
		updatedFD := &infrav1.CloudStackFailureDomain{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, fdKey, updatedFD)
			return err == nil &&
				updatedFD.Status.Ready &&
				controllerutil.ContainsFinalizer(updatedFD, infrav1.FailureDomainFinalizer)
		}, timeout).Should(BeTrue())
	})

	t.Run("Should handle deletion when no owned resources exist", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.ResolveZone(gomock.Any()).Times(1)
			m.ResolveNetworkForZone(gomock.Any()).AnyTimes().Do(
				func(arg1 interface{}) {
					arg1.(*infrav1.CloudStackZoneSpec).Network.ID = "SomeID"
					arg1.(*infrav1.CloudStackZoneSpec).Network.Type = cloud.NetworkTypeShared
				}).Times(1)
		}
		expectClient(mockCSClient.EXPECT())

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
		// Already add finalizer to the failure domain so we can skip the first reconcile.
		dummies.CSFailureDomain1.Finalizers = []string{infrav1.FailureDomainFinalizer}
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret1, dummies.CSFailureDomain1, ns)).To(Succeed())
		}()

		// Check that the failure domain was created correctly before reconciling.
		fdKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSFailureDomain1.Name}
		fd := &infrav1.CloudStackFailureDomain{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, fdKey, fd)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSFailureDomain1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the failure domain was updated correctly
		updatedFD := &infrav1.CloudStackFailureDomain{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, fdKey, updatedFD)
			return err == nil &&
				updatedFD.Status.Ready &&
				controllerutil.ContainsFinalizer(updatedFD, infrav1.FailureDomainFinalizer)
		}, timeout).Should(BeTrue())

		// Delete the failure domain
		g.Expect(testEnv.Delete(ctx, dummies.CSFailureDomain1)).To(Succeed())
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, fdKey, updatedFD)
			return err == nil &&
				updatedFD.DeletionTimestamp != nil
		}, timeout).Should(BeTrue())

		// Reconcile the failure domain once more.
		result, err = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSFailureDomain1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the failure domain was deleted
		deletedFD := &infrav1.CloudStackFailureDomain{}
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, fdKey, deletedFD); err != nil {
				return errors.IsNotFound(err)
			}
			return false
		}, timeout).Should(BeTrue())
	})
}
