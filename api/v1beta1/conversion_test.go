/*
Copyright 2022 The Kubernetes Authors.

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

package v1beta1_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"

	infrav1beta1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta1"
	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

var _ = Describe("Conversion", func() {
	BeforeEach(func() { // Reset test vars to initial state.
	})

	Context("GetFailureDomains function", func() {
		It("Converts v1beta1 cluster spec to v1beta3 failure domains", func() {
			csCluster := &infrav1beta1.CloudStackCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "namespace1",
				},
				Spec: infrav1beta1.CloudStackClusterSpec{
					Zones: []infrav1beta1.Zone{
						{
							ID: "76472a84-d23f-4e97-b154-ee1b975ed936",
							Network: infrav1beta1.Network{
								Name: "network1",
							},
						},
					},
					ControlPlaneEndpoint: clusterv1beta1.APIEndpoint{
						Host: "endpoint1",
						Port: 443,
					},
					Account: "account1",
					Domain:  "domain1",
				},
				Status: infrav1beta1.CloudStackClusterStatus{},
			}
			failureDomains, err := infrav1beta1.GetFailureDomains(csCluster)
			expectedResult := []infrav1.CloudStackFailureDomainSpec{
				{
					Name: "76472a84-d23f-4e97-b154-ee1b975ed936",
					Zone: infrav1.CloudStackZoneSpec{
						ID:      "76472a84-d23f-4e97-b154-ee1b975ed936",
						Network: infrav1.Network{Name: "network1"},
					},
					Account: "account1",
					Domain:  "domain1",
					ACSEndpoint: corev1.SecretReference{
						Name:      "global",
						Namespace: "namespace1",
					},
				},
			}
			Ω(err).ShouldNot(HaveOccurred())
			Ω(failureDomains).Should(Equal(expectedResult))
		})
	})

	Context("v1beta3 to v1beta1 function", func() {
		It("Converts v1beta3 cluster spec to v1beta1 zone based cluster spec", func() {
			csCluster := &infrav1.CloudStackCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "namespace1",
				},
				Spec: infrav1.CloudStackClusterSpec{
					FailureDomains: []infrav1.CloudStackFailureDomainSpec{
						{
							Name: "76472a84-d23f-4e97-b154-ee1b975ed936",
							Zone: infrav1.CloudStackZoneSpec{
								ID:      "76472a84-d23f-4e97-b154-ee1b975ed936",
								Network: infrav1.Network{Name: "network1"},
							},
							Account: "account1",
							Domain:  "domain1",
							ACSEndpoint: corev1.SecretReference{
								Name:      "global",
								Namespace: "namespace1",
							},
						},
					},
					ControlPlaneEndpoint: clusterv1beta1.APIEndpoint{
						Host: "endpoint1",
						Port: 443,
					},
					APIServerLoadBalancer: &infrav1.APIServerLoadBalancer{
						Enabled:         ptr.To(true),
						AdditionalPorts: []int{},
						AllowedCIDRs:    []string{},
					},
				},
				Status: infrav1.CloudStackClusterStatus{},
			}
			converted := &infrav1beta1.CloudStackCluster{}
			err := infrav1beta1.Convert_v1beta3_CloudStackCluster_To_v1beta1_CloudStackCluster(csCluster, converted, nil)
			expectedResult := &infrav1beta1.CloudStackCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "namespace1",
				},
				Spec: infrav1beta1.CloudStackClusterSpec{
					Zones: []infrav1beta1.Zone{
						{
							ID: "76472a84-d23f-4e97-b154-ee1b975ed936",
							Network: infrav1beta1.Network{
								Name: "network1",
							},
						},
					},
					ControlPlaneEndpoint: clusterv1beta1.APIEndpoint{
						Host: "endpoint1",
						Port: 443,
					},
					Account: "account1",
					Domain:  "domain1",
				},
				Status: infrav1beta1.CloudStackClusterStatus{},
			}

			Ω(err).ShouldNot(HaveOccurred())
			Ω(converted).Should(Equal(expectedResult))
		})

		It("Returns error when len(failureDomains) < 1", func() {
			csCluster := &infrav1.CloudStackCluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cluster1",
					Namespace: "namespace1",
				},
				Spec: infrav1.CloudStackClusterSpec{
					ControlPlaneEndpoint: clusterv1beta1.APIEndpoint{
						Host: "endpoint1",
						Port: 443,
					},
				},
				Status: infrav1.CloudStackClusterStatus{},
			}
			err := infrav1beta1.Convert_v1beta3_CloudStackCluster_To_v1beta1_CloudStackCluster(csCluster, nil, nil)
			Ω(err).Should(HaveOccurred())
		})
	})
})
