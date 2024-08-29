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
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"
	utilsnet "k8s.io/utils/net"
	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	capcstrings "sigs.k8s.io/cluster-api-provider-cloudstack/pkg/utils/strings"
	"sigs.k8s.io/cluster-api/util/record"
)

type IsoNetworkIface interface {
	GetOrCreateIsolatedNetwork(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackIsolatedNetwork, *infrav1.CloudStackCluster) error
	ReconcileLoadBalancer(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackIsolatedNetwork, *infrav1.CloudStackCluster) error

	AssociatePublicIPAddress(*infrav1.CloudStackFailureDomain, *infrav1.CloudStackIsolatedNetwork, string) (*cloudstack.PublicIpAddress, error)
	CreateEgressFirewallRules(*infrav1.CloudStackIsolatedNetwork) error
	GetPublicIP(*infrav1.CloudStackFailureDomain, string) (*cloudstack.PublicIpAddress, error)
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

// AssociatePublicIPAddress gets a public IP and associates it to the isolated network.
func (c *client) AssociatePublicIPAddress(
	fd *infrav1.CloudStackFailureDomain,
	isoNet *infrav1.CloudStackIsolatedNetwork,
	desiredIP string,
) (*cloudstack.PublicIpAddress, error) {
	// Check specified IP address is available or get an unused one if not specified.
	publicAddress, err := c.GetPublicIP(fd, desiredIP)
	if err != nil {
		return nil, errors.Wrap(err, "fetching a public IP address")
	}

	// Check if the address is already associated with the network.
	if publicAddress.Associatednetworkid == isoNet.Spec.ID {
		return publicAddress, nil
	}

	// Public IP found, but not yet associated with network -- associate it.
	p := c.cs.Address.NewAssociateIpAddressParams()
	p.SetIpaddress(publicAddress.Ipaddress)
	p.SetNetworkid(isoNet.Spec.ID)
	if _, err := c.cs.Address.AssociateIpAddress(p); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, errors.Wrapf(err,
			"associating public IP address with ID %s to network with ID %s",
			publicAddress.Id, isoNet.Spec.ID)
	} else if err := c.AddCreatedByCAPCTag(ResourceTypeIPAddress, publicAddress.Id); err != nil {
		return nil, errors.Wrapf(err,
			"adding tag to public IP address with ID %s", publicAddress.Id)
	}

	return publicAddress, nil
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
	if isoNet.Spec.CIDR != "" {
		m, err := parseCIDR(isoNet.Spec.CIDR)
		if err != nil {
			return errors.Wrap(err, "parsing CIDR")
		}
		// Set the needed IP subnet config
		p.SetGateway(m["gateway"])
		p.SetNetmask(m["netmask"])
		p.SetStartip(m["startip"])
		p.SetEndip(m["endip"])
	}
	resp, err := c.cs.Network.CreateNetwork(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrapf(err, "creating network with name %s", isoNet.Spec.Name)
	}
	isoNet.Spec.ID = resp.Id
	isoNet.Spec.CIDR = resp.Cidr

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

// GetPublicIP gets a public IP. If desiredIP is empty, it will pick the next available IP.
func (c *client) GetPublicIP(
	fd *infrav1.CloudStackFailureDomain,
	desiredIP string,
) (*cloudstack.PublicIpAddress, error) {
	p := c.cs.Address.NewListPublicIpAddressesParams()
	p.SetAllocatedonly(false)
	p.SetZoneid(fd.Spec.Zone.ID)
	setIfNotEmpty(desiredIP, p.SetIpaddress)
	publicAddresses, err := c.cs.Address.ListPublicIpAddresses(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, err
	} else if desiredIP != "" && publicAddresses.Count == 1 {
		// Desired IP specified and IP found.
		// Ignore already allocated here since the IP was specified.
		return publicAddresses.PublicIpAddresses[0], nil
	} else if publicAddresses.Count > 0 {
		// Desired IP not specified. Pick first available address.
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
		isoNet.Spec.CIDR = netDetails.Cidr
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
	isoNet.Spec.CIDR = netDetails.Cidr
	return nil
}

// GetLoadBalancerRules fetches the current loadbalancer rules for the isolated network.
func (c *client) GetLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork) ([]*cloudstack.LoadBalancerRule, error) {
	p := c.cs.LoadBalancer.NewListLoadBalancerRulesParams()
	p.SetPublicipid(isoNet.Status.APIServerLoadBalancer.IPAddressID)
	loadBalancerRules, err := c.cs.LoadBalancer.ListLoadBalancerRules(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, errors.Wrap(err, "listing load balancer rules")
	}

	return loadBalancerRules.LoadBalancerRules, nil
}

// ReconcileLoadBalancerRules manages the loadbalancer rules for all ports.
func (c *client) ReconcileLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error {
	// If there is no public IP address associated with the load balancer, do nothing.
	if isoNet.Status.APIServerLoadBalancer.IPAddressID == "" {
		return nil
	}

	lbr, err := c.GetLoadBalancerRules(isoNet)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrap(err, "retrieving load balancer rules")
	}

	portsAndIDs := mapExistingLoadBalancerRules(lbr)

	if csCluster.Spec.APIServerLoadBalancer.IsEnabled() {
		// Load balancer enabled, reconcile the rules.
		ports := gatherPorts(csCluster)

		lbRuleIDs, err := c.ensureLoadBalancerRules(isoNet, ports, portsAndIDs, csCluster)
		if err != nil {
			return err
		}

		if err := c.cleanupObsoleteLoadBalancerRules(portsAndIDs, ports); err != nil {
			return err
		}

		if len(lbRuleIDs) > 1 {
			capcstrings.Canonicalize(lbRuleIDs)
		}

		isoNet.Status.LoadBalancerRuleIDs = lbRuleIDs
	} else {
		// Load balancer disabled, delete all rules.
		if err := c.cleanupAllLoadBalancerRules(portsAndIDs); err != nil {
			return err
		}

		isoNet.Status.LoadBalancerRuleIDs = []string{}
	}

	return nil
}

