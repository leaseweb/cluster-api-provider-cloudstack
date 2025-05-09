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
	"strings"

	"github.com/pkg/errors"
)

const (
	rootDomain      = "ROOT"
	domainDelimiter = "/"
)

type UserCredIFace interface {
	ResolveDomain(domain *Domain) error
	ResolveAccount(account *Account) error
	ResolveUser(user *User) error
	ResolveUserKeys(user *User) error
	GetUserWithKeys(user *User) (bool, error)
}

// Domain contains specifications that identify a domain.
type Domain struct {
	Name            string
	Path            string
	ID              string
	CPUAvailable    string
	MemoryAvailable string
	VMAvailable     string
}

// Account contains specifications that identify an account.
type Account struct {
	Name            string
	Domain          Domain
	ID              string
	CPUAvailable    string
	MemoryAvailable string
	VMAvailable     string
}

// Project contains specifications that identify a project.
type Project struct {
	Name            string
	ID              string
	CPUAvailable    string
	MemoryAvailable string
	VMAvailable     string
}

// User contains information uniquely identifying and scoping a user.
type User struct {
	ID        string
	Name      string
	APIKey    string
	SecretKey string
	Account
	Project
}

// ResolveDomain resolves a domain's information.
func (c *client) ResolveDomain(domain *Domain) error {
	// A domain can be specified by Id, Name, and or Path.
	// Parse path and use it to set name if not present.
	tokens := []string{}
	if domain.Path != "" {
		// Split path and get name.
		tokens = strings.Split(domain.Path, domainDelimiter)
		if domain.Name == "" {
			domain.Name = tokens[len(tokens)-1]
		}
		// Ensure the path begins with ROOT.
		if !strings.EqualFold(tokens[0], rootDomain) {
			tokens = append([]string{rootDomain}, tokens...)
		} else {
			tokens[0] = rootDomain
		}
		domain.Path = strings.Join(tokens, domainDelimiter)
	}

	// Set present search/list parameters.
	p := c.cs.Domain.NewListDomainsParams()
	p.SetListall(true)
	setIfNotEmpty(domain.Name, p.SetName)
	setIfNotEmpty(domain.ID, p.SetId)

	// If path was provided also use level narrow the search for domain.
	if level := len(tokens) - 1; level >= 0 {
		p.SetLevel(level)
	}

	resp, retErr := c.cs.Domain.ListDomains(p)
	if retErr != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(retErr)

		return retErr
	}

	// If the Id was provided.
	if domain.ID != "" {
		if resp.Count != 1 {
			return errors.Errorf("domain ID %s provided, expected exactly one domain, got %d", domain.ID, resp.Count)
		}
		if domain.Path != "" && !strings.EqualFold(resp.Domains[0].Path, domain.Path) {
			return errors.Errorf("domain Path %s did not match domain ID %s", domain.Path, domain.ID)
		}
		domain.Path = resp.Domains[0].Path
		domain.Name = resp.Domains[0].Name
		domain.CPUAvailable = resp.Domains[0].Cpuavailable
		domain.MemoryAvailable = resp.Domains[0].Memoryavailable
		domain.VMAvailable = resp.Domains[0].Vmavailable

		return nil
	}

	// Consider the case where only the domain name is provided.
	if domain.Path == "" && domain.Name != "" {
		if resp.Count != 1 {
			return errors.Errorf(
				"only domain name: %s provided, expected exactly one domain, got %d", domain.Name, resp.Count)
		}
	}

	// Finally, search for the domain by Path.
	for _, possibleDomain := range resp.Domains {
		if possibleDomain.Path != domain.Path {
			continue
		}
		domain.ID = possibleDomain.Id
		domain.CPUAvailable = possibleDomain.Cpuavailable
		domain.MemoryAvailable = possibleDomain.Memoryavailable
		domain.VMAvailable = possibleDomain.Vmavailable

		return nil
	}

	return errors.Errorf("domain not found for domain path %s", domain.Path)
}

