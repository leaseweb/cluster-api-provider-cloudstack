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

	"github.com/hashicorp/go-multierror"
	"github.com/pkg/errors"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

type TagIface interface {
	AddClusterTag(rType ResourceType, rID string, csCluster *infrav1.CloudStackCluster) error
	DeleteClusterTag(rType ResourceType, rID string, csCluster *infrav1.CloudStackCluster) error
	AddCreatedByCAPCTag(rType ResourceType, rID string) error
	DeleteCreatedByCAPCTag(rType ResourceType, rID string) error
	DoClusterTagsAllowDisposal(rType ResourceType, rID string) (bool, error)
	AddTags(rType ResourceType, rID string, tags map[string]string) error
	GetTags(rType ResourceType, rID string) (map[string]string, error)
	DeleteTags(rType ResourceType, rID string, tagsToDelete map[string]string) error
}

type ResourceType string

const (
	ClusterTagNamePrefix                      = "CAPC_cluster_"
	CreatedByCAPCTagName                      = "created_by_CAPC"
	ResourceTypeNetwork          ResourceType = "Network"
	ResourceTypeIPAddress        ResourceType = "PublicIpAddress"
	ResourceTypeLoadBalancerRule ResourceType = "LoadBalancer"
	ResourceTypeFirewallRule     ResourceType = "FirewallRule"
)

// ignoreAlreadyPresentErrors returns nil if the error is an already present tag error.
func ignoreAlreadyPresentErrors(err error, rType ResourceType, rID string) error {
	matchSubString := strings.ToLower("already on " + string(rType) + " with id " + rID)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), matchSubString) {
		return err
	}

	return nil
}

// IsCapcManaged checks whether the resource has the CreatedByCAPCTag.
func (c *client) IsCapcManaged(resourceType ResourceType, resourceID string) (bool, error) {
	tags, err := c.GetTags(resourceType, resourceID)
	if err != nil {
		return false, errors.Wrapf(err,
			"checking if %s with ID: %s is tagged as CAPC managed", resourceType, resourceID)
	}
	_, CreatedByCAPC := tags[CreatedByCAPCTagName]

	return CreatedByCAPC, nil
}

// AddClusterTag adds cluster tag to a resource. This tag indicates the resource is used by a given the cluster.
func (c *client) AddClusterTag(rType ResourceType, rID string, csCluster *infrav1.CloudStackCluster) error {
	if managedByCAPC, err := c.IsCapcManaged(rType, rID); err != nil {
		return err
	} else if managedByCAPC {
		ClusterTagName := generateClusterTagName(csCluster)

		return c.AddTags(rType, rID, map[string]string{ClusterTagName: "1"})
	}

	return nil
}

// DeleteClusterTag deletes the tag that associates the resource with a given cluster.
func (c *client) DeleteClusterTag(rType ResourceType, rID string, csCluster *infrav1.CloudStackCluster) error {
	if managedByCAPC, err := c.IsCapcManaged(rType, rID); err != nil {
		return err
	} else if managedByCAPC {
		ClusterTagName := generateClusterTagName(csCluster)

		return c.DeleteTags(rType, rID, map[string]string{ClusterTagName: "1"})
	}

	return nil
}

// AddCreatedByCAPCTag adds the tag that indicates that the resource was created by CAPC.
// This is useful when a resource is disassociated but not deleted.
func (c *client) AddCreatedByCAPCTag(rType ResourceType, rID string) error {
	return c.AddTags(rType, rID, map[string]string{CreatedByCAPCTagName: "1"})
}

// DeleteCreatedByCAPCTag deletes the tag that indicates that the resource was created by CAPC.
func (c *client) DeleteCreatedByCAPCTag(rType ResourceType, rID string) error {
	return c.DeleteTags(rType, rID, map[string]string{CreatedByCAPCTagName: "1"})
}

// DoClusterTagsAllowDisposal checks to see if the resource is in a state that makes it eligible for disposal.  CAPC can
// dispose of a resource if the tags show it was created by CAPC and isn't being used by any clusters.
func (c *client) DoClusterTagsAllowDisposal(rType ResourceType, rID string) (bool, error) {
	tags, err := c.GetTags(rType, rID)
	if err != nil {
		return false, err
	}

	var clusterTagCount int
	for tagName := range tags {
		if strings.HasPrefix(tagName, ClusterTagNamePrefix) {
			clusterTagCount++
		}
	}

	return clusterTagCount == 0 && tags[CreatedByCAPCTagName] != "", nil
}

// AddTags adds arbitrary tags to a resource.
func (c *client) AddTags(rType ResourceType, rID string, tags map[string]string) error {
	p := c.cs.Resourcetags.NewCreateTagsParams([]string{rID}, string(rType), tags)
	_, err := c.cs.Resourcetags.CreateTags(p)
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

	return ignoreAlreadyPresentErrors(err, rType, rID)
}

// GetTags gets all of a resource's tags.
func (c *client) GetTags(rType ResourceType, rID string) (map[string]string, error) {
	p := c.cs.Resourcetags.NewListTagsParams()
	p.SetResourceid(rID)
	p.SetResourcetype(string(rType))
	p.SetListall(true)
	setIfNotEmpty(c.user.Project.ID, p.SetProjectid)
	listTagResponse, err := c.cs.Resourcetags.ListTags(p)
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return nil, err
	}
	tags := make(map[string]string, listTagResponse.Count)
	for _, t := range listTagResponse.Tags {
		tags[t.Key] = t.Value
	}

	return tags, nil
}

// DeleteTags deletes the given tags from a resource.
// Ignores errors if the tag is not present.
func (c *client) DeleteTags(rType ResourceType, rID string, tagsToDelete map[string]string) error {
	for tagkey, tagval := range tagsToDelete {
		p := c.cs.Resourcetags.NewDeleteTagsParams([]string{rID}, string(rType))
		p.SetTags(tagsToDelete)
		if _, err1 := c.cs.Resourcetags.DeleteTags(p); err1 != nil { // Error in deletion attempt. Check for tag.
			c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err1)
			currTag := map[string]string{tagkey: tagval}
			if tags, err2 := c.GetTags(rType, rID); len(tags) != 0 {
				c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err2)
				if _, foundTag := tags[tagkey]; foundTag {
					return errors.Wrapf(multierror.Append(err1, err2),
						"could not remove tag %s from %s with ID %s", currTag, rType, rID)
				}
			}
		}
	}

	return nil
}

func generateClusterTagName(csCluster *infrav1.CloudStackCluster) string {
	return ClusterTagNamePrefix + string(csCluster.UID)
}
