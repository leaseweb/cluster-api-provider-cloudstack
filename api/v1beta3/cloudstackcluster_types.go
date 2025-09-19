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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	ClusterFinalizer = "cloudstackcluster.infrastructure.cluster.x-k8s.io"
)

var K8sClient client.Client

// CloudStackClusterSpec defines the desired state of CloudStackCluster.
type CloudStackClusterSpec struct {
	// FailureDomains is a list of failure domains for the cluster.
	//+listType=map
	//+listMapKey=name
	//+listMapKeyType=string
	FailureDomains []CloudStackFailureDomainSpec `json:"failureDomains"`

	// The kubernetes control plane endpoint.
	ControlPlaneEndpoint clusterv1.APIEndpoint `json:"controlPlaneEndpoint"`

	// APIServerLoadBalancer configures the optional LoadBalancer for the APIServer.
	// If not specified, no load balancer will be created for the API server.
	//+optional
	APIServerLoadBalancer *APIServerLoadBalancer `json:"apiServerLoadBalancer,omitempty"`
}

// The status of the CloudStackCluster object.
type CloudStackClusterStatus struct {
	// CAPI recognizes failure domains as a method to spread machines.
	// CAPC sets failure domains to indicate functioning CloudStackFailureDomains.
	//+optional
	FailureDomains clusterv1.FailureDomains `json:"failureDomains,omitempty"`

	// Reflects the readiness of the CS cluster.
	//+optional
	Ready bool `json:"ready"`

	// Conditions defines current service state of the CloudStackCluster.
	//+optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

// GetConditions returns the conditions for the CloudStackCluster.
func (r *CloudStackCluster) GetConditions() clusterv1.Conditions {
	return r.Status.Conditions
}

// SetConditions sets the conditions for the CloudStackCluster.
func (r *CloudStackCluster) SetConditions(conditions clusterv1.Conditions) {
	r.Status.Conditions = conditions
}

//+kubebuilder:resource:path=cloudstackclusters,scope=Namespaced,categories=cluster-api,shortName=cscluster
//+kubebuilder:subresource:status
//+kubebuilder:storageversion
//+kubebuilder:object:root=true
//+kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels['cluster\\.x-k8s\\.io/cluster-name']",description="Cluster"
//+kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of CloudStackCluster"

// CloudStackCluster is the Schema for the cloudstackclusters API.
type CloudStackCluster struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec CloudStackClusterSpec `json:"spec,omitempty"`

	// The actual cluster state reported by CloudStack.
	Status CloudStackClusterStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CloudStackClusterList contains a list of CloudStackCluster.
type CloudStackClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudStackCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudStackCluster{}, &CloudStackClusterList{})
}
