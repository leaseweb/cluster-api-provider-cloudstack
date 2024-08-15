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
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	utilsnet "k8s.io/utils/net"
	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	capcstrings "sigs.k8s.io/cluster-api-provider-cloudstack/pkg/utils/strings"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/record"
)

type IsoNetworkIface interface {
	GetOrCreateIsolatedNetwork(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackIsolatedNetwork, *infrav1.CloudStackCluster) error

	AssociatePublicIPAddress(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackIsolatedNetwork, *infrav1.CloudStackCluster) error
	CreateEgressFirewallRules(*infrav1.CloudStackIsolatedNetwork) error
	GetPublicIP(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackCluster) (*cloudstack.PublicIpAddress, error)
	GetLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork) ([]*cloudstack.LoadBalancerRule, error)
	ReconcileLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error
	GetFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork) ([]*cloudstack.FirewallRule, error)
	ReconcileFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error

	AssignVMToLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, instanceID string) error
	DeleteNetwork(infrav1.Network) error
	DisposeIsoNetResources(*infrav1.CloudStackIsolatedNetwork, *infrav1.CloudStackCluster) error
}

// getNetworkOfferingID fetches the id of a network offering.
func (c *client) getNetworkOfferingID() (string, error) {
	offeringID, count, retErr := c.cs.NetworkOffering.GetNetworkOfferingID(NetOffering)
	if retErr != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(retErr)
		return "", retErr
	} else if count != 1 {
		return "", errors.New("found more than one network offering")
	}

	return offeringID, nil
}

// AssociatePublicIPAddress Gets a PublicIP and associates the public IP to passed isolated network.
func (c *client) AssociatePublicIPAddress(
	fd *infrav1.CloudStackFailureDomain,
	isoNet *infrav1.CloudStackIsolatedNetwork,
	csCluster *infrav1.CloudStackCluster,
) (retErr error) {
	// Check specified IP address is available or get an unused one if not specified.
	publicAddress, err := c.GetPublicIP(fd, csCluster)
	if err != nil {
		return errors.Wrapf(err, "fetching a public IP address")
	}
	isoNet.Spec.ControlPlaneEndpoint.Host = publicAddress.Ipaddress
	if !annotations.IsExternallyManaged(csCluster) {
		// Do not update the infracluster's controlPlaneEndpoint when the controlplane
		// is externally managed, it is the responsibility of the externally managed
		// control plane to update this.
		csCluster.Spec.ControlPlaneEndpoint.Host = publicAddress.Ipaddress
	}
	isoNet.Status.PublicIPID = publicAddress.Id
	if isoNet.Status.APIServerLoadBalancer == nil {
		isoNet.Status.APIServerLoadBalancer = &infrav1.LoadBalancer{}
	}
	isoNet.Status.APIServerLoadBalancer.IP = publicAddress.Ipaddress

	// Check if the address is already associated with the network.
	if publicAddress.Associatednetworkid == isoNet.Spec.ID {
		return nil
	}

	// Public IP found, but not yet associated with network -- associate it.
	p := c.cs.Address.NewAssociateIpAddressParams()
	p.SetIpaddress(isoNet.Spec.ControlPlaneEndpoint.Host)
	p.SetNetworkid(isoNet.Spec.ID)
	if _, err := c.cs.Address.AssociateIpAddress(p); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrapf(err,
			"associating public IP address with ID %s to network with ID %s",
			publicAddress.Id, isoNet.Spec.ID)
	} else if err := c.AddClusterTag(ResourceTypeIPAddress, publicAddress.Id, csCluster); err != nil {
		return errors.Wrapf(err,
			"adding tag to public IP address with ID %s", publicAddress.Id)
	} else if err := c.AddCreatedByCAPCTag(ResourceTypeIPAddress, isoNet.Status.PublicIPID); err != nil {
		return errors.Wrapf(err,
			"adding tag to public IP address with ID %s", publicAddress.Id)
	}
	return nil
}

