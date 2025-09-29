package scope

import (
	"context"

	"github.com/pkg/errors"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	v1beta1patch "sigs.k8s.io/cluster-api/util/deprecated/v1beta1/patch"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
)

// MachineScopeParams defines the input parameters used to create a new Scope.
type MachineTemplateScopeParams struct {
	Client                    client.Client
	Logger                    *logger.Logger
	Cluster                   *clusterv1.Cluster
	CloudStackMachineTemplate *infrav1.CloudStackMachineTemplate
	CSClients                 CSClientsProvider
	ControllerName            string
}

// NewMachineScope creates a new Scope from the supplied parameters.
// This is meant to be called for each reconcile iteration.
func NewMachineTemplateScope(params MachineTemplateScopeParams) (*MachineTemplateScope, error) {
	if params.Cluster == nil {
		return nil, errors.New("failed to generate new scope from nil Cluster")
	}
	if params.CloudStackMachineTemplate == nil {
		return nil, errors.New("failed to generate new scope from nil CloudStackMachineTemplate")
	}

	if params.Logger == nil {
		log := klog.Background()
		params.Logger = logger.NewLogger(log)
	}

	machineTemplateScope := &MachineTemplateScope{
		Logger:                    *params.Logger,
		client:                    params.Client,
		Cluster:                   params.Cluster,
		CloudStackMachineTemplate: params.CloudStackMachineTemplate,
		controllerName:            params.ControllerName,
		CSClientsProvider:         params.CSClients,
	}

	helper, err := v1beta1patch.NewHelper(params.CloudStackMachineTemplate, params.Client)
	if err != nil {
		return nil, errors.Wrap(err, "failed to init patch helper")
	}

	machineTemplateScope.patchHelper = helper

	return machineTemplateScope, nil
}

// MachineTemplateScope defines the basic context for an actuator to operate upon.
type MachineTemplateScope struct {
	logger.Logger
	client      client.Client
	patchHelper *v1beta1patch.Helper

	Cluster                   *clusterv1.Cluster
	CloudStackMachineTemplate *infrav1.CloudStackMachineTemplate

	CSClientsProvider
	controllerName string
}

// Name returns the machine name.
func (s *MachineTemplateScope) Name() string {
	return s.CloudStackMachineTemplate.Name
}

// Namespace returns the machine namespace.
func (s *MachineTemplateScope) Namespace() string {
	return s.CloudStackMachineTemplate.Namespace
}

// PatchObject persists the machine template configuration and status.
func (s *MachineTemplateScope) PatchObject() error {
	return s.patchHelper.Patch(context.TODO(), s.CloudStackMachineTemplate)
}

// Close the MachineTemplateScope by updating the machine template spec, machine template status.
func (s *MachineTemplateScope) Close() error {
	return s.PatchObject()
}