// mapExistingLoadBalancerRules creates a lookup map for existing load balancer rules based on their public port.
func mapExistingLoadBalancerRules(lbr []*cloudstack.LoadBalancerRule) map[string]string {
	portsAndIDs := make(map[string]string)
	for _, rule := range lbr {
		// Check if the rule is managed by CAPC.
		capcManaged := false
		for _, t := range rule.Tags {
			if t.Key == CreatedByCAPCTagName && t.Value == "1" {
				capcManaged = true

				break
			}
		}
		if capcManaged {
			portsAndIDs[rule.Publicport] = rule.Id
		}
	}

	return portsAndIDs
}

// ensureLoadBalancerRules ensures that the necessary load balancer rules are in place.
func (c *client) ensureLoadBalancerRules(isoNet *infrav1.CloudStackIsolatedNetwork, ports []int, portsAndIDs map[string]string, csCluster *infrav1.CloudStackCluster) ([]string, error) {
	lbRuleIDs := make([]string, 0)
	for _, port := range ports {
		ruleID, err := c.getOrCreateLoadBalancerRule(isoNet, port, portsAndIDs)
		if err != nil {
			return nil, err
		}
		lbRuleIDs = append(lbRuleIDs, ruleID)

		// For backwards compatibility.
		if port == int(csCluster.Spec.ControlPlaneEndpoint.Port) {
			isoNet.Status.LBRuleID = ruleID
		}
	}
	return lbRuleIDs, nil
}

// getOrCreateLoadBalancerRule retrieves or creates a load balancer rule for a given port.
func (c *client) getOrCreateLoadBalancerRule(isoNet *infrav1.CloudStackIsolatedNetwork, port int, portsAndIDs map[string]string) (string, error) {
	portStr := strconv.Itoa(port)
	ruleID, found := portsAndIDs[portStr]
	if found {
		return ruleID, nil
	}
	// If not found, create the lb rule for port
	ruleID, err := c.CreateLoadBalancerRule(isoNet, port)
	if err != nil {
		return "", errors.Wrap(err, "creating load balancer rule")
	}
	return ruleID, nil
}

// cleanupObsoleteLoadBalancerRules deletes load balancer rules that are no longer needed.
func (c *client) cleanupObsoleteLoadBalancerRules(portsAndIDs map[string]string, ports []int) error {
	for port, ruleID := range portsAndIDs {
		intPort, err := strconv.Atoi(port)
		if err != nil {
			return errors.Wrap(err, "converting port to int")
		}
		if !slices.Contains(ports, intPort) {
			if err := c.deleteLoadBalancerRuleByID(ruleID); err != nil {
				return err
			}
		}
	}
	return nil
}

