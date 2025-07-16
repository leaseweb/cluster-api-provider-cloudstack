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

package v1beta3

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// CloudStackMachineTemplateResource defines the data needed to create a CloudstackMachine from a template.
type CloudStackMachineTemplateResource struct {
	// Standard object's metadata.
	// More info: https://git.k8s.io/community/contributors/devel/sig-architecture/api-conventions.md#metadata
	//+optional
	ObjectMeta clusterv1.ObjectMeta `json:"metadata,omitempty"`

	// Spec is the specification of a desired behavior of the machine
	Spec CloudStackMachineSpec `json:"spec"`
}

// CloudStackMachineTemplateSpec defines the desired state of CloudstackMachineTemplate.
type CloudStackMachineTemplateSpec struct {
	Template CloudStackMachineTemplateResource `json:"template"`
}

// CloudStackMachineTemplateStatus defines the observed state of CloudStackMachineTemplate.
type CloudStackMachineTemplateStatus struct {
	// Capacity defines the resource capacity for this machine.
	// This value is used for autoscaling from zero operations as defined in:
	// https://github.com/kubernetes-sigs/cluster-api/blob/main/docs/proposals/20210310-opt-in-autoscaling-from-zero.md
	// +optional
	Capacity corev1.ResourceList `json:"capacity,omitempty"`
}

//+kubebuilder:subresource:status
//+kubebuilder:object:root=true
//+kubebuilder:resource:path=cloudstackmachinetemplates,scope=Namespaced,categories=cluster-api,shortName=csmt
//+kubebuilder:storageversion
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of CloudStackMachineTemplate"

// CloudStackMachineTemplate is the Schema for the cloudstackmachinetemplates API.
type CloudStackMachineTemplate struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudStackMachineTemplateSpec   `json:"spec,omitempty"`
	Status CloudStackMachineTemplateStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CloudStackMachineTemplateList contains a list of CloudStackMachineTemplate.
type CloudStackMachineTemplateList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudStackMachineTemplate `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudStackMachineTemplate{}, &CloudStackMachineTemplateList{})
}
