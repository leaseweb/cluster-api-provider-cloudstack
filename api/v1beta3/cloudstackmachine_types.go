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
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
)

// The presence of a finalizer prevents CAPI from deleting the corresponding CAPI data.
const MachineFinalizer = "cloudstackmachine.infrastructure.cluster.x-k8s.io"

const (
	AffinityTypePro      = "pro"
	AffinityTypeAnti     = "anti"
	AffinityTypeSoftPro  = "soft-pro"
	AffinityTypeSoftAnti = "soft-anti"
	AffinityTypeNo       = "no"
)

// CloudStackMachineSpec defines the desired state of CloudStackMachine.
type CloudStackMachineSpec struct {
	// Name.
	//+optional
	Name string `json:"name,omitempty"`

	// ID.
	//+optional
	ID string `json:"id,omitempty"`

	// Instance ID. Should only be useful to modify an existing instance.
	InstanceID *string `json:"instanceID,omitempty"`

	// CloudStack compute offering.
	Offering CloudStackResourceIdentifier `json:"offering"`

	// CloudStack template to use.
	Template CloudStackResourceIdentifier `json:"template"`

	// CloudStack disk offering to use.
	//+optional
	DiskOffering *CloudStackResourceDiskOffering `json:"diskOffering,omitempty"`

	// CloudStack ssh key to use.
	//+optional
	SSHKey string `json:"sshKey"`

	// Optional details map for deployVirtualMachine
	Details map[string]string `json:"details,omitempty"`

	// Optional affinitygroupids for deployVirtualMachine
	//+listType=set
	//+optional
	AffinityGroupIDs []string `json:"affinityGroupIDs,omitempty"`

	// Mutually exclusive parameter with AffinityGroupIDs.
	// Defaults to `no`. Can be `pro` or `anti`. Will create an affinity group per machine set.
	//+optional
	Affinity string `json:"affinity,omitempty"`

	// Mutually exclusive parameter with AffinityGroupIDs.
	// Is a reference to a CloudStack affinity group CRD.
	//+optional
	AffinityGroupRef *corev1.ObjectReference `json:"cloudstackAffinityRef,omitempty"`

	// The CS specific unique identifier. Of the form: fmt.Sprintf("cloudstack:///%s", CS Machine ID)
	//+optional
	ProviderID *string `json:"providerID,omitempty"`

	// FailureDomainName -- the name of the FailureDomain the machine is placed in.
	//+optional
	FailureDomainName string `json:"failureDomainName,omitempty"`

	// UncompressedUserData specifies whether the user data is gzip-compressed.
	// cloud-init has built-in support for gzip-compressed user data, ignition does not.
	//
	//+optional
	UncompressedUserData *bool `json:"uncompressedUserData,omitempty"`
}

func (r *CloudStackMachine) CompressUserdata() bool {
	return r.Spec.UncompressedUserData == nil || !*r.Spec.UncompressedUserData
}

type CloudStackResourceDiskOffering struct {
	CloudStackResourceIdentifier `json:",inline"`
	// Desired disk size. Used if disk offering is customizable as indicated by the ACS field 'Custom Disk Size'.
	//+optional
	CustomSize int64 `json:"customSizeInGB"`
	// mount point the data disk uses to mount. The actual partition, mkfs and mount are done by cloud-init generated by kubeadmConfig.
	MountPath string `json:"mountPath"`
	// device name of data disk, for example /dev/vdb.
	Device string `json:"device"`
	// filesystem used by data disk, for example, ext4, xfs.
	Filesystem string `json:"filesystem"`
	// label of data disk, used by mkfs as label parameter.
	Label string `json:"label"`
}

