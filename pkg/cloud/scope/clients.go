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
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
)

// CSClients is a struct that contains the CloudStack Provider and User Client.
type CSClients struct {
	// CSClient is the CAPC provider CloudStack Client.
	CSClient cloud.Client
	// CSUser is the CloudStack User Client based on the Failure Domain.
	CSUser cloud.Client
}

type Scope interface {
	Name() string
	Namespace() string
	KubernetesClusterName() string
	ClientFactory() cloud.Factory
	FailureDomain(ctx context.Context) (*infrav1.CloudStackFailureDomain, error)
}

// getClientsForFailureDomain gets the CloudStack Client for the Failure Domain.
func getClientsForFailureDomain(k8sClient client.Client, scope Scope) (clients CSClients, err error) {
	ctx := context.Background()

	if scope == nil {
		return clients, errors.New("scope is nil")
	}

	fd, err := scope.FailureDomain(ctx)
	if err != nil {
		return clients, errors.Wrap(err, "failed to get failure domain")
	}
	if fd == nil {
		return clients, errors.New("failure domain is nil")
	}

	endpointCredentials := &corev1.Secret{}
	key := client.ObjectKey{Name: fd.Spec.ACSEndpoint.Name, Namespace: fd.Spec.ACSEndpoint.Namespace}
	if err := k8sClient.Get(ctx, key, endpointCredentials); err != nil {
		return clients, errors.Wrapf(err, "getting ACSEndpoint secret with ref: %v", fd.Spec.ACSEndpoint)
	}

	clientConfig := &corev1.ConfigMap{}
	key = client.ObjectKey{Name: cloud.ClientConfigMapName, Namespace: cloud.ClientConfigMapNamespace}
	_ = k8sClient.Get(ctx, key, clientConfig)

	factory := scope.ClientFactory()

	if clients.CSClient, err = factory.NewClientFromK8sSecret(endpointCredentials, clientConfig, cloud.WithProject(fd.Spec.Project)); err != nil {
		return clients, errors.Wrapf(err, "parsing ACSEndpoint secret with ref: %v", fd.Spec.ACSEndpoint)
	}

	if fd.Spec.Account != "" {
		// Set CSUser CloudStack Client per Account and Domain.
		client, err := factory.NewClientInDomainAndAccount(clients.CSClient, fd.Spec.Domain, fd.Spec.Account, cloud.WithProject(fd.Spec.Project))
		if err != nil {
			return clients, errors.Wrapf(err, "creating CloudStack User Client with domain %s and account %s", fd.Spec.Domain, fd.Spec.Account)
		}
		clients.CSUser = client
	} else {
		// Set CSUser CloudStack Client to CSClient since Account & Domain weren't provided.
		clients.CSUser = clients.CSClient
	}

	return clients, nil
}

// getFailureDomainByName gets the CloudStack Failure Domain by name.
func getFailureDomainByName(ctx context.Context, k8sClient client.Client, name, namespace, clusterName string) (*infrav1.CloudStackFailureDomain, error) {
	fd := &infrav1.CloudStackFailureDomain{}
	metaHashName := infrav1.FailureDomainHashedMetaName(name, clusterName)
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: metaHashName, Namespace: namespace}, fd); err != nil {
		return nil, errors.Wrapf(err, "failed to get failure domain with name %s", name)
	}
	return fd, nil
}
