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

	"github.com/pkg/errors"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util/conditions"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// ClusterScopeParams defines the input parameters used to create a new Scope.
type ClusterScopeParams struct {
	Client            client.Client
	Logger            *logger.Logger
	Cluster           *clusterv1.Cluster
	CloudStackCluster *infrav1.CloudStackCluster
	ControllerName    string
}

// NewClusterScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewClusterScope(params ClusterScopeParams) (*ClusterScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.CloudStackCluster == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackCluster")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	clusterScope := &ClusterScope{
		Logger:            *params.Logger,
		client:            params.Client,
		Cluster:           params.Cluster,
		CloudStackCluster: params.CloudStackCluster,
		controllerName:    params.ControllerName,
	}

	helper, err := patch.NewHelper(params.CloudStackCluster, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	clusterScope.patchHelper = helper

	return clusterScope, nil
}

// ClusterScope defines the basic context for an actuator to operate upon.
type ClusterScope struct {
	logger.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster           *clusterv1.Cluster
	CloudStackCluster *infrav1.CloudStackCluster

	controllerName string
}

// Name returns the cluster name.
func (s *ClusterScope) Name() string {
	return s.CloudStackCluster.Name
}

// Namespace returns the cluster namespace.
func (s *ClusterScope) Namespace() string {
	return s.CloudStackCluster.Namespace
}

// KubernetesClusterName is the name of the Kubernetes cluster.
func (s *ClusterScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// SetReady sets the CloudStackCluster Ready Status.
func (s *ClusterScope) SetReady() {
	s.CloudStackCluster.Status.Ready = true
}

// SetNotReady sets the CloudStackCluster Ready Status to false.
func (s *ClusterScope) SetNotReady() {
	s.CloudStackCluster.Status.Ready = false
}

// PatchObject persists the cluster configuration and status.
func (s *ClusterScope) PatchObject() error {
	// Always update the readyCondition by summarizing the state of other conditions.
	// A step counter is added to represent progress during the provisioning process (disabled during deletion).
	// At a later stage, we will add more conditions indicating the readiness of other resources like networks, loadbalancers, etc.
	applicableConditions := []clusterv1.ConditionType{
		infrav1.FailureDomainsReadyCondition,
	}

	conditions.SetSummary(s.CloudStackCluster,
		conditions.WithConditions(applicableConditions...),
		conditions.WithStepCounterIf(s.CloudStackCluster.DeletionTimestamp.IsZero()),
	)

	return s.patchHelper.Patch(
		context.TODO(),
		s.CloudStackCluster,
		patch.WithOwnedConditions{Conditions: []clusterv1.ConditionType{
			clusterv1.ReadyCondition,
			infrav1.FailureDomainsReadyCondition,
		}},
	)
}

// Close the ClusterScope by updating the cluster spec, cluster status.
func (s *ClusterScope) Close() error {
	return s.PatchObject()
}

// SetFailureDomain sets the infrastructure provider failure domain key to the spec given as input.
func (s *ClusterScope) SetFailureDomain(id string, spec clusterv1.FailureDomainSpec) {
	if s.CloudStackCluster.Status.FailureDomains == nil {
		s.CloudStackCluster.Status.FailureDomains = make(clusterv1.FailureDomains)
	}
	s.CloudStackCluster.Status.FailureDomains[id] = spec
}

func (s *ClusterScope) FailureDomains() []infrav1.CloudStackFailureDomainSpec {
	return s.CloudStackCluster.Spec.FailureDomains
}

func (s *ClusterScope) OwnerGVK() schema.GroupVersionKind {
	return s.CloudStackCluster.GetObjectKind().GroupVersionKind()
}