// Type pulled mostly from the CloudStack API.
type CloudStackMachineStatus struct {
	// Ready indicates the readiness of the provider resource.
	//+optional
	Ready bool `json:"ready"`

	// Addresses contains a CloudStack VM instance's IP addresses.
	Addresses []corev1.NodeAddress `json:"addresses,omitempty"`

	// InstanceState is the state of the CloudStack instance for this machine.
	//+optional
	InstanceState string `json:"instanceState,omitempty"`

	// InstanceStateLastUpdated is the time the instance state was last updated.
	//+optional
	InstanceStateLastUpdated metav1.Time `json:"instanceStateLastUpdated,omitempty"`

	// Status indicates the status of the provider resource.
	//+optional
	// Deprecated: This field has no function and is going to be removed in the next release.
	Status *string `json:"status,omitempty"`

	// Reason indicates the reason of status failure.
	//+optional
	// Deprecated: This field has no function and is going to be removed in the next release.
	Reason *string `json:"reason,omitempty"`

	// FailureReason will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a succinct value suitable
	// for machine interpretation.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureReason *string `json:"failureReason,omitempty"`

	// FailureMessage will be set in the event that there is a terminal problem
	// reconciling the Machine and will contain a more verbose string suitable
	// for logging and human consumption.
	//
	// This field should not be set for transitive errors that a controller
	// faces that are expected to be fixed automatically over
	// time (like service outages), but instead indicate that something is
	// fundamentally wrong with the Machine's spec or the configuration of
	// the controller, and that manual intervention is required. Examples
	// of terminal errors would be invalid combinations of settings in the
	// spec, values that are unsupported by the controller, or the
	// responsible controller itself being critically misconfigured.
	//
	// Any transient errors that occur during the reconciliation of Machines
	// can be added as events to the Machine object and/or logged in the
	// controller's output.
	// +optional
	FailureMessage *string `json:"failureMessage,omitempty"`

	// Conditions defines current service state of the CloudStackMachine.
	//+optional
	Conditions clusterv1.Conditions `json:"conditions,omitempty"`
}

// GetConditions returns the conditions for the CloudStackMachine.
func (r *CloudStackMachine) GetConditions() clusterv1.Conditions {
	return r.Status.Conditions
}

// SetConditions sets the conditions for the CloudStackMachine.
func (r *CloudStackMachine) SetConditions(conditions clusterv1.Conditions) {
	r.Status.Conditions = conditions
}

// TimeSinceLastStateChange returns the amount of time that's elapsed since the state was last updated.  If the state
// hasn't ever been updated, it returns a negative value.
func (s *CloudStackMachineStatus) TimeSinceLastStateChange() time.Duration {
	if s.InstanceStateLastUpdated.IsZero() {
		return time.Duration(-1)
	}

	return time.Since(s.InstanceStateLastUpdated.Time)
}

// +kubebuilder:object:root=true
// +kubebuilder:resource:path=cloudstackmachines,scope=Namespaced,categories=cluster-api,shortName=csm
// +kubebuilder:storageversion
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Cluster",type="string",JSONPath=".metadata.labels.cluster\\.x-k8s\\.io/cluster-name",description="Cluster to which this CloudStackMachine belongs"
// +kubebuilder:printcolumn:name="Machine",type="string",JSONPath=".metadata.ownerReferences[?(@.kind==\"Machine\")].name",description="Machine object which owns with this CloudStackMachine"
// +kubebuilder:printcolumn:name="ProviderID",type="string",JSONPath=".spec.providerID",description="CloudStack instance ID"
// +kubebuilder:printcolumn:name="InstanceState",type="string",JSONPath=".status.instanceState",description="CloudStack instance state"
// +kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.ready",description="Machine ready status"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp",description="Time duration since creation of CloudStackMachine"

// CloudStackMachine is the Schema for the cloudstackmachines API.
type CloudStackMachine struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   CloudStackMachineSpec   `json:"spec,omitempty"`
	Status CloudStackMachineStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// CloudStackMachineList contains a list of CloudStackMachine.
type CloudStackMachineList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []CloudStackMachine `json:"items"`
}

func init() {
	SchemeBuilder.Register(&CloudStackMachine{}, &CloudStackMachineList{})
}
