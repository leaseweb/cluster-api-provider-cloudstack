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
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// AffinityGroupScopeParams defines the input parameters used to create a new Scope.
type FailureDomainScopeParams struct {
	Client                  client.Client
	Logger                  *logger.Logger
	Cluster                 *clusterv1.Cluster
	CloudStackFailureDomain *infrav1.CloudStackFailureDomain
	CSClientFactory         cloud.Factory
	ControllerName          string
}

// NewFailureDomainScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewFailureDomainScope(params FailureDomainScopeParams) (*FailureDomainScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.CloudStackFailureDomain == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackFailureDomain")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	failureDomainScope := &FailureDomainScope{
		Logger:                  *params.Logger,
		client:                  params.Client,
		Cluster:                 params.Cluster,
		CloudStackFailureDomain: params.CloudStackFailureDomain,
		controllerName:          params.ControllerName,
		csClientFactory:         params.CSClientFactory,
	}

	clients, err := getClientsForFailureDomain(params.Client, failureDomainScope)
	if err != nil {
		return nil, errors.Errorf("failed to create CloudStack Clients: %v", err)
	}
	failureDomainScope.CSClients = clients

	helper, err := patch.NewHelper(params.CloudStackFailureDomain, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	failureDomainScope.patchHelper = helper

	return failureDomainScope, nil
}

// FailureDomainScope defines the basic context for an actuator to operate upon.
type FailureDomainScope struct {
	logger.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster                 *clusterv1.Cluster
	CloudStackFailureDomain *infrav1.CloudStackFailureDomain

	CSClients
	controllerName  string
	csClientFactory cloud.Factory
}

// Name returns the affinity group name.
func (s *FailureDomainScope) Name() string {
	return s.CloudStackFailureDomain.Name
}

// Namespace returns the affinity group namespace.
func (s *FailureDomainScope) Namespace() string {
	return s.CloudStackFailureDomain.Namespace
}

// KubernetesClusterName is the name of the Kubernetes cluster.
func (s *FailureDomainScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// SetReady sets the CloudStackFailureDomain Ready Status.
func (s *FailureDomainScope) SetReady() {
	s.CloudStackFailureDomain.Status.Ready = true
}

// SetNotReady sets the CloudStackAffinityGroup Ready Status to false.
func (s *FailureDomainScope) SetNotReady() {
	s.CloudStackFailureDomain.Status.Ready = false
}

// PatchObject persists the failure domain configuration and status.
func (s *FailureDomainScope) PatchObject() error {
	return s.patchHelper.Patch(context.TODO(), s.CloudStackFailureDomain)
}

// Close the FailureDomainScope by updating the failure domain spec, failure domain status.
func (s *FailureDomainScope) Close() error {
	return s.PatchObject()
}

func (s *FailureDomainScope) ResolveZone() error {
	return s.CSUser.ResolveZone(&s.CloudStackFailureDomain.Spec.Zone)
}

func (s *FailureDomainScope) ResolveNetwork() error {
	return s.CSUser.ResolveNetworkForZone(&s.CloudStackFailureDomain.Spec.Zone)
}

func (s *FailureDomainScope) Network() infrav1.Network {
	return s.CloudStackFailureDomain.Spec.Zone.Network
}

func (s *FailureDomainScope) NetworkID() string {
	return s.CloudStackFailureDomain.Spec.Zone.Network.ID
}

func (s *FailureDomainScope) NetworkName() string {
	return s.CloudStackFailureDomain.Spec.Zone.Network.Name
}

func (s *FailureDomainScope) NetworkType() string {
	return s.CloudStackFailureDomain.Spec.Zone.Network.Type
}

func (s *FailureDomainScope) OwnerGVK() schema.GroupVersionKind {
	return s.CloudStackFailureDomain.GetObjectKind().GroupVersionKind()
}

// ClientFactory returns the CloudStack Client Factory.
func (s *FailureDomainScope) ClientFactory() cloud.Factory {
	return s.csClientFactory
}

// FailureDomain returns the failure domain.
func (s *FailureDomainScope) FailureDomain(ctx context.Context) (*infrav1.CloudStackFailureDomain, error) {
	return s.CloudStackFailureDomain, nil
}
