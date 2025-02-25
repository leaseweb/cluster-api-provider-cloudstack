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

package cloud

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/hashicorp/go-multierror"
	"github.com/jellydator/ttlcache/v3"
	"github.com/pkg/errors"
	"gopkg.in/yaml.v3"
	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/metrics"
)

//go:generate ../../hack/tools/bin/mockgen -destination=../mocks/mock_client.go -package=mocks sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud Client

type Client interface {
	VMIface
	NetworkIface
	AffinityGroupIface
	TagIface
	ZoneIFace
	IsoNetworkIface
	UserCredIFace
	GetConfig() Config
	GetUser() *User
}

// Config is the cloud-config ini structure.
type Config struct {
	APIUrl    string `yaml:"api-url"`
	APIKey    string `yaml:"api-key"`
	SecretKey string `yaml:"secret-key"`
	VerifySSL string `yaml:"verify-ssl"`
	ProjectID string `yaml:"project-id"`
}

func (c *Config) Validate() (err error) {
	if c.APIUrl == "" {
		err = multierror.Append(err, errors.New("api-url is required"))
	}
	if c.APIKey == "" {
		err = multierror.Append(err, errors.New("api-key is required"))
	}
	if c.SecretKey == "" {
		err = multierror.Append(err, errors.New("secret-key is required"))
	}

	return err
}

type client struct {
	cs            *cloudstack.CloudStackClient
	csAsync       *cloudstack.CloudStackClient
	config        Config
	user          *User
	customMetrics metrics.ACSCustomMetrics
}

var _ Client = &client{}

type SecretConfig struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Type       string            `yaml:"type"`
	Metadata   map[string]string `yaml:"metadata"`
	StringData Config            `yaml:"stringData"`
}

var (
	clientCache *ttlcache.Cache[string, *client]
	cacheMutex  sync.Mutex
)

var (
	NewAsyncClient = cloudstack.NewAsyncClient
	NewClient      = cloudstack.NewClient
)

const (
	ClientConfigMapName      = "capc-client-config"
	ClientConfigMapNamespace = "capc-system"
	ClientCacheTTLKey        = "client-cache-ttl"
	DefaultClientCacheTTL    = 1 * time.Hour
)

// UnmarshalAllSecretConfigs parses a yaml document for each secret.
func UnmarshalAllSecretConfigs(in []byte, out *[]SecretConfig) error {
	r := bytes.NewReader(in)
	decoder := yaml.NewDecoder(r)
	for {
		var conf SecretConfig
		if err := decoder.Decode(&conf); err != nil {
			// Break when there are no more documents to decode
			if errors.Is(err, io.EOF) {
				return err
			}

			break
		}
		*out = append(*out, conf)
	}

	return nil
}

// NewClientFromK8sSecret returns a client from a k8s secret.
func NewClientFromK8sSecret(endpointSecret *corev1.Secret, clientConfig *corev1.ConfigMap, options ...ClientOption) (Client, error) {
	endpointSecretStrings := map[string]string{}
	for k, v := range endpointSecret.Data {
		endpointSecretStrings[k] = string(v)
	}
	bytes, err := yaml.Marshal(endpointSecretStrings)
	if err != nil {
		return nil, err
	}

	return NewClientFromBytesConfig(bytes, clientConfig, options...)
}

// NewClientFromBytesConfig returns a client from a bytes array that unmarshals to a yaml config.
func NewClientFromBytesConfig(conf []byte, clientConfig *corev1.ConfigMap, options ...ClientOption) (Client, error) {
	r := bytes.NewReader(conf)
	dec := yaml.NewDecoder(r)
	var config Config
	if err := dec.Decode(&config); err != nil {
		return nil, err
	}

	return NewClientFromConf(config, clientConfig, options...)
}

// NewClientFromYamlPath returns a client from a yaml config at path.
func NewClientFromYamlPath(confPath string, secretName string, options ...ClientOption) (Client, error) {
	content, err := os.ReadFile(confPath)
	if err != nil {
		return nil, err
	}
	configs := &[]SecretConfig{}
	if err := UnmarshalAllSecretConfigs(content, configs); err != nil {
		return nil, err
	}
	var conf Config
	for _, config := range *configs {
		if config.Metadata["name"] == secretName {
			conf = config.StringData

			break
		}
	}
	if conf.APIKey == "" {
		return nil, errors.Errorf("config with secret name %s not found", secretName)
	}

	return NewClientFromConf(conf, nil, options...)
}

