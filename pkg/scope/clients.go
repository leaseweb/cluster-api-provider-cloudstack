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

// Package scope implement the scope for the CloudStack clients when doing the reconciliation process.
package scope

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/util/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
)

const (
	ClientCacheTTL = 1 * time.Hour
)

// CSClientsProvider defines the interface for accessing CloudStack clients.
type CSClientsProvider interface {
	// CSClient returns the CAPC provider CloudStack Client.
	CSClient() cloud.Client
	// CSUser returns the CloudStack User Client
	CSUser() cloud.Client
}

// CSClients implements CSClientsProvider.
type CSClients struct {
	csClient cloud.Client
	csUser   cloud.Client
}

func (c CSClients) CSClient() cloud.Client {
	return c.csClient
}

func (c CSClients) CSUser() cloud.Client {
	return c.csUser
}

// NewCSClients creates a new CSClients instance.
func NewCSClients(csClient, csUser cloud.Client) CSClients {
	return CSClients{
		csClient: csClient,
		csUser:   csUser,
	}
}

type Scope interface {
	CSClients() CSClientsProvider
	FailureDomain() *infrav1.CloudStackFailureDomain
	ProjectID() string
}

// ClientScopeFactory instantiates a new Scope using credentials from a failure domain.
type ClientScopeFactory interface {
	NewClientScopeForFailureDomain(ctx context.Context, k8sClient client.Client, fd *infrav1.CloudStackFailureDomain) (Scope, error)
	NewClientScopeForFailureDomainByName(ctx context.Context, k8sClient client.Client, name, namespace, clusterName string) (Scope, error)
}

type clientScopeFactory struct {
	clientCache *cache.LRUExpireCache
}

// NewClientScopeFactory creates the default scope factory. It generates clients which make CloudStack API calls against a running cloud.
func NewClientScopeFactory(maxCacheSize int) ClientScopeFactory {
	var c *cache.LRUExpireCache
	if maxCacheSize > 0 {
		c = cache.NewLRUExpireCache(maxCacheSize)
	}
	return &clientScopeFactory{
		clientCache: c,
	}
}

func (s *clientScopeFactory) NewClientScopeForFailureDomain(ctx context.Context, k8sClient client.Client, fd *infrav1.CloudStackFailureDomain) (scope Scope, err error) {
	if fd == nil {
		return nil, errors.New("failure domain is nil")
	}

	cloudConfig, err := getCloudConfigFromSecret(ctx, k8sClient, fd.Spec.ACSEndpoint.Namespace, fd.Spec.ACSEndpoint.Name)
	if err != nil {
		return nil, errors.Wrapf(err, "getting client config from secret")
	}

	clientConfig, err := getClientConfig(ctx, k8sClient, fd.Spec.ACSEndpoint.Namespace, fd.Spec.ACSEndpoint.Name)
	if err != nil {
		return nil, errors.Wrapf(err, "getting client configuration configmap")
	}

	return NewClientScope(s.clientCache, fd, cloudConfig, clientConfig)
}

func (s *clientScopeFactory) NewClientScopeForFailureDomainByName(ctx context.Context, k8sClient client.Client, name, namespace, clusterName string) (scope Scope, err error) {
	fd, err := getFailureDomainByName(ctx, k8sClient, name, namespace, clusterName)
	if err != nil {
		return nil, err
	}
	return s.NewClientScopeForFailureDomain(ctx, k8sClient, fd)
}

type clientScope struct {
	clients       CSClients
	failureDomain *infrav1.CloudStackFailureDomain
	projectID     string
}

func getScopeCacheKey(clientConfig cloud.Config) string {
	return fmt.Sprintf("%d", HashConfig(clientConfig))
}

func NewClientScope(cache *cache.LRUExpireCache, fd *infrav1.CloudStackFailureDomain, cloudConfig cloud.Config, clientConfig *corev1.ConfigMap) (scope Scope, err error) {
	key := getScopeCacheKey(cloudConfig)

	if scope, found := cache.Get(key); found {
		return scope.(Scope), nil
	}

	csClient, err := cloud.NewClientFromConf(cloudConfig, clientConfig, cloud.WithProject(fd.Spec.Project))
	if err != nil {
		return nil, errors.Wrapf(err, "parsing ACSEndpoint secret with ref: %v", fd.Spec.ACSEndpoint)
	}
	csUser := csClient
	if fd.Spec.Account != "" {
		// Set CSUser CloudStack Client per Account and Domain.
		csUser, err = cloud.NewClientInDomainAndAccount(csClient, fd.Spec.Domain, fd.Spec.Account, cloud.WithProject(fd.Spec.Project))
		if err != nil {
			return nil, errors.Wrapf(err, "creating CloudStack User Client with domain %s and account %s", fd.Spec.Domain, fd.Spec.Account)
		}
	}

	scope = &clientScope{
		clients:       NewCSClients(csClient, csUser),
		failureDomain: fd,
		projectID:     fd.Spec.Project,
	}
	cache.Add(key, scope, ClientCacheTTL)

	return scope, nil
}

func (s *clientScope) CSClients() CSClientsProvider {
	return s.clients
}

func (s *clientScope) FailureDomain() *infrav1.CloudStackFailureDomain {
	return s.failureDomain
}

func (s *clientScope) ProjectID() string {
	return s.projectID
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

// getCloudConfigFromSecret gets the CloudStack credentials from a k8s secret referenced by the failure domain ACSEndpoint.
func getCloudConfigFromSecret(ctx context.Context, k8sClient client.Client, secretNamespace, secretName string) (cloud.Config, error) {
	cloudConfig := cloud.Config{}
	endpointSecret := &corev1.Secret{}
	key := client.ObjectKey{Name: secretName, Namespace: secretNamespace}
	if err := k8sClient.Get(ctx, key, endpointSecret); err != nil {
		return cloudConfig, errors.Wrapf(err, "getting ACSEndpoint secret with ref: %s/%s", secretNamespace, secretName)
	}

	endpointSecretStrings := map[string]string{}
	for k, v := range endpointSecret.Data {
		endpointSecretStrings[k] = string(v)
	}
	bytes, err := yaml.Marshal(endpointSecretStrings)
	if err != nil {
		return cloudConfig, err
	}

	if err := yaml.Unmarshal(bytes, &cloudConfig); err != nil {
		return cloudConfig, err
	}

	if err := cloudConfig.Validate(); err != nil {
		return cloudConfig, errors.Wrapf(err, "invalid cloud config")
	}

	return cloudConfig, nil
}

// getClientConfig gets the client configuration configmap.
func getClientConfig(ctx context.Context, k8sClient client.Client, configMapNamespace, configMapName string) (*corev1.ConfigMap, error) {
	clientConfig := &corev1.ConfigMap{}
	key := client.ObjectKey{Name: configMapName, Namespace: configMapNamespace}
	if err := k8sClient.Get(ctx, key, clientConfig); err != nil {
		if !k8serrors.IsNotFound(err) {
			return nil, errors.Wrapf(err, "getting client configuration configmap with ref: %s/%s", configMapNamespace, configMapName)
		}
	}

	return clientConfig, nil
}