// cleanupAllLoadBalancerRules deletes all load balancer rules created by CAPC.
func (c *client) cleanupAllLoadBalancerRules(portsAndIDs map[string]string) error {
	for _, ruleID := range portsAndIDs {
		if err := c.deleteLoadBalancerRuleByID(ruleID); err != nil {
			return err
		}
	}

	return nil
}

// deleteLoadBalancerRuleByID wraps the deletion logic with error handling.
func (c *client) deleteLoadBalancerRuleByID(ruleID string) error {
	success, err := c.DeleteLoadBalancerRule(ruleID)
	if err != nil {
		return errors.Wrap(err, "deleting load balancer rule")
	}
	if !success {
		return errors.New("delete load balancer rule returned unsuccessful")
	}
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

	p.SetPublicipid(isoNet.Status.APIServerLoadBalancer.IPAddressID)
	p.SetProtocol(NetworkProtocolTCP)
	// Do not open the firewall to the world, we'll manage that ourselves (unfortunately).
	p.SetOpenfirewall(false)
	resp, err := c.cs.LoadBalancer.CreateLoadBalancerRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return "", err
	}
	if err := c.AddCreatedByCAPCTag(ResourceTypeLoadBalancerRule, resp.Id); err != nil {
		return "", errors.Wrap(err, "adding created by CAPC tag")
	}

	return resp.Id, nil
}

// DeleteLoadBalancerRule deletes an existing load balancer rule.
func (c *client) DeleteLoadBalancerRule(id string) (bool, error) {
	isCAPCManaged, err := c.IsCapcManaged(ResourceTypeLoadBalancerRule, id)
	if err != nil {
		return false, err
	}

	if !isCAPCManaged {
		return false, errors.Errorf("load balancer rule with id %s is not managed by CAPC", id)
	}

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
	p.SetIpaddressid(isoNet.Status.APIServerLoadBalancer.IPAddressID)
	p.SetNetworkid(isoNet.Spec.ID)
	fwRules, err := c.cs.Firewall.ListFirewallRules(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return nil, errors.Wrap(err, "listing firewall rules")
	}

	return fwRules.FirewallRules, nil
}

// ReconcileFirewallRules manages the firewall rules for all port <-> allowedCIDR combinations.
func (c *client) ReconcileFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork, csCluster *infrav1.CloudStackCluster) error {
	// If there is no public IP address associated with the load balancer, do nothing.
	if isoNet.Status.APIServerLoadBalancer.IPAddressID == "" {
		return nil
	}

	fwr, err := c.GetFirewallRules(isoNet)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return errors.Wrap(err, "retrieving firewall rules")
	}

	portsAndIDs := mapExistingFirewallRules(fwr)

	if csCluster.Spec.APIServerLoadBalancer.IsEnabled() {
		// Load balancer enabled, reconcile firewall rules.
		ports := gatherPorts(csCluster)
		allowedCIDRS := getCanonicalAllowedCIDRs(isoNet, csCluster)

		// A note on the implementation here:
		// Due to the lack of a `cidrlist` parameter in UpdateFirewallRule, we have to manage
		// firewall rules for every item in the list of allowed CIDRs.
		// See https://github.com/apache/cloudstack/issues/8382
		if err := c.reconcileFirewallRulesForPorts(isoNet, fwr, ports, allowedCIDRS); err != nil {
			return err
		}

		if err := c.cleanupObsoleteFirewallRules(portsAndIDs, ports); err != nil {
			return err
		}

		// Update the list of allowed CIDRs in the status
		isoNet.Status.APIServerLoadBalancer.AllowedCIDRs = allowedCIDRS
	} else {
		// Load balancer disabled, remove all firewall rules.
		if err := c.cleanupAllFirewallRules(portsAndIDs); err != nil {
			return err
		}

		isoNet.Status.APIServerLoadBalancer.AllowedCIDRs = []string{}
	}

	return nil
}

