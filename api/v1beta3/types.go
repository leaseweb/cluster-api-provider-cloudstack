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

package v1beta3

// CloudStackResourceIdentifier is used to identify any CloudStack resource that can be referenced by either ID or Name.
type CloudStackResourceIdentifier struct {
	// Cloudstack resource ID.
	//+optional
	ID string `json:"id,omitempty"`

	// Cloudstack resource Name.
	//+optional
	Name string `json:"name,omitempty"`
}

// LoadBalancer represents basic information about the associated OpenStack LoadBalancer.
type LoadBalancer struct {
	IPAddress   string `json:"ipAddress"`
	IPAddressID string `json:"ipAddressID"`
	//+optional
	AllowedCIDRs []string `json:"allowedCIDRs,omitempty"`
}

type APIServerLoadBalancer struct {
	// Enabled defines whether a load balancer should be created. This value
	// defaults to true if an APIServerLoadBalancer is given.
	//
	// There is no reason to set this to false. To disable creation of the
	// API server loadbalancer, omit the APIServerLoadBalancer field in the
	// cluster spec instead.
	//
	//+kubebuilder:validation:Required
	//+kubebuilder:default:=true
	Enabled *bool `json:"enabled"`

	// AdditionalPorts adds additional tcp ports to the load balancer.
	//+optional
	//+listType=set
	AdditionalPorts []int `json:"additionalPorts,omitempty"`

	// AllowedCIDRs restrict access to all API-Server listeners to the given address CIDRs.
	//+optional
	//+listType=set
	AllowedCIDRs []string `json:"allowedCIDRs,omitempty"`
}

func (s *APIServerLoadBalancer) IsZero() bool {
	return s == nil || ((s.Enabled == nil || !*s.Enabled) && len(s.AdditionalPorts) == 0 && len(s.AllowedCIDRs) == 0)
}

func (s *APIServerLoadBalancer) IsEnabled() bool {
	// The CRD default value for Enabled is true, so if the field is nil, it should be considered as true.
	return s != nil && (s.Enabled == nil || *s.Enabled)
}
