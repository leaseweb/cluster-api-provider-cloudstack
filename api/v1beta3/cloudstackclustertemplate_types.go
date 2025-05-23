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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// CloudStackClusterTemplateSpec defines the desired state of CloudStackClusterTemplate.
type CloudStackClusterTemplateSpec struct {
	Template CloudStackClusterTemplateResource `json:"template"`
}

// CloudStackClusterTemplateResource describes the data needed to create a CloudStackCluster from a template.
type CloudStackClusterTemplateResource struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	// +optional
	ObjectMeta clusterv1.ObjectMeta  `json:"metadata,omitempty"`
	Spec       CloudStackClusterSpec `json:"spec"`
}

//+kubebuilder:resource:path=cloudstackclustertemplates,scope=Namespaced,categories=cluster-api,shortName=csct
//+kubebuilder:object:root=true
//+kubebuilder:storageversion
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of CloudStackClusterTemplate"

// CloudStackClusterTemplate is the Schema for the cloudstackclustertemplate API.
type CloudStackClusterTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CloudStackClusterTemplateSpec `json:"spec,omitempty"`
}

//+kubebuilder:object:root=true

// CloudStackClusterTemplateList contains a list of CloudStackClusterTemplate.
type CloudStackClusterTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudStackClusterTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudStackClusterTemplate{}, &CloudStackClusterTemplateList{})
}
