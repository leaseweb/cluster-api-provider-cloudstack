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
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// IsolatedNetworkScopeParams defines the input parameters used to create a new Scope.
type IsolatedNetworkScopeParams struct {
	Client                    client.Client
	Logger                    *logger.Logger
	Cluster                   *clusterv1.Cluster
	CloudStackCluster         *infrav1.CloudStackCluster
	CloudStackFailureDomain   *infrav1.CloudStackFailureDomain
	CloudStackIsolatedNetwork *infrav1.CloudStackIsolatedNetwork
	CSClients                 CSClientsProvider
	ControllerName            string
}

// NewIsolatedNetworkScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewIsolatedNetworkScope(params IsolatedNetworkScopeParams) (*IsolatedNetworkScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.CloudStackIsolatedNetwork == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackIsolatedNetwork")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	isolatedNetworkScope := &IsolatedNetworkScope{
		Logger:                    *params.Logger,
		client:                    params.Client,
		Cluster:                   params.Cluster,
		CloudStackCluster:         params.CloudStackCluster,
		CloudStackFailureDomain:   params.CloudStackFailureDomain,
		CloudStackIsolatedNetwork: params.CloudStackIsolatedNetwork,
		controllerName:            params.ControllerName,
		CSClientsProvider:         params.CSClients,
	}

	helper, err := patch.NewHelper(params.CloudStackIsolatedNetwork, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	isolatedNetworkScope.patchHelper = helper

	return isolatedNetworkScope, nil
}

// IsolatedNetworkScope defines the basic context for an actuator to operate upon.
type IsolatedNetworkScope struct {
	logger.Logger
	client      client.Client
	patchHelper *patch.Helper

	Cluster                   *clusterv1.Cluster
	CloudStackCluster         *infrav1.CloudStackCluster
	CloudStackFailureDomain   *infrav1.CloudStackFailureDomain
	CloudStackIsolatedNetwork *infrav1.CloudStackIsolatedNetwork

	CSClientsProvider
	controllerName string
}

// Name returns the isolated network name.
func (s *IsolatedNetworkScope) Name() string {
	return s.CloudStackIsolatedNetwork.Name
}

// Namespace returns the isolated network namespace.
func (s *IsolatedNetworkScope) Namespace() string {
	return s.CloudStackIsolatedNetwork.Namespace
}

// KubernetesClusterName is the name of the Kubernetes cluster.
func (s *IsolatedNetworkScope) KubernetesClusterName() string {
	return s.Cluster.Name
}

// FailureDomainName returns the isolated network's failure domain name.
func (s *IsolatedNetworkScope) FailureDomainName() string {
	return s.CloudStackIsolatedNetwork.Spec.FailureDomainName
}

// SetReady sets the CloudStackIsolatedNetwork Ready Status.
func (s *IsolatedNetworkScope) SetReady() {
	s.CloudStackIsolatedNetwork.Status.Ready = true
}

// SetNotReady sets the CloudStackIsolatedNetwork Ready Status to false.
func (s *IsolatedNetworkScope) SetNotReady() {
	s.CloudStackIsolatedNetwork.Status.Ready = false
}

// PatchObject persists the isolated network configuration and status.
func (s *IsolatedNetworkScope) PatchObject() error {
	return s.patchHelper.Patch(context.TODO(), s.CloudStackIsolatedNetwork)
}

// Close the IsolatedNetworkScope by updating the isolated network spec, isolated network status.
func (s *IsolatedNetworkScope) Close() error {
	return s.PatchObject()
}

func (s *IsolatedNetworkScope) OwnerGVK() schema.GroupVersionKind {
	return s.CloudStackIsolatedNetwork.GetObjectKind().GroupVersionKind()
}

// FailureDomainZoneID returns the failure domain's zone ID.
func (s *IsolatedNetworkScope) FailureDomainZoneID() string {
	if s.CloudStackFailureDomain == nil {
		return ""
	}
	return s.CloudStackFailureDomain.Spec.Zone.ID
}

// SetControlPlaneEndpointHost sets the control plane endpoint host.
func (s *IsolatedNetworkScope) SetControlPlaneEndpointHost(host string) {
	s.CloudStackIsolatedNetwork.Spec.ControlPlaneEndpoint.Host = host
	s.CloudStackCluster.Spec.ControlPlaneEndpoint.Host = host
}

// ControlPlaneEndpointHost returns the control plane endpoint host.
func (s *IsolatedNetworkScope) ControlPlaneEndpointHost() string {
	return s.CloudStackIsolatedNetwork.Spec.ControlPlaneEndpoint.Host
}

// SetControlPlaneEndpointPort sets the control plane endpoint port.
func (s *IsolatedNetworkScope) SetControlPlaneEndpointPort(port int32) {
	s.CloudStackIsolatedNetwork.Spec.ControlPlaneEndpoint.Port = port
	s.CloudStackCluster.Spec.ControlPlaneEndpoint.Port = port
}

// ControlPlaneEndpointPort returns the control plane endpoint port.
func (s *IsolatedNetworkScope) ControlPlaneEndpointPort() int32 {
	return s.CloudStackIsolatedNetwork.Spec.ControlPlaneEndpoint.Port
}

// SetPublicIPID sets the public IP ID.
func (s *IsolatedNetworkScope) SetPublicIPID(id string) {
	s.CloudStackIsolatedNetwork.Status.PublicIPID = id
}

// PublicIPID returns the public IP ID.
func (s *IsolatedNetworkScope) PublicIPID() string {
	return s.CloudStackIsolatedNetwork.Status.PublicIPID
}

// SetPublicIPAddress sets the public IP address.
func (s *IsolatedNetworkScope) SetPublicIPAddress(address string) {
	s.CloudStackIsolatedNetwork.Status.PublicIPAddress = address
}

// PublicIPAddress returns the public IP address.
func (s *IsolatedNetworkScope) PublicIPAddress() string {
	return s.CloudStackIsolatedNetwork.Status.PublicIPAddress
}

// SetAPIServerLoadBalancer sets the API server load balancer.
func (s *IsolatedNetworkScope) SetAPIServerLoadBalancer(loadBalancer *infrav1.LoadBalancer) {
	s.CloudStackIsolatedNetwork.Status.APIServerLoadBalancer = loadBalancer
}

// APIServerLoadBalancer returns the API server load balancer.
func (s *IsolatedNetworkScope) APIServerLoadBalancer() *infrav1.LoadBalancer {
	return s.CloudStackIsolatedNetwork.Status.APIServerLoadBalancer
}

// FailureDomain returns the failure domain of the isolated network.
func (s *IsolatedNetworkScope) FailureDomain(ctx context.Context) (*infrav1.CloudStackFailureDomain, error) {
	if s.CloudStackFailureDomain != nil {
		return s.CloudStackFailureDomain, nil
	}

	fd, err := getFailureDomainByName(ctx, s.client, s.FailureDomainName(), s.Namespace(), s.KubernetesClusterName())
	if err != nil {
		return nil, errors.Wrapf(err, "failed to get failure domain with name %s", s.FailureDomainName())
	}
	s.CloudStackFailureDomain = fd
	return fd, nil
}
