/*
Copyright 2025.

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

package controllers

import (
	"context"
	"fmt"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/runtime"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

// CloudStackMachineTemplateReconciler reconciles a CloudStackMachineTemplate object.
type CloudStackMachineTemplateReconciler struct {
	client.Client
	Scheme           *runtime.Scheme
	ScopeFactory     scope.ClientScopeFactory
	WatchFilterValue string
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachinetemplates,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachinetemplates/status,verbs=get;update;patch

func (r *CloudStackMachineTemplateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := logger.FromContext(ctx).WithValues("cloudstackmachinetemplate", req.NamespacedName)

	csMachineTemplate := &infrav1.CloudStackMachineTemplate{}
	if err := r.Get(ctx, req.NamespacedName, csMachineTemplate); err != nil {
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// If the capacity is already set, skip the reconciliation.
	// The template spec is immutable after the template is created, so this doesnt need updating.
	if len(csMachineTemplate.Status.Capacity) > 0 {
		return ctrl.Result{}, nil
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, csMachineTemplate.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info("Cluster controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	if annotations.IsPaused(cluster, csMachineTemplate) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", cluster.Name)

	csCluster, err := r.getInfraCluster(ctx, cluster, csMachineTemplate)
	if err != nil {
		return ctrl.Result{}, err
	}

	var fdName string
	if csMachineTemplate.Spec.Template.Spec.FailureDomainName != "" {
		fdName = csMachineTemplate.Spec.Template.Spec.FailureDomainName
	} else {
		// If the failure domain name is not set in the MachineTemplate spec,
		// we take the first failure domain from the CloudStackCluster spec.
		fdName = csCluster.Spec.FailureDomains[0].Name
	}

	fd, err := GetFailureDomainByName(ctx, r.Client, fdName, csMachineTemplate.Namespace, cluster.Name)
	if err != nil {
		log.Error(err, "Failed to get failure domain", "fdname", fdName)
		return ctrl.Result{}, err
	}

	clientScope, err := r.ScopeFactory.NewClientScopeForFailureDomain(ctx, r.Client, fd)
	if err != nil {
		log.Error(err, "Failed to create client scope")
		return ctrl.Result{}, err
	}

	scope, err := scope.NewMachineTemplateScope(scope.MachineTemplateScopeParams{
		Client:                    r.Client,
		Logger:                    log,
		Cluster:                   cluster,
		CloudStackMachineTemplate: csMachineTemplate,
		CSClients:                 clientScope.CSClients(),
		ControllerName:            "cloudstackmachinetemplate",
	})
	if err != nil {
		log.Error(err, "Failed to create machine template scope")
		return ctrl.Result{}, err
	}

	// Always close the scope after each reconciliation to patch the CloudStackMachineTemplate object.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	return ctrl.Result{}, r.reconcile(scope)
}

func (r *CloudStackMachineTemplateReconciler) reconcile(scope *scope.MachineTemplateScope) error {
	scope.Info("Reconcile CloudStackMachineTemplate")

	var serviceOffering *cloudstack.ServiceOffering
	var err error
	if scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.Name != "" {
		serviceOffering, err = scope.CSUser().GetServiceOfferingByName(scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.Name)
		if err != nil {
			scope.Error(err, "Failed to get service offering", "name", scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.Name)
			return err
		}
	} else if scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.ID != "" {
		serviceOffering, err = scope.CSUser().GetServiceOfferingByID(scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.ID)
		if err != nil {
			scope.Error(err, "Failed to get service offering", "id", scope.CloudStackMachineTemplate.Spec.Template.Spec.Offering.ID)
			return err
		}
	}

	capacity, err := getCloudStackMachineCapacity(serviceOffering)
	if err != nil {
		scope.Error(err, "Failed to get capacity for instance", "offering", serviceOffering.Name)
		return err
	}
	scope.CloudStackMachineTemplate.Status.Capacity = capacity
	scope.Info("Successfully populated capacity for instance", "offering", serviceOffering.Name, "capacity", capacity)

	return nil
}

// getInfraCluster fetches the CloudStackCluster for the given CloudStackMachineTemplate.
func (r *CloudStackMachineTemplateReconciler) getInfraCluster(ctx context.Context, cluster *clusterv1.Cluster, cloudStackMachineTemplate *infrav1.CloudStackMachineTemplate) (*infrav1.CloudStackCluster, error) {
	cloudStackCluster := &infrav1.CloudStackCluster{}
	cloudStackClusterName := client.ObjectKey{
		Namespace: cloudStackMachineTemplate.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Get(ctx, cloudStackClusterName, cloudStackCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return cloudStackCluster, nil
}

// getCloudStackMachineCapacity returns the capacity of the given service offering.
// It returns an error if the capacity cannot be parsed.
func getCloudStackMachineCapacity(serviceOffering *cloudstack.ServiceOffering) (corev1.ResourceList, error) {
	capacity := corev1.ResourceList{}

	if serviceOffering.Cpunumber == 0 {
		return nil, fmt.Errorf("service offering %s has no CPU", serviceOffering.Name)
	}
	cpu, err := resource.ParseQuantity(fmt.Sprintf("%d", serviceOffering.Cpunumber))
	if err != nil {
		return nil, fmt.Errorf("failed to parse CPU quantity: %w", err)
	}
	capacity[corev1.ResourceCPU] = cpu

	if serviceOffering.Memory == 0 {
		return nil, fmt.Errorf("service offering %s has no memory", serviceOffering.Name)
	}
	memory, err := resource.ParseQuantity(fmt.Sprintf("%dM", serviceOffering.Memory))
	if err != nil {
		return nil, fmt.Errorf("failed to parse memory quantity: %w", err)
	}
	capacity[corev1.ResourceMemory] = memory

	return capacity, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackMachineTemplateReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)

	return ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackMachineTemplate{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(r.Scheme, log.GetLogger(), r.WatchFilterValue)).
		Complete(r)
}
