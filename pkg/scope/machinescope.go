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

// Package scope implement the scope for the CloudStack Cluster when doing the reconciliation process.
package scope

import (
	"context"
	"fmt"
	"strings"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/klog/v2"
	"k8s.io/utils/ptr"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// MachineScopeParams defines the input parameters used to create a new Scope.
type MachineScopeParams struct {
	Client                    client.Client
	Logger                    *logger.Logger
	Cluster                   *clusterv1.Cluster
	CloudStackCluster         *infrav1.CloudStackCluster
	CloudStackFailureDomain   *infrav1.CloudStackFailureDomain
	CloudStackIsolatedNetwork *infrav1.CloudStackIsolatedNetwork
	Machine                   *clusterv1.Machine
	CloudStackMachine         *infrav1.CloudStackMachine
	CSClients                 CSClientsProvider
	ControllerName            string
}

// NewMachineScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewMachineScope(params MachineScopeParams) (*MachineScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.Machine == nil {
		return nil, errors.New("failed to generate new scope from nil Machine")
	}
	if params.CloudStackMachine == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackMachine")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	machineScope := &MachineScope{
		Logger:                  *params.Logger,
		client:                  params.Client,
		Cluster:                 params.Cluster,
		CloudStackCluster:       params.CloudStackCluster,
		CloudStackFailureDomain: params.CloudStackFailureDomain,
		CloudStackMachine:       params.CloudStackMachine,
		Machine:                 params.Machine,
		controllerName:          params.ControllerName,
		CSClientsProvider:       params.CSClients,
	}

	helper, err := patch.NewHelper(params.CloudStackMachine, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	machineScope.patchHelper = helper

	return machineScope, nil
}

// MachineScope defines the basic context for an actuator to operate upon.
type MachineScope struct {
	logger.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster                   *clusterv1.Cluster
	CloudStackCluster         *infrav1.CloudStackCluster
	CloudStackFailureDomain   *infrav1.CloudStackFailureDomain
	CloudStackIsolatedNetwork *infrav1.CloudStackIsolatedNetwork
	CloudStackAffinityGroup   *infrav1.CloudStackAffinityGroup
	Machine                   *clusterv1.Machine
	CloudStackMachine         *infrav1.CloudStackMachine

	CSClientsProvider
	controllerName string
}

// Name returns the machine name.
func (s *MachineScope) Name() string {
	return s.CloudStackMachine.Name
}

// Namespace returns the machine namespace.
func (s *MachineScope) Namespace() string {
	return s.CloudStackMachine.Namespace
}

// KubernetesClusterName is the name of the Kubernetes cluster.
func (s *MachineScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// FailureDomainName returns the machine's failure domain name.
// If the machine has a failure domain, it will be used.
// Otherwise, the CloudStackMachine's failure domain name will be used.
func (s *MachineScope) FailureDomainName() string {
	if s.Machine.Spec.FailureDomain != nil {
		return *s.Machine.Spec.FailureDomain
	}

	return s.CloudStackMachine.Spec.FailureDomainName
}

// SetReady sets the CloudStackMachine Ready Status.
func (s *MachineScope) SetReady() {
	s.CloudStackMachine.Status.Ready = true
}

// SetNotReady sets the CloudStackMachine Ready Status to false.
func (s *MachineScope) SetNotReady() {
	s.CloudStackMachine.Status.Ready = false
}

func (s *MachineScope) IsReady() bool {
	return s.CloudStackMachine.Status.Ready
}

// PatchObject persists the machine configuration and status.
func (s *MachineScope) PatchObject() error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (instead we are hiding during the deletion process).
	// At a later stage, we will add more conditions indicating the readiness of other resources security groups etc.
	applicableConditions := []clusterv1.ConditionType{
		infrav1.InstanceReadyCondition,
	}

	if s.IsControlPlane() {
		applicableConditions = append(applicableConditions, infrav1.LoadBalancerAttachedCondition)
	}

	conditions.SetSummary(s.CloudStackMachine,
		conditions.WithConditions(applicableConditions...),
		conditions.WithStepCounterIf(s.CloudStackMachine.ObjectMeta.DeletionTimestamp.IsZero()),
	)

	return s.patchHelper.Patch(
		context.TODO(),
		s.CloudStackMachine,
		patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
			clusterv1.ReadyCondition,
			infrav1.InstanceReadyCondition,
			infrav1.LoadBalancerAttachedCondition,
		}},
	)
}

// Close the MachineScope by updating the machine spec, machine status.
func (s *MachineScope) Close() error {
	return s.PatchObject()
}

func (s *MachineScope) OwnerGVK() schema.GroupVersionKind {
	return s.CloudStackMachine.GetObjectKind().GroupVersionKind()
}

// NetworkName returns the name of the network.
func (s *MachineScope) NetworkName() string {
	return s.CloudStackFailureDomain.Spec.Zone.Network.Name
}

// NetworkType returns the type of the network.
func (s *MachineScope) NetworkType() string {
	return s.CloudStackFailureDomain.Spec.Zone.Network.Type
}

// IsolatedNetwork returns the isolated network of the machine.
func (s *MachineScope) IsolatedNetwork(ctx context.Context) (*infrav1.CloudStackIsolatedNetwork, error) {
	isonet := &infrav1.CloudStackIsolatedNetwork{}
	if s.CloudStackIsolatedNetwork == nil || s.CloudStackIsolatedNetwork.Name == "" {
		err := s.client.Get(ctx, client.ObjectKey{Name: s.IsolatedNetworkName(), Namespace: s.Namespace()}, isonet)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get isolated network with name %s", s.IsolatedNetworkName())
		}
	}

	return isonet, nil
}

