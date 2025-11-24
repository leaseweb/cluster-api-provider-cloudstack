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

package v1beta3

import (
	"strings"
	"testing"

	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilfeature "k8s.io/component-base/featuregate/testing"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/cluster-api/feature"
	ctrl "sigs.k8s.io/controller-runtime"
)

var ctx = ctrl.SetupSignalHandler()

func TestCloudStackClusterTemplateFeatureGateEnabled(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.ClusterTopology, true)
	g := NewWithT(t)

	tests := []struct {
		name      string
		template  *CloudStackClusterTemplate
		wantError bool
	}{
		{
			name: "valid template",
			template: &CloudStackClusterTemplate{
				Spec: CloudStackClusterTemplateSpec{
					Template: CloudStackClusterTemplateResource{
						Spec: CloudStackClusterSpec{
							FailureDomains: []CloudStackFailureDomainSpec{
								{
									Name: "zone1",
									Zone: CloudStackZoneSpec{
										Name: "zone1",
										Network: Network{
											Name: "network1",
										},
									},
									ACSEndpoint: corev1.SecretReference{
										Name:      "test-secret",
										Namespace: "test-ns",
									},
								},
							},
						},
					},
				},
			},
			wantError: false,
		},
		{
			name: "missing failure domains",
			template: &CloudStackClusterTemplate{
				Spec: CloudStackClusterTemplateSpec{
					Template: CloudStackClusterTemplateResource{
						Spec: CloudStackClusterSpec{},
					},
				},
			},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(_ *testing.T) {
			webhook := &CloudStackClusterTemplateWebhook{}
			warnings, err := webhook.ValidateCreate(ctx, tt.template)
			if tt.wantError {
				g.Expect(err).To(HaveOccurred())
				g.Expect(warnings).To(BeEmpty())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(warnings).To(BeEmpty())
			}
		})
	}
}

func TestCloudStackClusterTemplateFeatureGateDisabled(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.ClusterTopology, false)
	g := NewWithT(t)

	cct := &CloudStackClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cloudstackclustertemplate-test",
			Namespace: "test-namespace",
		},
		Spec: CloudStackClusterTemplateSpec{
			Template: CloudStackClusterTemplateResource{
				Spec: CloudStackClusterSpec{},
			},
		},
	}
	webhook := &CloudStackClusterTemplateWebhook{}
	warnings, err := webhook.ValidateCreate(ctx, cct)
	g.Expect(err).To(HaveOccurred())
	g.Expect(warnings).To(BeEmpty())
}

func TestCloudStackClusterTemplateValidationMetadata(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.ClusterTopology, true)

	tests := []struct {
		name        string
		labels      map[string]string
		annotations map[string]string
		expectErr   bool
	}{
		{
			name: "should return error for invalid labels and annotations",
			labels: map[string]string{
				"foo":          "$invalid-key",
				"bar":          strings.Repeat("a", 64) + "too-long-value",
				"/invalid-key": "foo",
			},
			annotations: map[string]string{
				"/invalid-key": "foo",
			},
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			cct := &CloudStackClusterTemplate{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "cloudstackclustertemplate-test",
					Namespace: "test-namespace",
				},
				Spec: CloudStackClusterTemplateSpec{
					Template: CloudStackClusterTemplateResource{
						ObjectMeta: clusterv1beta1.ObjectMeta{
							Labels: map[string]string{
								"foo":          "$invalid-key",
								"bar":          strings.Repeat("a", 64) + "too-long-value",
								"/invalid-key": "foo",
							},
							Annotations: map[string]string{
								"/invalid-key": "foo",
							},
						},
						Spec: CloudStackClusterSpec{},
					},
				},
			}
			webhook := &CloudStackClusterTemplateWebhook{}
			warnings, err := webhook.ValidateCreate(ctx, cct)
			if tt.expectErr {
				g.Expect(err).To(HaveOccurred())
				g.Expect(warnings).To(BeEmpty())
			} else {
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(warnings).To(BeEmpty())
			}
		})
	}
}

func TestCloudStackClusterTemplateValidateUpdate(t *testing.T) {
	utilfeature.SetFeatureGateDuringTest(t, feature.Gates, feature.ClusterTopology, true)
	g := NewWithT(t)

	invalidRegex := ".*is invalid\\:.*Invalid value\\: \\S+?\\{.*\\}\\: %s"

	// Create a CloudStackClusterTemplate
	cct := &CloudStackClusterTemplate{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "cloudstackclustertemplate-test",
			Namespace: "test-namespace",
		},
	}

	modifiedCct := cct.DeepCopy()
	// Update the CloudStackClusterTemplate
	modifiedCct.Spec.Template.Spec.FailureDomains = []CloudStackFailureDomainSpec{
		{
			Name: "zone1",
			Zone: CloudStackZoneSpec{
				Name: "zone1",
			},
		},
	}

	webhook := &CloudStackClusterTemplateWebhook{}
	warnings, err := webhook.ValidateUpdate(ctx, cct, modifiedCct)
	g.Expect(err).To(MatchError(MatchRegexp(invalidRegex, "CloudStackClusterTemplate spec.template.spec field is immutable. Please create new resource instead.")))
	g.Expect(warnings).To(BeEmpty())
}
