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
	"regexp"
	"runtime/debug"
	"testing"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

func TestCloudStackMachineReconcilerIntegrationTests(t *testing.T) {
	var (
		reconciler             CloudStackMachineReconciler
		mockCtrl               *gomock.Controller
		mockClientScopeFactory *scope.MockClientScopeFactory
		mockCSClient           *mocks.MockClient
		mockCSCUser            *mocks.MockClient
		recorder               *record.FakeRecorder
		ctx                    context.Context
	)

	setup := func(t *testing.T) {
		t.Helper()
		mockCtrl = gomock.NewController(t)
		mockClientScopeFactory = scope.NewMockClientScopeFactory(mockCtrl, "")
		mockCSClient = mockClientScopeFactory.MockCSClients().MockCSClient()
		mockCSCUser = mockClientScopeFactory.MockCSClients().MockCSUser()
		recorder = record.NewFakeRecorder(fakeEventBufferSize)
		reconciler = CloudStackMachineReconciler{
			Client:           testEnv.Client,
			Recorder:         recorder,
			ScopeFactory:     mockClientScopeFactory,
			WatchFilterValue: "",
		}
		ctx = context.TODO()
	}

	teardown := func() {
		mockCtrl.Finish()
	}

	t.Run("Should call CreateVMInstance and set Status.Ready to true", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).To(BeNil())
		dummies.SetDummyVars(ns.Name)

		mockCSCUser.EXPECT().GetVMInstanceByID(gomock.AssignableToTypeOf(*dummies.CSMachine1.Spec.InstanceID)).
			Return(nil, nil).Times(1)
		mockCSCUser.EXPECT().CreateVMInstance(
			gomock.AssignableToTypeOf(&infrav1.CloudStackMachine{}),
			gomock.AssignableToTypeOf(&clusterv1.Machine{}),
			gomock.AssignableToTypeOf(&infrav1.CloudStackFailureDomain{}),
			gomock.AssignableToTypeOf(&infrav1.CloudStackAffinityGroup{}),
			gomock.AssignableToTypeOf(""),
		).Return(&cloudstack.VirtualMachine{
			Id:    *dummies.CSMachine1.Spec.InstanceID,
			Name:  dummies.CSMachine1.Name,
			State: cloud.InstanceStateRunning,
		}, nil).Times(1)
		mockCSCUser.EXPECT().GetInstanceAddresses(gomock.AssignableToTypeOf(&cloudstack.VirtualMachine{
			Id:    *dummies.CSMachine1.Spec.InstanceID,
			Name:  dummies.CSMachine1.Name,
			State: cloud.InstanceStateRunning,
		})).Return([]corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "192.168.1.1",
			},
		}, nil).Times(1)

		// Create test objects
		g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
		dummies.CSCluster.Spec.FailureDomains = dummies.CSCluster.Spec.FailureDomains[:1]
		dummies.CSCluster.Spec.FailureDomains[0].Name = dummies.CSFailureDomain1.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
		// Set owner ref from CAPI cluster to CS Cluster and patch back the CS Cluster.
		g.Eventually(func() error {
			ph, err := patch.NewHelper(dummies.CSCluster, testEnv.Client)
			g.Expect(err).To(BeNil())
			dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
				Kind:       "Cluster",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       dummies.CAPICluster.Name,
				UID:        types.UID("cluster-uid"),
			})

			return ph.Patch(ctx, dummies.CSCluster, patch.WithStatusObservedGeneration{})
		}, timeout).Should(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		// Point CAPI machine Bootstrap secret ref to dummy bootstrap secret.
		dummies.CAPIMachine.Spec.Bootstrap.DataSecretName = &dummies.BootstrapSecret.Name
		g.Expect(testEnv.Create(ctx, dummies.BootstrapSecret)).To(Succeed())

		// Create CAPI and CS machines.
		g.Expect(testEnv.Create(ctx, dummies.CAPIMachine)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSMachine1)).To(Succeed())

		// Fetch the CS Machine that was created.
		key := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSMachine1.Name}
		g.Eventually(func() error {
			return testEnv.Get(ctx, key, dummies.CSMachine1)
		}, timeout).Should(BeNil())

		// Set owner ref from CAPI machine to CS machine and patch back the CS machine.
		g.Eventually(func() error {
			ph, err := patch.NewHelper(dummies.CSMachine1, testEnv.Client)
			g.Expect(err).To(BeNil())
			dummies.CSMachine1.OwnerReferences = append(dummies.CSMachine1.OwnerReferences, metav1.OwnerReference{
				Kind:       "Machine",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       dummies.CAPIMachine.Name,
				UID:        "uniqueness",
			})

			return ph.Patch(ctx, dummies.CSMachine1, patch.WithStatusObservedGeneration{})
		}, timeout).Should(Succeed())

		setClusterReady(g, testEnv.Client)

		defer func() {
			if err := recover(); err != nil {
				g.Fail(FailMessage(err))
			}
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret1, dummies.CSFailureDomain1, dummies.CSISONet1, dummies.BootstrapSecret, dummies.CAPIMachine, dummies.CSMachine1, ns)).To(Succeed())
		}()

		// Check that the machine was created correctly before reconciling.
		machineKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSMachine1.Name}
		machine := &infrav1.CloudStackMachine{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, machineKey, machine)
			return err == nil
		}, timeout).WithPolling(pollInterval).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSMachine1.Name,
			},
		})
		g.Expect(err).To(BeNil())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Eventually the machine should set ready to true.
		g.Eventually(func() bool {
			tempMachine := &infrav1.CloudStackMachine{}
			key := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSMachine1.Name}
			if err := testEnv.Get(ctx, key, tempMachine); err == nil {
				if tempMachine.Status.Ready {
					return len(tempMachine.ObjectMeta.Finalizers) > 0
				}
			}

			return false
		}, timeout).WithPolling(pollInterval).Should(BeTrue())
	})

	t.Run("Should call DestroyVMInstance when CS machine deleted", func(t *testing.T) {
		g := NewWithT(t)

		setup(t)
		defer teardown()

		ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("integ-test-%s", util.RandomString(5)))
		g.Expect(err).To(BeNil())
		dummies.SetDummyVars(ns.Name)

		gomock.InOrder(
			mockCSCUser.EXPECT().GetVMInstanceByID(gomock.AssignableToTypeOf(*dummies.CSMachine1.Spec.InstanceID)).
				Return(&cloudstack.VirtualMachine{
					Id:    *dummies.CSMachine1.Spec.InstanceID,
					Name:  dummies.CSMachine1.Name,
					State: cloud.InstanceStateRunning,
				}, nil).Times(2),
			mockCSCUser.EXPECT().GetVMInstanceByID(gomock.AssignableToTypeOf(*dummies.CSMachine1.Spec.InstanceID)).
				Return(&cloudstack.VirtualMachine{
					Id:    *dummies.CSMachine1.Spec.InstanceID,
					Name:  dummies.CSMachine1.Name,
					State: cloud.InstanceStateDestroyed,
				}, nil).Times(1),
		)
		mockCSCUser.EXPECT().GetInstanceAddresses(gomock.AssignableToTypeOf(&cloudstack.VirtualMachine{
			Id:    *dummies.CSMachine1.Spec.InstanceID,
			Name:  dummies.CSMachine1.Name,
			State: cloud.InstanceStateRunning,
		})).Return([]corev1.NodeAddress{
			{
				Type:    corev1.NodeInternalIP,
				Address: "192.168.1.1",
			},
		}, nil).Times(1)
		mockCSClient.EXPECT().DestroyVMInstance(gomock.AssignableToTypeOf(&infrav1.CloudStackMachine{
			ObjectMeta: metav1.ObjectMeta{
				Name:      dummies.CSMachine1.Name,
				Namespace: ns.Name,
			},
			Spec: infrav1.CloudStackMachineSpec{
				InstanceID: dummies.CSMachine1.Spec.InstanceID,
			},
		})).Return(fmt.Errorf("VM deletion in progress")).Times(1)

		// Create test objects
		g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
		dummies.CSCluster.Spec.FailureDomains = dummies.CSCluster.Spec.FailureDomains[:1]
		dummies.CSCluster.Spec.FailureDomains[0].Name = dummies.CSFailureDomain1.Spec.Name
		g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
		// Set owner ref from CAPI cluster to CS Cluster and patch back the CS Cluster.
		g.Eventually(func() error {
			ph, err := patch.NewHelper(dummies.CSCluster, testEnv.Client)
			g.Expect(err).To(BeNil())
			dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
				Kind:       "Cluster",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       dummies.CAPICluster.Name,
				UID:        types.UID("cluster-uid"),
			})

			return ph.Patch(ctx, dummies.CSCluster, patch.WithStatusObservedGeneration{})
		}, timeout).Should(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

		// Point CAPI machine Bootstrap secret ref to dummy bootstrap secret.
		dummies.CAPIMachine.Spec.Bootstrap.DataSecretName = &dummies.BootstrapSecret.Name
		g.Expect(testEnv.Create(ctx, dummies.BootstrapSecret)).To(Succeed())

		// Create CAPI and CS machines.
		g.Expect(testEnv.Create(ctx, dummies.CAPIMachine)).To(Succeed())
		g.Expect(testEnv.Create(ctx, dummies.CSMachine1)).To(Succeed())

		// Fetch the CS Machine that was created.
		key := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSMachine1.Name}
		g.Eventually(func() error {
			return testEnv.Get(ctx, key, dummies.CSMachine1)
		}, timeout).Should(BeNil())

		// Set owner ref from CAPI machine to CS machine and patch back the CS machine.
		g.Eventually(func() error {
			ph, err := patch.NewHelper(dummies.CSMachine1, testEnv.Client)
			g.Expect(err).To(BeNil())
			dummies.CSMachine1.OwnerReferences = append(dummies.CSMachine1.OwnerReferences, metav1.OwnerReference{
				Kind:       "Machine",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       dummies.CAPIMachine.Name,
				UID:        "uniqueness",
			})
			controllerutil.AddFinalizer(dummies.CSMachine1, infrav1.MachineFinalizer)

			return ph.Patch(ctx, dummies.CSMachine1, patch.WithStatusObservedGeneration{})
		}, timeout).Should(Succeed())

		setClusterReady(g, testEnv.Client)

		defer func() {
			if err := recover(); err != nil {
				g.Fail(FailMessage(err))
			}
			g.Expect(testEnv.Cleanup(ctx, dummies.CAPICluster, dummies.CSCluster, dummies.ACSEndpointSecret1, dummies.CSFailureDomain1, dummies.CSISONet1, dummies.BootstrapSecret, dummies.CAPIMachine, dummies.CSMachine1, ns)).To(Succeed())
		}()

		// Check that the machine was created correctly before reconciling.
		machineKey := client.ObjectKey{Namespace: ns.Name, Name: dummies.CSMachine1.Name}
		machine := &infrav1.CloudStackMachine{}
		g.Eventually(func() bool {
			err := testEnv.Get(ctx, machineKey, machine)
			return err == nil
		}, timeout).WithPolling(pollInterval).Should(BeTrue())

		result, err := reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSMachine1.Name,
			},
		})
		g.Expect(err).To(BeNil())
		g.Expect(result.RequeueAfter).To(BeZero())

		// Eventually the machine should set ready to true.
		g.Eventually(func() bool {
			tempMachine := &infrav1.CloudStackMachine{}
			if err := testEnv.Get(ctx, machineKey, tempMachine); err == nil {
				if tempMachine.Status.Ready {
					return len(tempMachine.ObjectMeta.Finalizers) > 0
				}
			}

			return false
		}, timeout).WithPolling(pollInterval).Should(BeTrue())

		g.Expect(testEnv.Delete(ctx, dummies.CSMachine1)).To(Succeed())
		g.Eventually(func() bool {
			tempMachine := &infrav1.CloudStackMachine{}
			err := testEnv.Get(ctx, machineKey, tempMachine)
			return err == nil &&
				tempMachine.DeletionTimestamp != nil
		}, timeout).WithPolling(pollInterval).Should(BeTrue())

		result, err = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSMachine1.Name,
			},
		})
		g.Expect(err).To(BeNil())
		g.Expect(result.RequeueAfter).To(Equal(10 * time.Second))

		result, err = reconciler.Reconcile(ctx, ctrl.Request{
			NamespacedName: types.NamespacedName{
				Namespace: ns.Name,
				Name:      dummies.CSMachine1.Name,
			},
		})
		g.Expect(err).To(BeNil())
		g.Expect(result.RequeueAfter).To(BeZero())

		g.Eventually(func() bool {
			tempMachine := &infrav1.CloudStackMachine{}
			if err := testEnv.Get(ctx, machineKey, tempMachine); err != nil {
				return errors.IsNotFound(err)
			}

			return false
		}, timeout).WithPolling(pollInterval).Should(BeTrue())
	})
}

// FailMessage returns message for gomega matcher if panic happened
func FailMessage(err interface{}) string {
	message := ShortPanicMessage()
	if message == "" {
		return "Expected panic to be nil"
	}

	return fmt.Sprintf(
		"Expected:\n\t\t%s\n\t%s\nto be nil",
		err, message,
	)
}

// ShortPanicMessage returns the exact line where the panic occurred
func ShortPanicMessage() string {
	group := "trace"
	re := regexp.MustCompile(`panic.go.+\n.+\n(?P<` + group + `>.+)`)

	matches := re.FindStringSubmatch(string(debug.Stack()))
	if len(matches) == 0 {
		return ""
	}

	return matches[re.SubexpIndex(group)]
}