// ResolveAccount resolves an account's information.
func (c *client) ResolveAccount(account *Account) error {
	// Resolve domain prior to any account resolution activity.
	if err := c.ResolveDomain(&account.Domain); err != nil &&
		!strings.Contains(err.Error(), "The API [listDomains] does not exist or is not available for the account Account") {
		return errors.Wrapf(err, "resolving domain %s details", account.Domain.Name)
	}

	p := c.cs.Account.NewListAccountsParams()
	p.SetDomainid(account.Domain.ID)
	setIfNotEmpty(account.ID, p.SetId)
	setIfNotEmpty(account.Name, p.SetName)
	resp, retErr := c.cs.Account.ListAccounts(p)
	if retErr != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(retErr)

		return retErr
	} else if resp.Count == 0 {
		return errors.Errorf("could not find account %s", account.Name)
	} else if resp.Count != 1 {
		return errors.Errorf("expected 1 Account with account name %s in domain ID %s, but got %d",
			account.Name, account.Domain.ID, resp.Count)
	}
	account.ID = resp.Accounts[0].Id
	account.Name = resp.Accounts[0].Name
	account.CPUAvailable = resp.Accounts[0].Cpuavailable
	account.MemoryAvailable = resp.Accounts[0].Memoryavailable
	account.VMAvailable = resp.Accounts[0].Vmavailable

	return nil
}

// ResolveProject resolves a project's information.
func (c *client) ResolveProject(user *User) error {
	if user.Project.ID == "" {
		return nil
	}

	p := c.cs.Project.NewListProjectsParams()
	p.SetListall(true)
	p.SetDomainid(c.user.Domain.ID)
	p.SetAccount(c.user.Account.Name)
	p.SetId(user.Project.ID)
	resp, err := c.cs.Project.ListProjects(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	} else if resp.Count == 0 {
		return errors.Errorf("could not find project with ID '%s'", user.Project.ID)
	} else if resp.Count != 1 {
		return errors.Errorf("expected 1 Project with ID '%s' in domain ID '%s', but got %d",
			user.Project.ID, c.user.Domain.ID, resp.Count)
	}
	c.user.Project.ID = resp.Projects[0].Id
	c.user.Project.Name = resp.Projects[0].Name
	c.user.Project.CPUAvailable = resp.Projects[0].Cpuavailable
	c.user.Project.MemoryAvailable = resp.Projects[0].Memoryavailable
	c.user.Project.VMAvailable = resp.Projects[0].Vmavailable

	return nil
}

// ResolveUser resolves a user's information.
func (c *client) ResolveUser(user *User) error {
	// Resolve account prior to any user resolution activity.
	if err := c.ResolveAccount(&user.Account); err != nil {
		return errors.Wrapf(err, "resolving account %s details", user.Account.Name)
	}

	p := c.cs.User.NewListUsersParams()
	p.SetAccount(user.Account.Name)
	p.SetDomainid(user.Domain.ID)
	p.SetListall(true)
	setIfNotEmpty(user.ID, p.SetId)
	resp, err := c.cs.User.ListUsers(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	} else if resp.Count != 1 {
		return errors.Errorf("expected 1 User with username %s but got %d", user.Name, resp.Count)
	}

	user.ID = resp.Users[0].Id
	user.Name = resp.Users[0].Username

	return nil
}

// ResolveUserKeys resolves a user's api keys.
func (c *client) ResolveUserKeys(user *User) error {
	// Resolve user prior to any api key resolution activity.
	if err := c.ResolveUser(user); err != nil {
		return errors.Wrap(err, "error encountered when resolving user details")
	}

	p := c.cs.User.NewGetUserKeysParams(user.ID)
	resp, err := c.cs.User.GetUserKeys(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return errors.Errorf("error encountered when resolving user api keys for user %s", user.Name)
	}
	user.APIKey = resp.Apikey
	user.SecretKey = resp.Secretkey

	return nil
}

// GetUserWithKeys will search a domain and account for the first user that has api keys.
// Returns true if a user is found and false otherwise.
func (c *client) GetUserWithKeys(user *User) (bool, error) {
	// Resolve account prior to any user resolution activity.
	if err := c.ResolveAccount(&user.Account); err != nil {
		return false, errors.Wrapf(err, "resolving account %s details", user.Account.Name)
	}

	if err := c.ResolveProject(user); err != nil {
		return false, errors.Wrapf(err, "resolving project %s details", user.Project.Name)
	}

	// List users and take first user that has already has api keys.
	p := c.cs.User.NewListUsersParams()
	p.SetAccount(user.Account.Name)
	setIfNotEmpty(user.Account.Domain.ID, p.SetDomainid)
	p.SetListall(true)
	resp, err := c.cs.User.ListUsers(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return false, err
	}

	// Return first user with keys.
	for _, possibleUser := range resp.Users {
		user.ID = possibleUser.Id
		if err := c.ResolveUserKeys(user); err == nil {
			return true, nil
		}
	}
	user.ID = ""

	return false, nil
}