// CreateIsolatedNetwork creates an isolated network in the relevant FailureDomain per passed network specification.
func (c *client) CreateIsolatedNetwork(fd *infrav1.CloudStackFailureDomain, isoNet *infrav1.CloudStackIsolatedNetwork) (retErr error) {
	offeringID, err := c.getNetworkOfferingID()
	if err != nil {
		return err
	}

	// Do isolated network creation.
	p := c.cs.Network.NewCreateNetworkParams(isoNet.Spec.Name, offeringID, fd.Spec.Zone.ID)
	p.SetDisplaytext(isoNet.Spec.Name)
	if isoNet.Spec.Domain != "" {
		p.SetNetworkdomain(isoNet.Spec.Domain)
	}
	resp, err := c.cs.Network.CreateNetwork(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrapf(err, "creating network with name %s", isoNet.Spec.Name)
	}
	isoNet.Spec.ID = resp.Id
	isoNet.Status.CIDR = resp.Cidr

	return c.AddCreatedByCAPCTag(ResourceTypeNetwork, isoNet.Spec.ID)
}

// CreateEgressFirewallRules sets the egress firewall rules for an isolated network.
func (c *client) CreateEgressFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork) (retErr error) {
	protocols := []string{NetworkProtocolTCP, NetworkProtocolUDP, NetworkProtocolICMP}
	for _, proto := range protocols {
		p := c.cs.Firewall.NewCreateEgressFirewallRuleParams(isoNet.Spec.ID, proto)

		if proto == "icmp" {
			p.SetIcmptype(-1)
			p.SetIcmpcode(-1)
		}

		_, err := c.cs.Firewall.CreateEgressFirewallRule(p)
		if err != nil &&
			// Ignore errors regarding already existing fw rules for TCP/UDP
			!strings.Contains(strings.ToLower(err.Error()), "there is already") &&
			// Ignore errors regarding already existing fw rule for ICMP
			!strings.Contains(strings.ToLower(err.Error()), "new rule conflicts with existing rule") {
			retErr = multierror.Append(retErr, errors.Wrapf(
				err, "failed creating egress firewall rule for network ID %s protocol %s", isoNet.Spec.ID, proto))
		}
	}
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(retErr)
	return retErr
}

// GetPublicIP gets a public IP with ID for cluster endpoint.
func (c *client) GetPublicIP(
	fd *infrav1.CloudStackFailureDomain,
	csCluster *infrav1.CloudStackCluster,
) (*cloudstack.PublicIpAddress, error) {
	ip := csCluster.Spec.ControlPlaneEndpoint.Host

	p := c.cs.Address.NewListPublicIpAddressesParams()
	p.SetAllocatedonly(false)
	p.SetZoneid(fd.Spec.Zone.ID)
	setIfNotEmpty(ip, p.SetIpaddress)
	publicAddresses, err := c.cs.Address.ListPublicIpAddresses(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, err
	} else if ip != "" && publicAddresses.Count == 1 {
		// Endpoint specified and IP found.
		// Ignore already allocated here since the IP was specified.
		return publicAddresses.PublicIpAddresses[0], nil
	} else if publicAddresses.Count > 0 {
		// Endpoint not specified. Pick first available address.
		for _, v := range publicAddresses.PublicIpAddresses {
			if v.Allocated == "" { // Found un-allocated Public IP.
				return v, nil
			}
		}
		return nil, errors.New("all Public IP Address(es) found were already allocated")
	}
	return nil, errors.New("no public addresses found in available networks")
}

// GetIsolatedNetwork gets an isolated network in the relevant Zone.
func (c *client) GetIsolatedNetwork(isoNet *infrav1.CloudStackIsolatedNetwork) (retErr error) {
	netDetails, count, err := c.cs.Network.GetNetworkByName(isoNet.Spec.Name)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		retErr = multierror.Append(retErr, errors.Wrapf(err, "could not get Network ID from %s", isoNet.Spec.Name))
	} else if count != 1 {
		retErr = multierror.Append(retErr, errors.Errorf(
			"expected 1 Network with name %s, but got %d", isoNet.Name, count))
	} else { // Got netID from the network's name.
		isoNet.Spec.ID = netDetails.Id
		isoNet.Status.CIDR = netDetails.Cidr
		return nil
	}

	netDetails, count, err = c.cs.Network.GetNetworkByID(isoNet.Spec.ID)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return multierror.Append(retErr, errors.Wrapf(err, "could not get Network by ID %s", isoNet.Spec.ID))
	} else if count != 1 {
		return multierror.Append(retErr, errors.Errorf("expected 1 Network with UUID %s, but got %d", isoNet.Spec.ID, count))
	}
	isoNet.Spec.Name = netDetails.Name
	isoNet.Status.CIDR = netDetails.Cidr
	return nil
}

