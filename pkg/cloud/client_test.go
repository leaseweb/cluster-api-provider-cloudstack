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

package cloud_test

import (
	"os"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
	helpers "sigs.k8s.io/cluster-api-provider-cloudstack/test/helpers/cloud"
)

var _ = Describe("Client", func() {
	var (
		mockCtrl   *gomock.Controller
		mockClient *cloudstack.CloudStackClient
		us         *cloudstack.MockUserServiceIface
		ds         *cloudstack.MockDomainServiceIface
		as         *cloudstack.MockAccountServiceIface
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = cloudstack.NewMockClient(mockCtrl)
		us = mockClient.User.(*cloudstack.MockUserServiceIface)
		ds = mockClient.Domain.(*cloudstack.MockDomainServiceIface)
		as = mockClient.Account.(*cloudstack.MockAccountServiceIface)
	})

	AfterEach(func() {
	})

	Context("When fetching a YAML config.", func() {
		It("Handles the positive case.", func() {
			// This test fixture is useful for development, but the actual method of parsing is confinded to the client's
			// new client method. The parsing used here is more of a schema, and we don't need to test another library's
			// abilities to parse said schema.
			Skip("Dev test suite.")
			// Create a real cloud client.
			var errConnect error
			_, errConnect = helpers.NewCSClient()
			Ω(errConnect).ShouldNot(HaveOccurred())

			_, errConnect = cloud.NewClientFromYamlPath(os.Getenv("REPO_ROOT")+"/cloud-config.yaml", "myendpoint")
			Ω(errConnect).ShouldNot(HaveOccurred())
		})
	})

	Context("GetClientCacheTTL", func() {
		It("Returns the default TTL when a nil is passed", func() {
			result := cloud.GetClientCacheTTL(nil)
			Ω(result).Should(Equal(cloud.DefaultClientCacheTTL))
		})

		It("Returns the default TTL when an empty config map is passed", func() {
			clientConfig := &corev1.ConfigMap{}
			result := cloud.GetClientCacheTTL(clientConfig)
			Ω(result).Should(Equal(cloud.DefaultClientCacheTTL))
		})

		It("Returns the default TTL when the TTL key does not exist", func() {
			clientConfig := &corev1.ConfigMap{}
			clientConfig.Data = map[string]string{}
			clientConfig.Data[cloud.ClientCacheTTLKey+"XXXX"] = "1m5s"
			result := cloud.GetClientCacheTTL(clientConfig)
			Ω(result).Should(Equal(cloud.DefaultClientCacheTTL))
		})

		It("Returns the default TTL when the TTL value is invalid", func() {
			clientConfig := &corev1.ConfigMap{}
			clientConfig.Data = map[string]string{}
			clientConfig.Data[cloud.ClientCacheTTLKey] = "5mXXX"
			result := cloud.GetClientCacheTTL(clientConfig)
			Ω(result).Should(Equal(cloud.DefaultClientCacheTTL))
		})

		It("Returns the TTL from the input clientConfig map", func() {
			clientConfig := &corev1.ConfigMap{}
			clientConfig.Data = map[string]string{}
			clientConfig.Data[cloud.ClientCacheTTLKey] = "5m10s"
			expected, _ := time.ParseDuration("5m10s")
			result := cloud.GetClientCacheTTL(clientConfig)
			Ω(result).Should(Equal(expected))
		})
	})

	Context("NewClientFromConf", func() {
		clientConfig := &corev1.ConfigMap{}
		cloud.NewAsyncClient = func(_, _, _ string, _ bool, _ ...cloudstack.ClientOption) *cloudstack.CloudStackClient {
			return mockClient
		}
		cloud.NewClient = func(_, _, _ string, _ bool, _ ...cloudstack.ClientOption) *cloudstack.CloudStackClient {
			return mockClient
		}

		BeforeEach(func() {
			clientConfig.Data = map[string]string{}
			clientConfig.Data[cloud.ClientCacheTTLKey] = "100ms"
			fakeListParams := &cloudstack.ListUsersParams{}
			fakeUser := &cloudstack.User{
				Id:      dummies.UserID,
				Account: dummies.AccountName,
				Domain:  dummies.DomainName,
			}
			us.EXPECT().NewListUsersParams().Return(fakeListParams).AnyTimes()
			us.EXPECT().ListUsers(fakeListParams).Return(&cloudstack.ListUsersResponse{
				Count: 1, Users: []*cloudstack.User{fakeUser},
			}, nil).AnyTimes()

			dsp := &cloudstack.ListDomainsParams{}
			ds.EXPECT().NewListDomainsParams().Return(dsp).AnyTimes()
			ds.EXPECT().ListDomains(dsp).Return(&cloudstack.ListDomainsResponse{Count: 1, Domains: []*cloudstack.Domain{{
				Id:   dummies.DomainID,
				Name: dummies.DomainName,
				Path: dummies.DomainPath,
			}}}, nil).AnyTimes()

			asp := &cloudstack.ListAccountsParams{}
			as.EXPECT().NewListAccountsParams().Return(asp).AnyTimes()
			as.EXPECT().ListAccounts(asp).Return(&cloudstack.ListAccountsResponse{Count: 1, Accounts: []*cloudstack.Account{{
				Id:   dummies.AccountID,
				Name: dummies.AccountName,
			}}}, nil).AnyTimes()
			ukp := &cloudstack.GetUserKeysParams{}
			us.EXPECT().NewGetUserKeysParams(gomock.Any()).Return(ukp).AnyTimes()
			us.EXPECT().GetUserKeys(ukp).Return(&cloudstack.GetUserKeysResponse{
				Apikey:    dummies.Apikey,
				Secretkey: dummies.SecretKey,
			}, nil).AnyTimes()
		})

		It("Returns a new client", func() {
			config := cloud.Config{
				APIUrl: "http://1.1.1.1",
			}
			result, err := cloud.NewClientFromConf(config, clientConfig)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(result).ShouldNot(BeNil())
		})

		It("Returns a new client for a different config", func() {
			config1 := cloud.Config{
				APIUrl: "http://2.2.2.2",
			}
			config2 := cloud.Config{
				APIUrl: "http://3.3.3.3",
			}
			result1, _ := cloud.NewClientFromConf(config1, clientConfig)
			result2, _ := cloud.NewClientFromConf(config2, clientConfig)
			Ω(result1).ShouldNot(Equal(result2))
		})

		It("Returns a cached client for the same config", func() {
			config1 := cloud.Config{
				APIUrl: "http://4.4.4.4",
			}
			config2 := cloud.Config{
				APIUrl: "http://4.4.4.4",
			}
			result1, _ := cloud.NewClientFromConf(config1, clientConfig)
			result2, _ := cloud.NewClientFromConf(config2, clientConfig)
			Ω(result1).Should(Equal(result2))
		})
	})
})
