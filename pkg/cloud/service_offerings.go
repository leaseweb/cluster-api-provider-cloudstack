package cloud

import (
	"fmt"
	"strings"

	"github.com/apache/cloudstack-go/v2/cloudstack"
)

type ServiceOfferingIface interface {
	GetServiceOfferingByID(id string) (*cloudstack.ServiceOffering, error)
	GetServiceOfferingByName(name string) (*cloudstack.ServiceOffering, error)
}

// GetServiceOfferingByID returns the service offering with the given ID.
func (c *client) GetServiceOfferingByID(id string) (*cloudstack.ServiceOffering, error) {
	offering, count, err := c.cs.ServiceOffering.GetServiceOfferingByID(id)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no match found") {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return nil, err
	} else if offering == nil {
		return nil, ErrNotFound
	} else if count == 0 {
		return nil, ErrNotFound
	} else if count > 1 {
		return nil, fmt.Errorf("found more than one service offering with id %s", id)
	}

	return offering, nil
}

// GetServiceOfferingByName returns the service offering with the given name.
func (c *client) GetServiceOfferingByName(name string) (*cloudstack.ServiceOffering, error) {
	offering, count, err := c.cs.ServiceOffering.GetServiceOfferingByName(name)
	if err != nil && !strings.Contains(strings.ToLower(err.Error()), "no match found") {
		c.customMetrics.EvaluateErrorAndIncrementAcsReconciliationErrorCounter(err)

		return nil, err
	} else if offering == nil {
		return nil, ErrNotFound
	} else if count == 0 {
		return nil, ErrNotFound
	} else if count > 1 {
		return nil, fmt.Errorf("found more than one service offering with name %s", name)
	}

	return offering, nil
}