// GetLoadBalancerRules fetches the current loadbalancer rules for the isolated network.
func (c *client) GetLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork) ([]*cloudstack.LoadBalancerRule, error) {
	p := c.cs.LoadBalancer.NewListLoadBalancerRulesParams()
	p.SetPublicipid(isoNet.Status.PublicIPID)
	loadBalancerRules, err := c.cs.LoadBalancer.ListLoadBalancerRules(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, errors.Wrap(err, "listing load balancer rules")
	}

	return loadBalancerRules.LoadBalancerRules, nil
}

func (c *client) ReconcileLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error {
	lbr, err := c.GetLoadBalancerRules(isoNet)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrap(err, "retrieving load balancer rules")
	}

	// Create a map for easy lookup of existing rules
	portsAndIDs := make(map[string]string)
	for _, rule := range lbr {
		portsAndIDs[rule.Publicport] = rule.Id
	}

	ports := []int{int(csCluster.Spec.ControlPlaneEndpoint.Port)}
	if len(csCluster.Spec.APIServerLoadBalancer.AdditionalPorts) > 0 {
		ports = append(ports, csCluster.Spec.APIServerLoadBalancer.AdditionalPorts...)
	}

	lbRuleIDs := make([]string, 0)
	for _, port := range ports {
		// Check if lb rule for port already exists
		ruleID, found := portsAndIDs[strconv.Itoa(port)]
		if found {
			lbRuleIDs = append(lbRuleIDs, ruleID)
		} else {
			// If not found, create the lb rule for port
			ruleID, err = c.CreateLoadBalancerRule(isoNet, port)
			if err != nil {
				return errors.Wrap(err, "creating load balancer rule")
			}
			lbRuleIDs = append(lbRuleIDs, ruleID)
		}

		// For backwards compatibility.
		if port == int(csCluster.Spec.ControlPlaneEndpoint.Port) {
			isoNet.Status.LBRuleID = ruleID
		}
	}

	// Delete any existing rules with a port that is no longer part of ports.
	for port, ruleID := range portsAndIDs {
		intport, err := strconv.Atoi(port)
		if err != nil {
			return errors.Wrap(err, "converting port to int")
		}

		if !slices.Contains(ports, intport) {
			success, err := c.DeleteLoadBalancerRule(ruleID)
			if err != nil {
				return errors.Wrap(err, "deleting load balancer rule")
			}
			if !success {
				return errors.New("delete load balancer rule returned unsuccessful")
			}
		}
	}

	if len(lbRuleIDs) > 1 {
		capcstrings.Canonicalize(lbRuleIDs)
	}

	isoNet.Status.LoadBalancerRuleIDs = lbRuleIDs

	return nil
}

// CreateLoadBalancerRule configures the loadbalancer to accept traffic to a certain IP:port.
//
// Note that due to the lack of a cidrlist parameter in UpdateLoadbalancerRule, we can't use
// loadbalancer ACLs to implement the allowedCIDR functionality, and are forced to use firewall
// rules instead. See https://github.com/apache/cloudstack/issues/8382 for details.
func (c *client) CreateLoadBalancerRule(isoNet *infrav1.CloudStackIsolatedNetwork, port int) (string, error) {
	name := fmt.Sprintf("K8s_API_%d", port)
	p := c.cs.LoadBalancer.NewCreateLoadBalancerRuleParams(
		"roundrobin", name, port, port)
	p.SetPublicport(port)
	p.SetNetworkid(isoNet.Spec.ID)

	p.SetPublicipid(isoNet.Status.PublicIPID)
	p.SetProtocol(NetworkProtocolTCP)
	// Do not open the firewall to the world, we'll manage that ourselves (unfortunately).
	p.SetOpenfirewall(false)
	resp, err := c.cs.LoadBalancer.CreateLoadBalancerRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return "", err
	}

	return resp.Id, nil
}

// DeleteLoadBalancerRule deletes an existing load balancer rule.
func (c *client) DeleteLoadBalancerRule(id string) (bool, error) {
	p := c.csAsync.LoadBalancer.NewDeleteLoadBalancerRuleParams(id)
	resp, err := c.csAsync.LoadBalancer.DeleteLoadBalancerRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return false, err
	}

	return resp.Success, nil
}

