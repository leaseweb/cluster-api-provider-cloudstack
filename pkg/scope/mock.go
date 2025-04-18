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

package scope

import (
	"context"

	"github.com/pkg/errors"
	"go.uber.org/mock/gomock"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/mocks"
)

type MockClientScopeFactory struct {
	csClients *MockCSClients
	projectID string
}

func NewMockClientScopeFactory(mockCtrl *gomock.Controller, projectID string) *MockClientScopeFactory {
	csClients := NewMockCSClients(mockCtrl)
	return &MockClientScopeFactory{
		csClients: csClients,
		projectID: projectID,
	}
}

func (m *MockClientScopeFactory) NewClientScopeForFailureDomain(_ context.Context, _ client.Client, fd *infrav1.CloudStackFailureDomain) (Scope, error) {
	if fd == nil {
		return nil, errors.New("failure domain is nil")
	}

	return m, nil
}

// ProjectID returns the CloudStack project ID, if any.
func (m *MockClientScopeFactory) ProjectID() string {
	return m.projectID
}

// CSClients returns the CloudStack clients.
func (m *MockClientScopeFactory) CSClients() CSClientsProvider {
	return m.csClients
}

// MockCSClientsProvider extends CSClientsProvider to provide access to mock clients for testing.
type MockCSClientsProvider interface {
	CSClientsProvider
	// MockCSClient returns the mock CloudStack client.
	MockCSClient() *mocks.MockClient
	// MockCSUser returns the mock CloudStack user (account/domain/project level) client.
	MockCSUser() *mocks.MockClient
}

// MockCSClients implements MockCSClientsProvider.
type MockCSClients struct {
	csClient *mocks.MockClient
	csUser   *mocks.MockClient
}

// CSClient returns the CAPC provider CloudStack Client.
func (m *MockCSClients) CSClient() cloud.Client {
	return m.csClient
}

func (m *MockCSClients) CSUser() cloud.Client {
	return m.csUser
}

// MockCSClient returns the mock CloudStack client.
func (m *MockCSClients) MockCSClient() *mocks.MockClient {
	return m.csClient
}

// MockCSUser returns the mock CloudStack user (account/domain/project level) client.
func (m *MockCSClients) MockCSUser() *mocks.MockClient {
	return m.csUser
}

// NewMockCSClients returns the mock CloudStack clients.
func NewMockCSClients(mockCtrl *gomock.Controller) *MockCSClients {
	return &MockCSClients{
		csClient: mocks.NewMockClient(mockCtrl),
		csUser:   mocks.NewMockClient(mockCtrl),
	}
}

// MockCSClients returns the mock CloudStack clients.
func (m *MockClientScopeFactory) MockCSClients() MockCSClientsProvider {
	return m.csClients
}