// NewClientFromConf creates a new Cloud Client form a map of strings to strings.
func NewClientFromConf(conf Config, clientConfig *corev1.ConfigMap, options ...ClientOption) (Client, error) {
	cacheMutex.Lock()
	defer cacheMutex.Unlock()

	if clientCache == nil {
		clientCache = newClientCache(clientConfig)
	}

	clientCacheKey := generateClientCacheKey(conf)
	if item := clientCache.Get(clientCacheKey); item != nil {
		return item.Value(), nil
	}

	verifySSL := true
	if conf.VerifySSL == "false" {
		verifySSL = false
	}

	// The client returned from NewAsyncClient works in a synchronous way. On the other hand,
	// a client returned from NewClient works in an asynchronous way. Dive into the constructor definition
	// comments for more details
	c := &client{config: conf}
	c.cs = NewClient(conf.APIUrl, conf.APIKey, conf.SecretKey, verifySSL)
	c.csAsync = NewAsyncClient(conf.APIUrl, conf.APIKey, conf.SecretKey, verifySSL)
	c.customMetrics = metrics.NewCustomMetrics()

	p := c.cs.User.NewListUsersParams()
	userResponse, err := c.cs.User.ListUsers(p)
	if err != nil {
		return c, err
	}
	user := &User{
		ID: userResponse.Users[0].Id,
		Account: Account{
			Name: userResponse.Users[0].Account,
			Domain: Domain{
				ID: userResponse.Users[0].Domainid,
			},
		},
	}

	// Add project config if a ProjectID is defined in the client config.
	if conf.ProjectID != "" {
		user.Project = Project{
			ID: conf.ProjectID,
		}
	}

	c.user = user
	for _, fn := range options {
		if err := fn(c); err != nil {
			return nil, err
		}
	}

	if found, err := c.GetUserWithKeys(user); err != nil {
		return nil, err
	} else if !found {
		return nil, errors.Errorf(
			"could not find sufficient user (with API keys) in domain/account %s/%s", userResponse.Users[0].Domain, userResponse.Users[0].Account)
	}
	clientCache.Set(clientCacheKey, c, ttlcache.DefaultTTL)

	return c, nil
}

// NewClientInDomainAndAccount returns a new client in the specified domain and account.
func NewClientInDomainAndAccount(c Client, domain string, account string, options ...ClientOption) (Client, error) {
	user := &User{}
	user.Account.Domain.Path = domain
	user.Account.Name = account

	client := c.(*client)
	oldUser := client.user
	client.user = user
	for _, fn := range options {
		if err := fn(client); err != nil {
			return nil, err
		}
	}
	user = client.user
	client.user = oldUser

	if found, err := client.GetUserWithKeys(user); err != nil {
		return nil, err
	} else if !found {
		return nil, errors.Errorf(
			"could not find sufficient user (with API keys) in domain/account %s/%s", domain, account)
	}
	client.config.APIKey = user.APIKey
	client.config.SecretKey = user.SecretKey
	client.user = user

	return NewClientFromConf(client.config, nil)
}

// NewClientFromCSAPIClient creates a client from a CloudStack-Go API client. Used only for testing.
func NewClientFromCSAPIClient(cs *cloudstack.CloudStackClient, user *User) Client {
	if user == nil {
		user = &User{
			Account: Account{
				Domain: Domain{
					CPUAvailable:    LimitUnlimited,
					MemoryAvailable: LimitUnlimited,
					VMAvailable:     LimitUnlimited,
				},
				CPUAvailable:    LimitUnlimited,
				MemoryAvailable: LimitUnlimited,
				VMAvailable:     LimitUnlimited,
			},
		}
	}
	c := &client{
		cs:            cs,
		csAsync:       cs,
		customMetrics: metrics.NewCustomMetrics(),
		user:          user,
	}

	return c
}

func (c *client) GetUser() *User {
	return c.user
}

func (c *client) GetConfig() Config {
	return c.config
}

// generateClientCacheKey generates a cache key from a Config.
func generateClientCacheKey(conf Config) string {
	return fmt.Sprintf("%+v", conf)
}

// newClientCache returns a new instance of client cache.
func newClientCache(clientConfig *corev1.ConfigMap) *ttlcache.Cache[string, *client] {
	cache := ttlcache.New[string, *client](
		ttlcache.WithTTL[string, *client](GetClientCacheTTL(clientConfig)),
		ttlcache.WithDisableTouchOnHit[string, *client](),
	)

	go cache.Start() // starts automatic expired item deletion

	return cache
}

// GetClientCacheTTL returns a client cache TTL duration from the passed config map.
func GetClientCacheTTL(clientConfig *corev1.ConfigMap) time.Duration {
	var cacheTTL time.Duration
	if clientConfig != nil {
		if ttl, exists := clientConfig.Data[ClientCacheTTLKey]; exists {
			cacheTTL, _ = time.ParseDuration(ttl)
		}
	}
	if cacheTTL == 0 {
		cacheTTL = DefaultClientCacheTTL
	}

	return cacheTTL
}

// ClientOption can be passed to new client functions to set custom options.
type ClientOption func(*client) error

// WithProject takes either a project name or ID and sets the project ID parameter in the user object.
func WithProject(project string) ClientOption {
	return func(c *client) error {
		if c == nil || c.user == nil {
			return errors.New("cannot create client with nil user")
		}

		// project arg empty or project ID already set through cloud config.
		if project == "" || c.user.Project.ID != "" {
			return nil
		}

		if !cloudstack.IsID(project) {
			id, _, err := c.cs.Project.GetProjectID(project)
			if err != nil {
				return err
			}
			project = id
		}

		c.user.Project.ID = project

		return nil
	}
}