// GetFirewallRules fetches the current firewall rules for the isolated network.
func (c *client) GetFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork) ([]*cloudstack.FirewallRule, error) {
	p := c.cs.Firewall.NewListFirewallRulesParams()
	p.SetIpaddressid(isoNet.Status.PublicIPID)
	p.SetNetworkid(isoNet.Spec.ID)
	fwRules, err := c.cs.Firewall.ListFirewallRules(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, errors.Wrap(err, "listing firewall rules")
	}

	return fwRules.FirewallRules, nil
}

func (c *client) ReconcileFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error {
	fwr, err := c.GetFirewallRules(isoNet)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrap(err, "retrieving load balancer rules")
	}

	// Create a map for easy lookup of existing rules
	portsAndIDs := make(map[int]string)
	for _, rule := range fwr {
		if rule.Startport == rule.Endport {
			portsAndIDs[rule.Startport] = rule.Id
		}
	}

	ports := []int{int(csCluster.Spec.ControlPlaneEndpoint.Port)}
	if len(csCluster.Spec.APIServerLoadBalancer.AdditionalPorts) > 0 {
		ports = append(ports, csCluster.Spec.APIServerLoadBalancer.AdditionalPorts...)
	}

	// A note on the implementation here:
	// Due to the lack of a `cidrlist` parameter in UpdateFirewallRule, we have to manage
	// firewall rules for every item in the list of allowed CIDRs.
	// See https://github.com/apache/cloudstack/issues/8382
	allowedCIDRS := getCanonicalAllowedCIDRs(isoNet, csCluster)
	for _, port := range ports {
		foundCIDRs := make([]string, 0)
		// Check if fw rule for port already exists
		for _, rule := range fwr {
			if rule.Startport == port && rule.Endport == port {
				// If the port matches and the rule CIDR is not in allowedCIDRs, delete
				if !slices.Contains(allowedCIDRS, rule.Cidrlist) {
					success, err := c.DeleteFirewallRule(rule.Id)
					if err != nil || !success {
						return errors.Wrap(err, "deleting firewall rule")
					}

					continue
				}
				foundCIDRs = append(foundCIDRs, rule.Cidrlist)
			}
		}

		_, createCIDRs := capcstrings.SliceDiff(foundCIDRs, allowedCIDRS)
		for _, cidr := range createCIDRs {
			// create fw rule
			if err := c.CreateFirewallRule(isoNet, port, cidr); err != nil {
				return errors.Wrap(err, "creating firewall rule")
			}
		}
	}

	// Delete any existing rules with a port that is no longer part of ports.
	for port, ruleID := range portsAndIDs {
		if !slices.Contains(ports, port) {
			success, err := c.DeleteFirewallRule(ruleID)
			if err != nil {
				return errors.Wrap(err, "deleting firewall rule")
			}
			if !success {
				return errors.New("delete firewall rule returned unsuccessful")
			}
		}
	}

	// Update the list of allowed CIDRs in the status
	isoNet.Status.APIServerLoadBalancer.AllowedCIDRs = allowedCIDRS

	return nil
}

// CreateFirewallRule creates a firewall rule to allow traffic from a certain CIDR to a port on our public IP.
func (c *client) CreateFirewallRule(isoNet *infrav1.CloudStackIsolatedNetwork, port int, cidr string) error {
	cidrList := []string{cidr}
	p := c.cs.Firewall.NewCreateFirewallRuleParams(isoNet.Status.PublicIPID, NetworkProtocolTCP)
	p.SetStartport(port)
	p.SetEndport(port)
	p.SetCidrlist(cidrList)
	_, err := c.cs.Firewall.CreateFirewallRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	}

	return nil
}

// DeleteFirewallRule deletes a firewall rule.
func (c *client) DeleteFirewallRule(id string) (bool, error) {
	p := c.csAsync.Firewall.NewDeleteFirewallRuleParams(id)
	resp, err := c.csAsync.Firewall.DeleteFirewallRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return false, err
	}

	return resp.Success, nil
}

