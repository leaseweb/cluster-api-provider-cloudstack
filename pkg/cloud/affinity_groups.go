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
	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/pkg/errors"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

const (
	AntiAffinityGroupType     = "host anti-affinity"
	AffinityGroupType         = "host affinity"
	SoftAntiAffinityGroupType = "non-strict anti-affinity"
	SoftAffinityGroupType     = "non-strict host affinity"
)

type AffinityGroup struct {
	Type string
	Name string
	ID   string
}

type AffinityGroupIface interface {
	FetchAffinityGroup(group *AffinityGroup) error
	GetOrCreateAffinityGroup(group *AffinityGroup) error
	DeleteAffinityGroup(group *AffinityGroup) error
	AssociateAffinityGroup(csMachine *infrav1.CloudStackMachine, group AffinityGroup) error
	DisassociateAffinityGroup(csMachine *infrav1.CloudStackMachine, group AffinityGroup) error
}

func (c *client) FetchAffinityGroup(group *AffinityGroup) error {
	if group.ID != "" {
		affinityGroup, count, err := c.cs.AffinityGroup.GetAffinityGroupByID(group.ID, cloudstack.WithProject(c.user.Project.ID))
		if err != nil {
			// handle via multierr
			c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

			return err
		} else if count > 1 {
			// handle via creating a new error.
			return errors.New("count bad")
		}
		group.Name = affinityGroup.Name
		group.Type = affinityGroup.Type

		return nil
	}
	if group.Name != "" {
		affinityGroup, count, err := c.cs.AffinityGroup.GetAffinityGroupByName(group.Name, cloudstack.WithProject(c.user.Project.ID))
		if err != nil {
			// handle via multierr
			c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

			return err
		} else if count > 1 {
			// handle via creating a new error.
			return errors.New("count bad")
		}
		group.ID = affinityGroup.Id
		group.Type = affinityGroup.Type

		return nil
	}

	return errors.Errorf(`could not fetch AffinityGroup by name "%s" or id "%s"`, group.Name, group.ID)
}

func (c *client) GetOrCreateAffinityGroup(group *AffinityGroup) error {
	if err := c.FetchAffinityGroup(group); err != nil { // Group not found?
		p := c.cs.AffinityGroup.NewCreateAffinityGroupParams(group.Name, group.Type)
		p.SetName(group.Name)
		setIfNotEmpty(c.user.Project.ID, p.SetProjectid)
		resp, err := c.cs.AffinityGroup.CreateAffinityGroup(p)
		if err != nil {
			c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

			return err
		}
		group.ID = resp.Id
	}

	return nil
}

func (c *client) DeleteAffinityGroup(group *AffinityGroup) error {
	p := c.cs.AffinityGroup.NewDeleteAffinityGroupParams()
	setIfNotEmpty(group.ID, p.SetId)
	setIfNotEmpty(group.Name, p.SetName)
	setIfNotEmpty(c.user.Project.ID, p.SetProjectid)
	_, err := c.cs.AffinityGroup.DeleteAffinityGroup(p)
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

	return err
}

type affinityGroups []AffinityGroup

func (c *client) getCurrentAffinityGroups(csMachine *infrav1.CloudStackMachine) (affinityGroups, error) {
	// Start by fetching VM details which includes an array of currently associated affinity groups.
	virtM, count, err := c.cs.VirtualMachine.GetVirtualMachineByID(*csMachine.Spec.InstanceID, cloudstack.WithProject(c.user.Project.ID))
	if err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return nil, err
	} else if count > 1 {
		return nil, errors.Errorf("found more than one VM for ID: %s", *csMachine.Spec.InstanceID)
	}
	groups := make([]AffinityGroup, 0, len(virtM.Affinitygroup))
	for _, v := range virtM.Affinitygroup {
		groups = append(groups, AffinityGroup{Name: v.Name, Type: v.Type, ID: v.Id})
	}

	return groups, nil
}

func (ags *affinityGroups) toArrayOfIDs() []string {
	groupIDs := make([]string, 0, len(*ags))
	for _, group := range *ags {
		groupIDs = append(groupIDs, group.ID)
	}

	return groupIDs
}

func (ags *affinityGroups) addGroup(addGroup AffinityGroup) {
	// This is essentially adding to a set followed by array conversion.
	groupSet := map[string]AffinityGroup{addGroup.ID: addGroup}
	for _, group := range *ags {
		groupSet[group.ID] = group
	}
	*ags = make([]AffinityGroup, 0, len(groupSet))
	for _, group := range groupSet {
		*ags = append(*ags, group)
	}
}

func (ags *affinityGroups) removeGroup(removeGroup AffinityGroup) {
	// This is essentially subtracting from a set followed by array conversion.
	groupSet := map[string]AffinityGroup{}
	for _, group := range *ags {
		groupSet[group.ID] = group
	}
	delete(groupSet, removeGroup.ID)
	*ags = make([]AffinityGroup, 0, len(groupSet))
	for _, group := range groupSet {
		*ags = append(*ags, group)
	}
}

func (c *client) stopAndModifyAffinityGroups(csMachine *infrav1.CloudStackMachine, groups affinityGroups) error {
	agp := c.cs.AffinityGroup.NewUpdateVMAffinityGroupParams(*csMachine.Spec.InstanceID)
	agp.SetAffinitygroupids(groups.toArrayOfIDs())

	p1 := c.cs.VirtualMachine.NewStopVirtualMachineParams(*csMachine.Spec.InstanceID)
	if _, err := c.cs.VirtualMachine.StopVirtualMachine(p1); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	}

	if _, err := c.cs.AffinityGroup.UpdateVMAffinityGroup(agp); err != nil {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return err
	}

	p2 := c.cs.VirtualMachine.NewStartVirtualMachineParams(*csMachine.Spec.InstanceID)
	_, err := c.cs.VirtualMachine.StartVirtualMachine(p2)
	c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

	return err
}

func (c *client) AssociateAffinityGroup(csMachine *infrav1.CloudStackMachine, group AffinityGroup) error {
	groups, err := c.getCurrentAffinityGroups(csMachine)
	if err != nil {
		return err
	}
	groups.addGroup(group)

	return c.stopAndModifyAffinityGroups(csMachine, groups)
}

func (c *client) DisassociateAffinityGroup(csMachine *infrav1.CloudStackMachine, group AffinityGroup) error {
	groups, err := c.getCurrentAffinityGroups(csMachine)
	if err != nil {
		return err
	}
	groups.removeGroup(group)

	return c.stopAndModifyAffinityGroups(csMachine, groups)
}
