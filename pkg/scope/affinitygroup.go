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

package scope

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// AffinityGroupScopeParams defines the input parameters used to create a new Scope.
type AffinityGroupScopeParams struct {
	Client                  client.Client
	Logger                  *logger.Logger
	Cluster                 *clusterv1.Cluster
	CloudStackAffinityGroup *infrav1.CloudStackAffinityGroup
	CSClients               CSClientsProvider
	ControllerName          string
}

// NewAffinityGroupScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewAffinityGroupScope(params AffinityGroupScopeParams) (*AffinityGroupScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.CloudStackAffinityGroup == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackAffinityGroup")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	affinityGroupScope := &AffinityGroupScope{
		Logger:                  *params.Logger,
		client:                  params.Client,
		Cluster:                 params.Cluster,
		CloudStackAffinityGroup: params.CloudStackAffinityGroup,
		controllerName:          params.ControllerName,
		CSClientsProvider:       params.CSClients,
	}

	helper, err := patch.NewHelper(params.CloudStackAffinityGroup, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	affinityGroupScope.patchHelper = helper

	return affinityGroupScope, nil
}

// AffinityGroupScope defines the basic context for an actuator to operate upon.
type AffinityGroupScope struct {
	logger.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster                 *clusterv1.Cluster
	CloudStackFailureDomain *infrav1.CloudStackFailureDomain
	CloudStackAffinityGroup *infrav1.CloudStackAffinityGroup

	CSClientsProvider
	controllerName string
}

// Name returns the affinity group name.
func (s *AffinityGroupScope) Name() string {
	return s.CloudStackAffinityGroup.Name
}

// Namespace returns the affinity group namespace.
func (s *AffinityGroupScope) Namespace() string {
	return s.CloudStackAffinityGroup.Namespace
}

// KubernetesClusterName is the name of the Kubernetes cluster.
func (s *AffinityGroupScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// Type returns the affinity group type.
func (s *AffinityGroupScope) Type() string {
	return s.CloudStackAffinityGroup.Spec.Type
}

// FailureDomainName returns the affinity group's failure domain name.
func (s *AffinityGroupScope) FailureDomainName() string {
	return s.CloudStackAffinityGroup.Spec.FailureDomainName
}

// SetReady sets the CloudStackAffinityGroup Ready Status.
func (s *AffinityGroupScope) SetReady() {
	s.CloudStackAffinityGroup.Status.Ready = true
}

// SetNotReady sets the CloudStackAffinityGroup Ready Status to false.
func (s *AffinityGroupScope) SetNotReady() {
	s.CloudStackAffinityGroup.Status.Ready = false
}

// SetID sets the CloudStackAffinityGroup ID.
func (s *AffinityGroupScope) SetID(id string) {
	s.CloudStackAffinityGroup.Spec.ID = id
}

// PatchObject persists the affinity group configuration and status.
func (s *AffinityGroupScope) PatchObject() error {
	return s.patchHelper.Patch(context.TODO(), s.CloudStackAffinityGroup)
}

// Close the AffinityGroupScope by updating the affinity group spec, affinity group status.
func (s *AffinityGroupScope) Close() error {
	return s.PatchObject()
}