// getCanonicalAllowedCIDRs gets a filtered list of CIDRs which should be allowed to access the API server loadbalancer.
// Invalid CIDRs are filtered from the list and emil a warning event.
// It returns a canonical representation that can be directly compared with other canonicalized lists.
func getCanonicalAllowedCIDRs(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) []string {
	allowedCIDRs := []string{}

	if csCluster.Spec.APIServerLoadBalancer != nil && len(csCluster.Spec.APIServerLoadBalancer.AllowedCIDRs) > 0 {
		allowedCIDRs = append(allowedCIDRs, csCluster.Spec.APIServerLoadBalancer.AllowedCIDRs...)

		if isoNet.Status.CIDR != "" {
			allowedCIDRs = append(allowedCIDRs, isoNet.Status.CIDR)
		}
	} else {
		// If there are no specific CIDRs defined to allow traffic from, default to allow all.
		allowedCIDRs = append(allowedCIDRs, "0.0.0.0/0")
	}

	// Filter invalid CIDRs and convert any IPs into CIDRs.
	validCIDRs := []string{}
	for _, v := range allowedCIDRs {
		switch {
		case utilsnet.IsIPv4String(v):
			validCIDRs = append(validCIDRs, v+"/32")
		case utilsnet.IsIPv4CIDRString(v):
			validCIDRs = append(validCIDRs, v)
		default:
			record.Warnf(csCluster, "FailedIPAddressValidation", "%s is not a valid IPv4 nor CIDR address and will not get applied to firewall rules", v)
		}
	}

	// Canonicalize by sorting and removing duplicates.
	return capcstrings.Canonicalize(validCIDRs)
}

// GetOrCreateIsolatedNetwork fetches or builds out the necessary structures for isolated network use.
func (c *client) GetOrCreateIsolatedNetwork(
	fd *infrav1.CloudStackFailureDomain,
	isoNet *infrav1.CloudStackIsolatedNetwork,
	csCluster *infrav1.CloudStackCluster,
) error {
	// Get or create the isolated network itself and resolve details into passed custom resources.
	net := isoNet.Network()
	if err := c.ResolveNetwork(net); err != nil { // Doesn't exist, create isolated network.
		if err = c.CreateIsolatedNetwork(fd, isoNet); err != nil {
			return errors.Wrap(err, "creating a new isolated network")
		}
	} else { // Network existed and was resolved. Set ID on isoNet CloudStackIsolatedNetwork in case it only had name set.
		isoNet.Spec.ID = net.ID
	}

	// Tag the created network.
	networkID := isoNet.Spec.ID
	if err := c.AddClusterTag(ResourceTypeNetwork, networkID, csCluster); err != nil {
		return errors.Wrapf(err, "tagging network with id %s", networkID)
	}

	if !annotations.IsExternallyManaged(csCluster) {
		// Check/set ControlPlaneEndpoint port.
		// Prefer csCluster ControlPlaneEndpoint port. Use isonet port if CP missing. Set to default if both missing.
		if csCluster.Spec.ControlPlaneEndpoint.Port != 0 {
			isoNet.Spec.ControlPlaneEndpoint.Port = csCluster.Spec.ControlPlaneEndpoint.Port
		} else if isoNet.Spec.ControlPlaneEndpoint.Port != 0 { // Override default public port if endpoint port specified.
			csCluster.Spec.ControlPlaneEndpoint.Port = isoNet.Spec.ControlPlaneEndpoint.Port
		} else {
			csCluster.Spec.ControlPlaneEndpoint.Port = 6443
			isoNet.Spec.ControlPlaneEndpoint.Port = 6443
		}

		// Set up a load balancing rules to map VM ports to Public IP ports.
		if csCluster.Spec.APIServerLoadBalancer.IsEnabled() {
			// Associate Public IP with CloudStackIsolatedNetwork
			if err := c.AssociatePublicIPAddress(fd, isoNet, csCluster); err != nil {
				return errors.Wrapf(err, "associating public IP address to csCluster")
			}

			if err := c.ReconcileLoadBalancerRules(isoNet, csCluster); err != nil {
				return errors.Wrap(err, "reconciling load balancing rules")
			}

			if err := c.ReconcileFirewallRules(isoNet, csCluster); err != nil {
				return errors.Wrap(err, "reconciling firewall rules")
			}
		}
	}

	// Open the Isolated Network egress firewall.
	return errors.Wrap(c.CreateEgressFirewallRules(isoNet), "opening the isolated network's egress firewall")
}

