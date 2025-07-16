/*
Copyright 2025.

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
	"reflect"
	"testing"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/go-logr/logr"
	. "github.com/onsi/gomega"
	"go.uber.org/mock/gomock"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

func TestCloudStackMachineTemplateReconciler_Reconcile(t *testing.T) {
	var (
		reconciler             CloudStackMachineTemplateReconciler
		mockCtrl               *gomock.Controller
		mockClientScopeFactory *scope.MockClientScopeFactory
		mockCSCUser            *mocks.MockClient
		ctx                    context.Context
	)

	setup := func(t *testing.T) {
		t.Helper()
		mockCtrl = gomock.NewController(t)
		mockClientScopeFactory = scope.NewMockClientScopeFactory(mockCtrl, "")
		mockCSCUser = mockClientScopeFactory.MockCSClients().MockCSUser()
		reconciler = CloudStackMachineTemplateReconciler{
			Client:           testEnv.Client,
			ScopeFactory:     mockClientScopeFactory,
			WatchFilterValue: "",
		}
		ctx = context.TODO()
		ctx = logr.NewContext(ctx, ctrl.LoggerFrom(ctx))
	}

	teardown := func() {
		mockCtrl.Finish()
	}

	testCases := []struct {
		name                      string
		expectError               bool
		cloudStackMachineTemplate *infrav1.CloudStackMachineTemplate
		cloudStackServiceOffering *cloudstack.ServiceOffering
		expectedCapacity          corev1.ResourceList
	}{
		{
			name:        "Should Reconcile successfully if no CloudStackMachineTemplate found",
			expectError: false,
		},
		{
			name:                      "Should Reconcile with valid memory and processor values when offering name is provided",
			cloudStackMachineTemplate: stubCloudStackMachineTemplateWithOfferingByName(),
			cloudStackServiceOffering: stubCloudStackServiceOffering(16, 32768),
			expectedCapacity: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("32768M"),
			},
			expectError: false,
		},
		{
			name:                      "Should Reconcile with valid memory and processor values when offering id is provided",
			cloudStackMachineTemplate: stubCloudStackMachineTemplateWithOfferingByID(),
			cloudStackServiceOffering: stubCloudStackServiceOffering(16, 32768),
			expectedCapacity: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("16"),
				corev1.ResourceMemory: resource.MustParse("32768M"),
			},
			expectError: false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			setup(t)
			defer teardown()

			ns, err := testEnv.CreateNamespace(ctx, fmt.Sprintf("namespace-%s", util.RandomString(5)))
			g.Expect(err).ToNot(HaveOccurred())
			dummies.SetDummyVars(ns.Name)

			mockCSCUser.EXPECT().GetServiceOfferingByName(gomock.Any()).Return(tc.cloudStackServiceOffering, nil).AnyTimes()
			mockCSCUser.EXPECT().GetServiceOfferingByID(gomock.Any()).Return(tc.cloudStackServiceOffering, nil).AnyTimes()

			// Create test objects
			g.Expect(testEnv.Create(ctx, dummies.CAPICluster)).To(Succeed())
			// Set CAPI cluster as owner of the CloudStackCluster.
			dummies.CSCluster.OwnerReferences = append(dummies.CSCluster.OwnerReferences, metav1.OwnerReference{
				Kind:       "Cluster",
				APIVersion: clusterv1.GroupVersion.String(),
				Name:       dummies.CAPICluster.Name,
				UID:        types.UID("cluster-uid"),
			})
			dummies.CSCluster.Spec.FailureDomains = dummies.CSCluster.Spec.FailureDomains[:1]
			dummies.CSCluster.Spec.FailureDomains[0].Name = dummies.CSFailureDomain1.Spec.Name
			g.Expect(testEnv.Create(ctx, dummies.CSCluster)).To(Succeed())
			g.Expect(testEnv.Create(ctx, dummies.ACSEndpointSecret1)).To(Succeed())
			g.Expect(testEnv.Create(ctx, dummies.CSFailureDomain1)).To(Succeed())
			g.Expect(testEnv.Create(ctx, dummies.CSISONet1)).To(Succeed())

			if tc.cloudStackMachineTemplate != nil {
				tc.cloudStackMachineTemplate.Namespace = ns.Name
				g.Expect(testEnv.Create(ctx, tc.cloudStackMachineTemplate)).To(Succeed())

				g.Eventually(func() bool {
					machineTemplate := &infrav1.CloudStackMachineTemplate{}
					key := client.ObjectKey{
						Name:      tc.cloudStackMachineTemplate.Name,
						Namespace: ns.Name,
					}
					err = testEnv.Get(ctx, key, machineTemplate)
					return err == nil
				}, 10*time.Second).Should(BeTrue())

				_, err := reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Namespace: ns.Name,
						Name:      tc.cloudStackMachineTemplate.Name,
					},
				})
				if tc.expectError {
					g.Expect(err).To(HaveOccurred())
				} else {
					g.Expect(err).ToNot(HaveOccurred())
					g.Eventually(func() bool {
						machineTemplate := &infrav1.CloudStackMachineTemplate{}
						key := client.ObjectKey{
							Name:      tc.cloudStackMachineTemplate.Name,
							Namespace: ns.Name,
						}
						err = testEnv.Get(ctx, key, machineTemplate)
						g.Expect(err).ToNot(HaveOccurred())
						return reflect.DeepEqual(machineTemplate.Status.Capacity, tc.expectedCapacity)
					}, 10*time.Second).Should(BeTrue())
				}
			} else {
				_, err = reconciler.Reconcile(ctx, ctrl.Request{
					NamespacedName: client.ObjectKey{
						Namespace: "default",
						Name:      "test",
					},
				})
				g.Expect(err).ToNot(HaveOccurred())
			}
		})
	}
}

func TestGetCloudStackMachineCapacity(t *testing.T) {
	testCases := []struct {
		name                      string
		cloudStackServiceOffering *cloudstack.ServiceOffering
		expectedCapacity          corev1.ResourceList
		expectErr                 bool
	}{
		{
			name:                      "with 8G memory and 4 cpu",
			cloudStackServiceOffering: stubCloudStackServiceOffering(4, 8192),
			expectedCapacity: map[corev1.ResourceName]resource.Quantity{
				corev1.ResourceCPU:    resource.MustParse("4"),
				corev1.ResourceMemory: resource.MustParse("8192M"),
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(tt *testing.T) {
			g := NewWithT(tt)
			capacity := getCloudStackMachineCapacity(tc.cloudStackServiceOffering)
			g.Expect(capacity).To(Equal(tc.expectedCapacity))
		})
	}
}

func stubCloudStackMachineTemplateWithOfferingByName() *infrav1.CloudStackMachineTemplate {
	return stubCloudStackMachineTemplate(infrav1.CloudStackResourceIdentifier{
		Name: "test-service-offering",
	})
}

func stubCloudStackMachineTemplateWithOfferingByID() *infrav1.CloudStackMachineTemplate {
	return stubCloudStackMachineTemplate(infrav1.CloudStackResourceIdentifier{
		ID: "test-service-offering-id",
	})
}

func stubCloudStackMachineTemplate(offering infrav1.CloudStackResourceIdentifier) *infrav1.CloudStackMachineTemplate {
	return &infrav1.CloudStackMachineTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name: "cloudstack-test-1",
			OwnerReferences: []metav1.OwnerReference{
				{
					Kind:       "Cluster",
					APIVersion: clusterv1.GroupVersion.String(),
					Name:       dummies.CAPICluster.Name,
					UID:        types.UID("cluster-uid"),
				},
			},
		},
		Spec: infrav1.CloudStackMachineTemplateSpec{
			Template: infrav1.CloudStackMachineTemplateResource{
				Spec: infrav1.CloudStackMachineSpec{
					Name:     "test-machine",
					Offering: offering,
					Template: infrav1.CloudStackResourceIdentifier{
						Name: "test-template",
					},
				},
			},
		},
	}
}

func stubCloudStackServiceOffering(cpu, memory int) *cloudstack.ServiceOffering {
	return &cloudstack.ServiceOffering{
		Name:      "test-service-offering",
		Id:        "test-service-offering-id",
		Cpunumber: cpu,
		Memory:    memory,
	}
}
