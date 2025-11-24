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

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

func TestCloudStackIsolatedNetworkReconcilerIntegrationTests(t *testing.T) {
	var (
		reconciler             CloudStackIsolatedNetworkReconciler
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
		reconciler = CloudStackIsolatedNetworkReconciler{
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

	t.Run("Should add finalizer and set ready on new isolated network", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		mockCSClient.EXPECT().GetOrCreateIsolatedNetwork(
			gomock.AssignableToTypeOf(&infrav1.CloudStackFailureDomain{}),
			gomock.AssignableToTypeOf(&infrav1.CloudStackIsolatedNetwork{}),
		).Times(1)
		mockCSClient.EXPECT().AddClusterTag(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
		mockCSClient.EXPECT().AssociatePublicIPAddress(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(&cloudstack.PublicIpAddress{
			Id:                  dummies.PublicIPID,
			Associatednetworkid: dummies.ISONet1.ID,
			Ipaddress:           dummies.CSCluster.Spec.ControlPlaneEndpoint.Host,
		}, nil)
		mockCSClient.EXPECT().ReconcileLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)

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
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret2)).To(Succeed())
		// We use CSFailureDomain2 here because CSFailureDomain1 has an empty Spec.Zone.ID
		dummies.CSISONet1.Spec.FailureDomainName = dummies.CSFailureDomain2.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain2)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret2, dummies.CSFailureDomain2, dummies.CSISONet1, ns)).To(Succeed())
		}()

		// Check that the isolated network was created correctly before reconciling.
		isoNetKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSISONet1.Name}
		isoNet := &infrav1.CloudStackIsolatedNetwork{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, isoNetKey, isoNet)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSISONet1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the isolated network was updated correctly
		g.Eventually(func() bool {
			tempIsoNet := &infrav1.CloudStackIsolatedNetwork{}
			key := client.ObjectKeyFromObject(dummies.CSISONet1)
			if err := testEnv.Get(ctx, key, tempIsoNet); err == nil {
				if tempIsoNet.Status.Ready {
					return controllerutil.ContainsFinalizer(tempIsoNet, infrav1.IsolatedNetworkFinalizer)
				}
			}

			return false
		}, timeout).Should(BeTrue())
	})

	t.Run("Should delete associated resources and remove finalizer on deleted isolated network", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		mockCSClient.EXPECT().GetOrCreateIsolatedNetwork(
			gomock.AssignableToTypeOf(&infrav1.CloudStackFailureDomain{}),
			gomock.AssignableToTypeOf(&infrav1.CloudStackIsolatedNetwork{}),
		).Times(1)
		mockCSClient.EXPECT().AddClusterTag(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
		mockCSClient.EXPECT().AssociatePublicIPAddress(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(&cloudstack.PublicIpAddress{
			Id:                  dummies.PublicIPID,
			Associatednetworkid: dummies.ISONet1.ID,
			Ipaddress:           dummies.CSCluster.Spec.ControlPlaneEndpoint.Host,
		}, nil)
		mockCSClient.EXPECT().ReconcileLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		mockCSClient.EXPECT().DisposeIsoNetResources(gomock.Any(), gomock.Any()).Times(1)

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
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret2)).To(Succeed())
		// We use CSFailureDomain2 here because CSFailureDomain1 has an empty Spec.Zone.ID
		dummies.CSISONet1.Spec.FailureDomainName = dummies.CSFailureDomain2.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain2)).To(Succeed())
		dummies.CSISONet1.Finalizers = []string{infrav1.IsolatedNetworkFinalizer}
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret2, dummies.CSFailureDomain2, dummies.CSISONet1, ns)).To(Succeed())
		}()

		// Check that the isolated network was created correctly before reconciling.
		isoNetKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSISONet1.Name}
		isoNet := &infrav1.CloudStackIsolatedNetwork{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, isoNetKey, isoNet)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSISONet1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the isolated network was updated correctly.
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, isoNetKey, isoNet); err == nil {
				if isoNet.Status.Ready {
					return controllerutil.ContainsFinalizer(isoNet, infrav1.IsolatedNetworkFinalizer)
				}
			}

			return false
		}, timeout).Should(BeTrue())

		// Delete the isolated network
		g.Expect(testEnv.Delete(ctx, dummies.CSISONet1)).To(Succeed())
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, isoNetKey, isoNet)
			return err == nil && isoNet.DeletionTimestamp != nil
		}, timeout).Should(BeTrue())

		// Reconcile the isolated network again.
		result, err = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSISONet1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the isolated network was deleted correctly.
		g.Eventually(func() bool {
			if err := testEnv.Get(ctx, isoNetKey, isoNet); err != nil {
				return errors.IsNotFound(err)
			}
			return false
		}, timeout).Should(BeTrue())
	})

	t.Run("Should succeed if API load balancer is disabled", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.GetOrCreateIsolatedNetwork(gomock.Any(), gomock.Any()).Times(1)
			m.AddClusterTag(gomock.Any(), gomock.Any(), gomock.Any()).Times(2)
			m.AssociatePublicIPAddress(gomock.Any(), gomock.Any(), gomock.Any()).Times(1).Return(&cloudstack.PublicIpAddress{
				Id:                  dummies.PublicIPID,
				Associatednetworkid: dummies.ISONet1.ID,
				Ipaddress:           dummies.CSCluster.Spec.ControlPlaneEndpoint.Host,
			}, nil)
			m.ReconcileLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
		}
		expectClient(mockCSClient.EXPECT())

		// Create test objects
		g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
		dummies.CSCluster.Spec.APIServerLoadBalancer.Enabled = ptr.To(false)
		// Set CAPI cluster as owner of the CloudStackCluster.
		dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
			Name:       dummies.CAPICluster.Name,
			UID:        types.UID("cluster-uid"),
		})
		g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret2)).To(Succeed())
		// We use CSFailureDomain2 here because CSFailureDomain1 has an empty Spec.Zone.ID
		dummies.CSISONet1.Spec.FailureDomainName = dummies.CSFailureDomain2.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain2)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret2, dummies.CSFailureDomain2, dummies.CSISONet1, ns)).To(Succeed())
		}()

		// Check that the isolated network was created correctly before reconciling.
		isoNetKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSISONet1.Name}
		isoNet := &infrav1.CloudStackIsolatedNetwork{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, isoNetKey, isoNet)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSISONet1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the isolated network was updated correctly
		g.Eventually(func() bool {
			tempIsoNet := &infrav1.CloudStackIsolatedNetwork{}
			key := client.ObjectKeyFromObject(dummies.CSISONet1)
			if err := testEnv.Get(ctx, key, tempIsoNet); err == nil {
				if tempIsoNet.Status.Ready {
					return controllerutil.ContainsFinalizer(tempIsoNet, infrav1.IsolatedNetworkFinalizer)
				}
			}

			return false
		}, timeout).Should(BeTrue())
	})

	t.Run("Should skip IP assignment and load balancer reconciliation if the cluster is externally managed", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).ToNot(HaveOccurred())
		dummies.SetDummyVars(ns.Name)

		expectClient := func(m *mocks.MockClientMockRecorder) {
			m.GetOrCreateIsolatedNetwork(gomock.Any(), gomock.Any()).Times(1)
			m.AddClusterTag(gomock.Any(), gomock.Any(), gomock.Any()).Times(1)
			m.AssociatePublicIPAddress(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
			m.ReconcileLoadBalancer(gomock.Any(), gomock.Any(), gomock.Any()).Times(0)
		}
		expectClient(mockCSClient.EXPECT())

		// Create test objects
		g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
		// Set CAPI cluster as owner of the CloudStackCluster.
		dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
			Kind:       "Cluster",
			APIVersion: clusterv1.GroupVersion.String(),
			Name:       dummies.CAPICluster.Name,
			UID:        types.UID("cluster-uid"),
		})
		dummies.CSCluster.Annotations = map[string]string{
			clusterv1.ManagedByAnnotation: "true",
		}
		g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret2)).To(Succeed())
		// We use CSFailureDomain2 here because CSFailureDomain1 has an empty Spec.Zone.ID
		dummies.CSISONet1.Spec.FailureDomainName = dummies.CSFailureDomain2.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain2)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		defer func() {
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret2, dummies.CSFailureDomain2, dummies.CSISONet1, ns)).To(Succeed())
		}()

		// Check that the isolated network was created correctly before reconciling.
		isoNetKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSISONet1.Name}
		isoNet := &infrav1.CloudStackIsolatedNetwork{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, isoNetKey, isoNet)
			return err == nil
		}, timeout).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSISONet1.Name,
			},
		})
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Check that the isolated network was updated correctly
		g.Eventually(func() bool {
			tempIsoNet := &infrav1.CloudStackIsolatedNetwork{}
			key := client.ObjectKeyFromObject(dummies.CSISONet1)
			if err := testEnv.Get(ctx, key, tempIsoNet); err == nil {
				if tempIsoNet.Status.Ready {
					return controllerutil.ContainsFinalizer(tempIsoNet, infrav1.IsolatedNetworkFinalizer)
				}
			}

			return false
		}, timeout).Should(BeTrue())
	})
}