// AssignVMToLoadBalancerRules assigns a VM instance to load balancing rules (specifying lb membership).
func (c *client) AssignVMToLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, instanceID string) error {
	var found bool
	for _, lbRuleID := range isoNet.Status.LoadBalancerRuleIDs {
		// Check that the instance isn't already in LB rotation.
		found = false
		lbRuleInstances, err := c.cs.LoadBalancer.ListLoadBalancerRuleInstances(
			c.cs.LoadBalancer.NewListLoadBalancerRuleInstancesParams(lbRuleID))
		if err != nil {
			c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
			return err
		}
		for _, instance := range lbRuleInstances.LoadBalancerRuleInstances {
			if instance.Id == instanceID { // Already assigned to load balancer..
				found = true
			}
		}

		if !found {
			// Assign to Load Balancer.
			p := c.cs.LoadBalancer.NewAssignToLoadBalancerRuleParams(lbRuleID)
			p.SetVirtualmachineids([]string{instanceID})
			if _, err = c.cs.LoadBalancer.AssignToLoadBalancerRule(p); err != nil {
				c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
				return err
			}
		}
	}

	return nil
}

// DeleteNetwork deletes an isolated network.
func (c *client) DeleteNetwork(net infrav1.Network) error {
	_, err := c.cs.Network.DeleteNetwork(c.cs.Network.NewDeleteNetworkParams(net.ID))
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
	return errors.Wrapf(err, "deleting network with id %s", net.ID)
}

// DisposeIsoNetResources cleans up isolated network resources.
func (c *client) DisposeIsoNetResources(
	isoNet *infrav1.CloudStackIsolatedNetwork,
	csCluster *infrav1.CloudStackCluster,
) (retError error) {
	if isoNet.Status.PublicIPID != "" {
		if err := c.DeleteClusterTag(ResourceTypeIPAddress, isoNet.Status.PublicIPID, csCluster); err != nil {
			return err
		}
		if err := c.DisassociatePublicIPAddressIfNotInUse(isoNet); err != nil {
			return err
		}
	}
	if err := c.RemoveClusterTagFromNetwork(csCluster, *isoNet.Network()); err != nil {
		return err
	}

	return c.DeleteNetworkIfNotInUse(*isoNet.Network())
}

// DeleteNetworkIfNotInUse deletes an isolated network if the network is no longer in use (indicated by in use tags).
func (c *client) DeleteNetworkIfNotInUse(net infrav1.Network) (retError error) {
	tags, err := c.GetTags(ResourceTypeNetwork, net.ID)
	if err != nil {
		return err
	}

	var clusterTagCount int
	for tagName := range tags {
		if strings.HasPrefix(tagName, ClusterTagNamePrefix) {
			clusterTagCount++
		}
	}

	if clusterTagCount == 0 && tags[CreatedByCAPCTagName] != "" {
		return c.DeleteNetwork(net)
	}

	return nil
}

// DisassociatePublicIPAddressIfNotInUse removes a CloudStack public IP association from passed isolated network
// if it is no longer in use (indicated by in use tags).
func (c *client) DisassociatePublicIPAddressIfNotInUse(isoNet *infrav1.CloudStackIsolatedNetwork) (retError error) {
	if tagsAllowDisposal, err := c.DoClusterTagsAllowDisposal(ResourceTypeIPAddress, isoNet.Status.PublicIPID); err != nil {
		return err
	} else if publicIP, _, err := c.cs.Address.GetPublicIpAddressByID(isoNet.Status.PublicIPID); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return err
	} else if publicIP == nil || publicIP.Issourcenat { // Can't disassociate an address if it's the source NAT address.
		return nil
	} else if tagsAllowDisposal {
		return c.DisassociatePublicIPAddress(isoNet)
	}
	return nil
}

// DisassociatePublicIPAddress removes a CloudStack public IP association from passed isolated network.
func (c *client) DisassociatePublicIPAddress(isoNet *infrav1.CloudStackIsolatedNetwork) (retErr error) {
	// Remove the CAPC creation tag, so it won't be there the next time this address is associated.
	retErr = c.DeleteCreatedByCAPCTag(ResourceTypeIPAddress, isoNet.Status.PublicIPID)
	if retErr != nil {
		return retErr
	}

	p := c.cs.Address.NewDisassociateIpAddressParams(isoNet.Status.PublicIPID)
	_, retErr = c.cs.Address.DisassociateIpAddress(p)
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(retErr)
	return retErr
}