// gatherPorts collects all the ports that need firewall or load balancer rules.
func gatherPorts(csCluster *infrav1.CloudStackCluster) []int {
	ports := []int{int(csCluster.Spec.ControlPlaneEndpoint.Port)}
	if len(csCluster.Spec.APIServerLoadBalancer.AdditionalPorts) > 0 {
		ports = append(ports, csCluster.Spec.APIServerLoadBalancer.AdditionalPorts...)
	}

	return ports
}

// mapExistingFirewallRules creates a lookup map for existing firewall rules based on their port.
func mapExistingFirewallRules(fwr []*cloudstack.FirewallRule) map[int][]string {
	portsAndIDs := make(map[int][]string)
	for _, rule := range fwr {
		// Check if the rule is managed by CAPC.
		capcManaged := false
		for _, t := range rule.Tags {
			if t.Key == CreatedByCAPCTagName && t.Value == "1" {
				capcManaged = true

				break
			}
		}
		if capcManaged && rule.Startport == rule.Endport {
			portsAndIDs[rule.Startport] = append(portsAndIDs[rule.Startport], rule.Id)
		}
	}

	return portsAndIDs
}

// reconcileFirewallRulesForPorts ensures the correct firewall rules exist for the given ports and CIDRs.
func (c *client) reconcileFirewallRulesForPorts(isoNet *infrav1.CloudStackIsolatedNetwork, fwr []*cloudstack.FirewallRule, ports []int, allowedCIDRS []string) error {
	for _, port := range ports {
		foundCIDRs := findExistingFirewallCIDRs(fwr, port)
		if err := c.deleteUnwantedFirewallRules(fwr, port, allowedCIDRS); err != nil {
			return err
		}

		if err := c.createMissingFirewallRules(isoNet, port, allowedCIDRS, foundCIDRs); err != nil {
			return err
		}
	}

	return nil
}

// findExistingFirewallCIDRs finds existing CIDRs for a specific port in the current firewall ruleset.
func findExistingFirewallCIDRs(fwr []*cloudstack.FirewallRule, port int) []string {
	foundCIDRs := make([]string, 0)
	for _, rule := range fwr {
		if rule.Startport == port && rule.Endport == port {
			foundCIDRs = append(foundCIDRs, rule.Cidrlist)
		}
	}

	return foundCIDRs
}

// deleteUnwantedFirewallRules deletes firewall rules that should no longer exist.
func (c *client) deleteUnwantedFirewallRules(fwr []*cloudstack.FirewallRule, port int, allowedCIDRS []string) error {
	for _, rule := range fwr {
		if rule.Startport == port && rule.Endport == port && !slices.Contains(allowedCIDRS, rule.Cidrlist) {
			if err := c.deleteFirewallRuleByID(rule.Id); err != nil {
				return err
			}
		}
	}

	return nil
}

// createMissingFirewallRules creates any firewall rules that are missing.
func (c *client) createMissingFirewallRules(isoNet *infrav1.CloudStackIsolatedNetwork, port int, allowedCIDRS, foundCIDRs []string) error {
	_, createCIDRs := capcstrings.SliceDiff(foundCIDRs, allowedCIDRS)
	for _, cidr := range createCIDRs {
		if err := c.CreateFirewallRule(isoNet, port, cidr); err != nil {
			return errors.Wrap(err, "creating firewall rule")
		}
	}

	return nil
}

