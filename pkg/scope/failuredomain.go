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
	"regexp"
	"strings"

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// FailureDomainScopeParams defines the input parameters used to create a new Scope.
type FailureDomainScopeParams struct {
	Client                  client.Client
	Logger                  *logger.Logger
	Cluster                 *clusterv1.Cluster
	CloudStackFailureDomain *infrav1.CloudStackFailureDomain
	CSClients               CSClientsProvider
	ControllerName          string
}

var metaNameRegex = regexp.MustCompile(`[^a-z0-9-]+`)

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
		CSClientsProvider:       params.CSClients,
	}

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

	CSClientsProvider
	controllerName string
}

// Name returns the failure domain name.
func (s *FailureDomainScope) Name() string {
	return s.CloudStackFailureDomain.Name
}

// Namespace returns the failure domain namespace.
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

// SetNotReady sets the CloudStackFailureDomain Ready Status to false.
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
	return s.CSUser().ResolveZone(&s.CloudStackFailureDomain.Spec.Zone)
}

func (s *FailureDomainScope) ResolveNetwork() error {
	return s.CSUser().ResolveNetworkForZone(&s.CloudStackFailureDomain.Spec.Zone)
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

// FailureDomain returns the failure domain.
func (s *FailureDomainScope) FailureDomain(_ context.Context) (*infrav1.CloudStackFailureDomain, error) {
	return s.CloudStackFailureDomain, nil
}

// IsolatedNetworkName returns the sanitized (CloudStack-compliant) name of the isolated network.
func (s *FailureDomainScope) IsolatedNetworkName() string {
	str := metaNameRegex.ReplaceAllString(fmt.Sprintf("%s-%s", s.KubernetesClusterName(), strings.ToLower(s.NetworkName())), "-")

	return strings.TrimSuffix(str, "-")
}