// SetIsolatedNetwork sets the isolated network of the machine.
func (s *MachineScope) SetIsolatedNetwork(isonet *infrav1.CloudStackIsolatedNetwork) {
	s.CloudStackIsolatedNetwork = isonet
}

// IsolatedNetworkName returns the sanitized (CloudStack-compliant) name of the isolated network.
func (s *MachineScope) IsolatedNetworkName() string {
	str := metaNameRegex.ReplaceAllString(fmt.Sprintf("%s-%s", s.KubernetesClusterName(), strings.ToLower(s.NetworkName())), "-")

	return strings.TrimSuffix(str, "-")
}

// AffinityEnabled returns true if affinity is enabled.
func (s *MachineScope) AffinityEnabled() bool {
	return s.CloudStackMachine.Spec.Affinity != "" && s.CloudStackMachine.Spec.Affinity != infrav1.AffinityTypeNo
}

// AffinityType returns the type of affinity.
func (s *MachineScope) AffinityType() string {
	return s.CloudStackMachine.Spec.Affinity
}

// AffinityGroupRef returns the reference to the affinity group.
func (s *MachineScope) AffinityGroupRef() *corev1.ObjectReference {
	return s.CloudStackMachine.Spec.AffinityGroupRef
}

// AffinityGroupName returns the name of the affinity group.
func (s *MachineScope) AffinityGroupName() string {
	return s.CloudStackMachine.Spec.AffinityGroupRef.Name
}

func (s *MachineScope) SetAffinityGroupRef(ref *corev1.ObjectReference) {
	s.CloudStackMachine.Spec.AffinityGroupRef = ref
}

// GetProviderID returns the provider ID of the CloudStackMachine.
func (s *MachineScope) GetProviderID() string {
	if s.CloudStackMachine.Spec.ProviderID != nil {
		return *s.CloudStackMachine.Spec.ProviderID
	}

	return ""
}

// SetProviderID sets the provider ID of the CloudStackMachine based on the instance ID.
func (s *MachineScope) SetProviderID(id string) {
	pid := ptr.To("cloudstack:///" + id)
	s.CloudStackMachine.Spec.ProviderID = pid
}

// GetInstanceID returns the instance ID of the CloudStackMachine.
func (s *MachineScope) GetInstanceID() string {
	if s.CloudStackMachine.Spec.InstanceID != nil {
		return *s.CloudStackMachine.Spec.InstanceID
	}

	return ""
}

// SetInstanceID sets the instance ID of the CloudStackMachine.
func (s *MachineScope) SetInstanceID(id string) {
	instanceID := ptr.To(id)
	s.CloudStackMachine.Spec.InstanceID = instanceID
}

// GetBootstrapData returns the bootstrap data from the secret in the Machine's bootstrap.dataSecretName.
func (s *MachineScope) GetBootstrapData() ([]byte, error) {
	if s.Machine.Spec.Bootstrap.DataSecretName == nil {
		return nil, errors.New("error retrieving bootstrap data: linked Machine's bootstrap.dataSecretName is nil")
	}

	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: s.Machine.Namespace, Name: *s.Machine.Spec.Bootstrap.DataSecretName}
	if err := s.client.Get(context.TODO(), key, secret); err != nil {
		return nil, errors.Wrapf(err, "failed to retrieve bootstrap data secret for CloudStackMachine %s/%s", s.Namespace(), s.Name())
	}

	value, ok := secret.Data["value"]
	if !ok {
		return nil, errors.New("error retrieving bootstrap data: secret value key is missing")
	}

	return value, nil
}

func (s *MachineScope) SetInstanceState(state string) {
	s.CloudStackMachine.Status.InstanceState = state
}

func (s *MachineScope) GetInstanceState() string {
	return s.CloudStackMachine.Status.InstanceState
}

func (s *MachineScope) SetAddresses(addresses []corev1.NodeAddress) {
	s.CloudStackMachine.Status.Addresses = addresses
}

func (s *MachineScope) SetFailureReason(reason capierrors.MachineStatusError) {
	s.CloudStackMachine.Status.FailureReason = &reason
}

func (s *MachineScope) SetFailureMessage(message error) {
	s.CloudStackMachine.Status.FailureMessage = ptr.To[string](message.Error())
}

func (s *MachineScope) HasFailed() bool {
	return s.CloudStackMachine.Status.FailureReason != nil || s.CloudStackMachine.Status.FailureMessage != nil
}

// InstanceIsRunning returns the instance state of the machine scope.
func (s *MachineScope) InstanceIsRunning() bool {
	state := s.GetInstanceState()
	return state != "" && cloud.InstanceRunningStates.Has(state)
}

// InstanceIsOperational returns the operational state of the machine scope.
func (s *MachineScope) InstanceIsOperational() bool {
	state := s.GetInstanceState()
	return state != "" && cloud.InstanceOperationalStates.Has(state)
}

// IsControlPlane returns true if the machine is a control plane.
func (s *MachineScope) IsControlPlane() bool {
	return util.IsControlPlaneMachine(s.Machine)
}

func (s *MachineScope) IsLBEnabled() bool {
	return s.CloudStackCluster.Spec.APIServerLoadBalancer.IsEnabled()
}

// CloudStackMachineIsDeleted returns true if the CloudStackMachine is deleted.
func (s *MachineScope) CloudStackMachineIsDeleted() bool {
	return !s.CloudStackMachine.ObjectMeta.DeletionTimestamp.IsZero()
}

// MachineIsDeleted returns true if the CAPI Machine is deleted.
func (s *MachineScope) MachineIsDeleted() bool {
	return !s.Machine.ObjectMeta.DeletionTimestamp.IsZero()
}

// IsExternallyManaged checks if the machine is externally managed.
func (s *MachineScope) IsExternallyManaged() bool {
	return annotations.IsExternallyManaged(s.CloudStackCluster)
}