// cleanupObsoleteFirewallRules deletes firewall rules that are no longer needed.
func (c *client) cleanupObsoleteFirewallRules(portsAndIDs map[int][]string, ports []int) error {
	for port, ruleIDs := range portsAndIDs {
		if !slices.Contains(ports, port) {
			for _, ruleID := range ruleIDs {
				if err := c.deleteFirewallRuleByID(ruleID); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// cleanupAllFirewallRules deletes all firewall rules created by CAPC.
func (c *client) cleanupAllFirewallRules(portsAndIDs map[int][]string) error {
	for _, ruleIDs := range portsAndIDs {
		for _, ruleID := range ruleIDs {
			if err := c.deleteFirewallRuleByID(ruleID); err != nil {
				return err
			}
		}
	}

	return nil
}

// deleteFirewallRuleByID wraps the firewall rule deletion logic with error handling.
func (c *client) deleteFirewallRuleByID(ruleID string) error {
	success, err := c.DeleteFirewallRule(ruleID)
	if err != nil {
		return errors.Wrap(err, "deleting firewall rule")
	}
	if !success {
		return errors.New("delete firewall rule returned unsuccessful")
	}

	return nil
}

// CreateFirewallRule creates a firewall rule to allow traffic from a certain CIDR to a port on our public IP.
func (c *client) CreateFirewallRule(isoNet *infrav1.CloudStackIsolatedNetwork, port int, cidr string) error {
	cidrList := []string{cidr}
	p := c.cs.Firewall.NewCreateFirewallRuleParams(isoNet.Status.APIServerLoadBalancer.IPAddressID, NetworkProtocolTCP)
	p.SetStartport(port)
	p.SetEndport(port)
	p.SetCidrlist(cidrList)
	resp, err := c.cs.Firewall.CreateFirewallRule(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	}
	if err := c.AddCreatedByCAPCTag(ResourceTypeFirewallRule, resp.Id); err != nil {
		return errors.Wrap(err, "adding created by CAPC tag")
	}

	return nil
}

// DeleteFirewallRule deletes a firewall rule.
func (c *client) DeleteFirewallRule(id string) (bool, error) {
	isCAPCManaged, err := c.IsCapcManaged(ResourceTypeFirewallRule, id)
	if err != nil {
		return false, err
	}

	if !isCAPCManaged {
		return false, errors.Errorf("firewall rule with id %s is not managed by CAPC", id)
	}

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

		// Add our own outgoing IP
		if len(isoNet.Status.PublicIPAddress) > 0 {
			allowedCIDRs = append(allowedCIDRs, isoNet.Status.PublicIPAddress)
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
	network := isoNet.Network()
	if err := c.ResolveNetwork(network); err != nil { // Doesn't exist, create isolated network.
		if err = c.CreateIsolatedNetwork(fd, isoNet); err != nil {
			return errors.Wrap(err, "creating a new isolated network")
		}
	} else {
		// Network existed and was resolved. Set ID on isoNet CloudStackIsolatedNetwork in case it only had name set.
		isoNet.Spec.ID = network.ID
		isoNet.Spec.CIDR = network.CIDR
	}

	// Open the Isolated Network egress firewall.
	return errors.Wrap(c.CreateEgressFirewallRules(isoNet), "opening the isolated network's egress firewall")
}

// ReconcileLoadBalancer configures the API server load balancer.
func (c *client) ReconcileLoadBalancer(
	fd *infrav1.CloudStackFailureDomain,
	isoNet *infrav1.CloudStackIsolatedNetwork,
	csCluster *infrav1.CloudStackCluster,
) error {
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

	// Associate public IP with the load balancer if enabled.
	/* TODO: implement possibility for load balancer to use a different IP than isonet
	if csCluster.Spec.APIServerLoadBalancer.IsEnabled() {
		// Associate Public IP with CloudStackIsolatedNetwork
		if err := c.AssociatePublicIPAddress(fd, isoNet, csCluster); err != nil {
			return errors.Wrapf(err, "associating public IP address to csCluster")
		}
	}*/

	// Set up load balancing rules to map VM ports to Public IP ports.
	if err := c.ReconcileLoadBalancerRules(isoNet, csCluster); err != nil {
		return errors.Wrap(err, "reconciling load balancing rules")
	}

	// Set up firewall rules to manage access to load balancer public IP ports.
	if err := c.ReconcileFirewallRules(isoNet, csCluster); err != nil {
		return errors.Wrap(err, "reconciling firewall rules")
	}

	if !csCluster.Spec.APIServerLoadBalancer.IsEnabled() && isoNet.Status.APIServerLoadBalancer != nil {
		// If the APIServerLoadBalancer has been disabled, release its IP unless it's the SNAT IP.
		released, err := c.DisassociatePublicIPAddressIfNotInUse(isoNet.Status.APIServerLoadBalancer.IPAddressID)
		if err != nil {
			return errors.Wrap(err, "disassociating public IP address")
		}
		if released {
			isoNet.Status.APIServerLoadBalancer.IPAddress = ""
			isoNet.Status.APIServerLoadBalancer.IPAddressID = ""
		}

		// Clear the load balancer status as it is disabled.
		isoNet.Status.APIServerLoadBalancer = nil
	}

	return nil
}

// AssignVMToLoadBalancerRules assigns a VM to the load balancing rules listed in isoNet.Status.LoadBalancerRuleIDs,
// if not already assigned.
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
			if instance.Id == instanceID { // Already assigned to load balancer.
				found = true

				break
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
) error {
	// Release the load balancer IP, if the load balancer is enabled and its IP is different from the isonet public IP.
	if csCluster.Spec.APIServerLoadBalancer.IsEnabled() && isoNet.Status.APIServerLoadBalancer.IPAddressID != "" &&
		isoNet.Status.APIServerLoadBalancer.IPAddressID != isoNet.Status.PublicIPID {
		if err := c.DeleteClusterTag(ResourceTypeIPAddress, isoNet.Status.APIServerLoadBalancer.IPAddressID, csCluster); err != nil {
			return err
		}
		if _, err := c.DisassociatePublicIPAddressIfNotInUse(isoNet.Status.APIServerLoadBalancer.IPAddressID); err != nil {
			return err
		}
	}

	// Release the isolated network public IP.
	if isoNet.Status.PublicIPID != "" {
		if err := c.DeleteClusterTag(ResourceTypeIPAddress, isoNet.Status.PublicIPID, csCluster); err != nil {
			return err
		}
		if _, err := c.DisassociatePublicIPAddressIfNotInUse(isoNet.Status.PublicIPID); err != nil {
			return err
		}
	}

	// Remove this cluster's tag from the isolated network.
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

// DisassociatePublicIPAddressIfNotInUse removes a CloudStack public IP association from an isolated network
// if it is no longer in use (indicated by in use tags). It returns a bool indicating whether an IP was actually
// disassociated, and an error in case an error occurred.
func (c *client) DisassociatePublicIPAddressIfNotInUse(ipAddressID string) (bool, error) {
	if ipAddressID == "" {
		return false, errors.New("ipAddressID cannot be empty")
	}
	if tagsAllowDisposal, err := c.DoClusterTagsAllowDisposal(ResourceTypeIPAddress, ipAddressID); err != nil {
		return false, err
	} else if publicIP, _, err := c.cs.Address.GetPublicIpAddressByID(ipAddressID); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)
		return false, err
	} else if publicIP == nil || publicIP.Issourcenat { // Can't disassociate an address if it's the source NAT address.
		return false, nil
	} else if tagsAllowDisposal {
		if err := c.DisassociatePublicIPAddress(ipAddressID); err != nil {
			return false, err
		}

		return true, nil
	}

	return false, nil
}

// DisassociatePublicIPAddress removes a CloudStack public IP association an isolated network.
func (c *client) DisassociatePublicIPAddress(ipAddressID string) error {
	if ipAddressID == "" {
		return errors.New("ipAddressID cannot be empty")
	}

	// Remove the CAPC creation tag, so it won't be there the next time this address is associated.
	err := c.DeleteCreatedByCAPCTag(ResourceTypeIPAddress, ipAddressID)
	if err != nil {
		return err
	}

	p := c.cs.Address.NewDisassociateIpAddressParams(ipAddressID)
	_, err = c.cs.Address.DisassociateIpAddress(p)
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

	return err
}

// parseCIDR parses a CIDR-formatted string into the components required for CreateNetwork.
func parseCIDR(cidr string) (map[string]string, error) {
	m := make(map[string]string, 4)

	ip, ipnet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, fmt.Errorf("unable to parse cidr %s: %s", cidr, err)
	}

	msk := ipnet.Mask
	sub := ip.Mask(msk)

	m["netmask"] = fmt.Sprintf("%d.%d.%d.%d", msk[0], msk[1], msk[2], msk[3])
	m["gateway"] = fmt.Sprintf("%d.%d.%d.%d", sub[0], sub[1], sub[2], sub[3]+1)
	m["startip"] = fmt.Sprintf("%d.%d.%d.%d", sub[0], sub[1], sub[2], sub[3]+2)
	m["endip"] = fmt.Sprintf("%d.%d.%d.%d",
		sub[0]+(0xff-msk[0]), sub[1]+(0xff-msk[1]), sub[2]+(0xff-msk[2]), sub[3]+(0xff-msk[3]-1))

	return m, nil
}
