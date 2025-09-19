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
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
)

const (
	// FailureDomainsReadyCondition reports on the successful reconciliation of CloudStack failure domains.
	FailureDomainsReadyCondition clusterv1.ConditionType = "FailureDomainsReady"

	// FailureDomainsNotReadyReason used when the failure domains are not ready.
	FailureDomainsNotReadyReason = "FailureDomainsNotReady"
	// FailureDomainsErrorReason used when there is an error creating or getting the failure domains.
	FailureDomainsErrorReason = "FailureDomainsError"
	// FailureDomainsDeletionFailedReason used when the deletion of a failure domain fails.
	FailureDomainsDeletionFailedReason = "FailureDomainsDeletionFailed"
)

const (
	// InstanceReadyCondition reports on current status of the CloudStack instance. Ready indicates the instance is in a Running state.
	InstanceReadyCondition clusterv1.ConditionType = "InstanceReady"

	// InstanceNotFoundReason used when the instance couldn't be retrieved.
	InstanceNotFoundReason = "InstanceNotFound"
	// InstanceTerminatedReason instance is in a terminated state.
	InstanceTerminatedReason = "InstanceTerminated"
	// InstanceStoppedReason instance is in a stopped state.
	InstanceStoppedReason = "InstanceStopped"
	// InstanceNotReadyReason used when the instance is in a pending state.
	InstanceNotReadyReason = "InstanceNotReady"
	// InstanceDeletingReason used when the instance is in a deleting state.
	InstanceDeletingReason = "InstanceDeleting"
	// InstanceProvisionStartedReason set when the provisioning of an instance started.
	InstanceProvisionStartedReason = "InstanceProvisionStarted"
	// InstanceProvisionFailedReason used for failures during instance provisioning.
	InstanceProvisionFailedReason = "InstanceProvisionFailed"
	// WaitingForClusterInfrastructureReason used when machine is waiting for cluster infrastructure to be ready before proceeding.
	WaitingForClusterInfrastructureReason = "WaitingForClusterInfrastructure"
	// WaitingForBootstrapDataReason used when machine is waiting for bootstrap data to be ready before proceeding.
	WaitingForBootstrapDataReason = "WaitingForBootstrapData"
	// WaitingForAffinityGroupReason used when machine is waiting for affinity group to be ready before proceeding.
	WaitingForAffinityGroupReason = "WaitingForAffinityGroup"
	// AffinityGroupErrorReason used when there is an error creating or getting the affinity group.
	AffinityGroupErrorReason = "AffinityGroupError"
	// BootstrapDataErrorReason used when there is an error getting the bootstrap data.
	BootstrapDataErrorReason = "BootstrapDataError"
)

const (
	// LoadBalancerAttachedCondition reports on the successful attachment of a load balancer to an instance.
	LoadBalancerAttachedCondition clusterv1.ConditionType = "LoadBalancerAttached"

	// LoadBalancerAttachFailedReason used when the attachment of a load balancer to an instance fails.
	LoadBalancerAttachFailedReason = "LoadBalancerAttachFailed"
	// LoadBalancerDetachFailedReason used when the detachment of a load balancer from an instance fails.
	LoadBalancerDetachFailedReason = "LoadBalancerDetachFailed"
)
