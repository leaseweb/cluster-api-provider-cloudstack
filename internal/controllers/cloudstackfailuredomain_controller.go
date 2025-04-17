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

package controllers

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

// CloudStackFailureDomainReconciler reconciles a CloudStackFailureDomain object.
type CloudStackFailureDomainReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	ScopeFactory     scope.ClientScopeFactory
	WatchFilterValue string

	IsoNet   *infrav1.CloudStackIsolatedNetwork
	Machines []infrav1.CloudStackMachine
}

// CloudStackFailureDomain RBAC permissions.
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackfailuredomains,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackfailuredomains/status,verbs=create;get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackfailuredomains/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

func (r *CloudStackFailureDomainReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := logger.FromContext(ctx)

	// Fetch the CloudStackFailureDomain instance
	csfd := &infrav1.CloudStackFailureDomain{}
	err := r.Client.Get(ctx, req.NamespacedName, csfd)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, csfd.ObjectMeta)
	if err != nil {
		log.Info("CloudStackFailureDomain is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this failure domain with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", klog.KObj(cluster))

	if annotations.IsPaused(cluster, csfd) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	clientScope, err := r.ScopeFactory.NewClientScopeForFailureDomain(ctx, r.Client, csfd)
	if err != nil {
		log.Error(err, "Failed to create client scope")
		return ctrl.Result{}, err
	}

	// Create the affinity group scope.
	scope, err := scope.NewFailureDomainScope(scope.FailureDomainScopeParams{
		Client:                  r.Client,
		Logger:                  log,
		Cluster:                 cluster,
		CloudStackFailureDomain: csfd,
		CSClients:               clientScope.CSClients(),
		ControllerName:          "cloudstackfailuredomain",
	})
	if err != nil {
		log.Error(err, "Failed to create failure domain scope")
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the CloudStackFailureDomain object and status after each reconciliation.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !csfd.DeletionTimestamp.IsZero() {
		// Handle deletion reconciliation loop.
		return r.reconcileDelete(ctx, scope)
	}

	// Handle normal reconciliation loop.
	return r.reconcileNormal(ctx, scope)
}

func (r *CloudStackFailureDomainReconciler) reconcileDelete(ctx context.Context, scope *scope.FailureDomainScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackFailureDomain deletion")

	// Get all the CloudStackMachines in the failure domain sorted by name.
	machines := &infrav1.CloudStackMachineList{}
	if err := r.Client.List(ctx, machines, client.MatchingLabels{infrav1.FailureDomainLabelName: scope.Name()}); err != nil {
		return ctrl.Result{}, err
	}
	items := machines.Items
	sort.Slice(items, func(i, j int) bool {
		return items[i].Name < items[j].Name
	})
	r.Machines = items

	// Check if the cluster isn't ready / if there is any rolling update going on. Requeue if so.
	if len(r.Machines) > 0 {
		// If the cluster is being deleted, we don't need to do anything.
		if !scope.Cluster.DeletionTimestamp.IsZero() {
			return ctrl.Result{}, nil
		}

		for _, condition := range scope.Cluster.Status.Conditions {
			if condition.Type == clusterv1.ReadyCondition && condition.Status == corev1.ConditionFalse {
				scope.Info("Cluster status not ready. Requeueing.")
				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
		}
	}

	// Delete the machines in the failure domain.
	res, err := r.DeleteMachinesFromFailureDomain(ctx, scope)
	if err != nil || !res.IsZero() {
		return res, err
	}

	// Delete any owned objects before removing finalizer
	if err := r.DeleteOwnedObjects(ctx, scope); err != nil {
		return ctrl.Result{}, err
	}

	// Ensure owned objects are deleted before removing finalizer
	if err := r.EnsureOwnedObjectsDeleted(ctx, scope); err != nil {
		scope.Info("Waiting for owned objects to be deleted", "error", err.Error())
		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	// Remove the finalizer from the failure domain.
	controllerutil.RemoveFinalizer(scope.CloudStackFailureDomain, infrav1.FailureDomainFinalizer)

	return ctrl.Result{}, nil
}

func (r *CloudStackFailureDomainReconciler) reconcileNormal(ctx context.Context, scope *scope.FailureDomainScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackFailureDomain")

	// If the CloudStackCluster doesn't have our finalizer, add it.
	if controllerutil.AddFinalizer(scope.CloudStackFailureDomain, infrav1.FailureDomainFinalizer) {
		// Register the finalizer immediately to avoid orphaning resources on delete
		if err := scope.PatchObject(); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Get the zone and network information.
	if err := scope.ResolveZone(); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "resolving CloudStack zone information")
	}
	if err := scope.ResolveNetwork(); err != nil &&
		!strings.Contains(strings.ToLower(err.Error()), "no match") {
		return ctrl.Result{}, errors.Wrap(err, "resolving Cloudstack network information")
	}

	if scope.NetworkID() == "" || scope.NetworkType() == infrav1.NetworkTypeIsolated {
		if err := r.GenerateIsolatedNetwork(ctx, scope); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "generating isolated network")
		}

		// Get the isolated network from the cluster.
		r.IsoNet = &infrav1.CloudStackIsolatedNetwork{}
		objectKey := client.ObjectKey{Name: scope.IsolatedNetworkName(), Namespace: scope.Namespace()}
		err := client.IgnoreNotFound(r.Client.Get(ctx, objectKey, r.IsoNet))
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to get CloudStackIsolatedNetwork")
		}

		if r.IsoNet.Name == "" {
			scope.Info("Couldn't find isolated network. Requeueing.")

			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
		if !r.IsoNet.Status.Ready {
			scope.Info("Isolated network dependency not ready. Requeueing.")

			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}
	scope.SetReady()

	return ctrl.Result{}, nil
}

// GenerateIsolatedNetwork creates a CloudStackIsolatedNetwork object in the cluster that is owned by the CloudStackFailureDomain.
func (r *CloudStackFailureDomainReconciler) GenerateIsolatedNetwork(ctx context.Context, scope *scope.FailureDomainScope) error {
	lowerName := strings.ToLower(scope.NetworkName())
	metaName := fmt.Sprintf("%s-%s", scope.KubernetesClusterName(), lowerName)
	ownerGVK := scope.OwnerGVK()
	csIsoNet := &infrav1.CloudStackIsolatedNetwork{}
	csIsoNet.ObjectMeta = metav1.ObjectMeta{
		Name:      metaName,
		Namespace: scope.Namespace(),
		Labels:    map[string]string{clusterv1.ClusterNameLabel: scope.KubernetesClusterName()},
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(scope.CloudStackFailureDomain, ownerGVK),
		},
	}
	csIsoNet.Spec.Name = lowerName
	csIsoNet.Spec.FailureDomainName = scope.FailureDomainName()
	if scope.Network().CIDR != "" {
		csIsoNet.Spec.CIDR = scope.Network().CIDR
	}
	if scope.Network().Domain != "" {
		csIsoNet.Spec.Domain = strings.ToLower(scope.Network().Domain)
	}
	csIsoNet.Spec.ControlPlaneEndpoint.Host = scope.Cluster.Spec.ControlPlaneEndpoint.Host
	csIsoNet.Spec.ControlPlaneEndpoint.Port = scope.Cluster.Spec.ControlPlaneEndpoint.Port

	if err := r.Client.Create(ctx, csIsoNet); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return errors.Wrap(err, "failed to create CloudStackIsolatedNetwork resource")
	}

	return nil
}

// DeleteMachinesFromFailureDomain deletes the CAPI machine in FailureDomain.
func (r *CloudStackFailureDomainReconciler) DeleteMachinesFromFailureDomain(ctx context.Context, scope *scope.FailureDomainScope) (ctrl.Result, error) {
	// Pick first machine to delete
	if len(r.Machines) > 0 {
		for _, ref := range r.Machines[0].OwnerReferences {
			if ref.Kind != "Machine" {
				continue
			}
			machine := &clusterv1.Machine{}
			if err := r.Client.Get(ctx, client.ObjectKey{Namespace: scope.Namespace(), Name: ref.Name}, machine); err != nil {
				return ctrl.Result{}, err
			}
			if !machine.DeletionTimestamp.IsZero() {
				scope.Info("Machine is being deleted, ", "machine", machine.Name)

				return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
			}
			if err := r.Client.Delete(ctx, machine); err != nil {
				return ctrl.Result{}, err
			}

			scope.Info("Start to delete machine, ", "machine", machine.Name)

			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	return ctrl.Result{}, nil
}

// DeleteOwnedObjects deletes any CloudStackAffinityGroup or CloudStackIsolatedNetwork resources
// owned by the CloudStackFailureDomain.
func (r *CloudStackFailureDomainReconciler) DeleteOwnedObjects(ctx context.Context, scope *scope.FailureDomainScope) error {
	// Delete owned CloudStackIsolatedNetworks
	isoNets := &infrav1.CloudStackIsolatedNetworkList{}
	if err := r.Client.List(ctx, isoNets, client.InNamespace(scope.Namespace())); err != nil {
		return fmt.Errorf("failed to list CloudStackIsolatedNetworks: %w", err)
	}

	for i := range isoNets.Items {
		isoNet := &isoNets.Items[i]
		if metav1.IsControlledBy(isoNet, scope.CloudStackFailureDomain) {
			if err := r.Client.Delete(ctx, isoNet); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete owned CloudStackIsolatedNetwork %s: %w",
					isoNet.Name, err)
			}
			scope.Info("Deleted owned CloudStackIsolatedNetwork", "name", isoNet.Name)
		}
	}

	// Delete owned CloudStackAffinityGroups
	affinityGroups := &infrav1.CloudStackAffinityGroupList{}
	if err := r.Client.List(ctx, affinityGroups, client.InNamespace(scope.Namespace())); err != nil {
		return fmt.Errorf("failed to list CloudStackAffinityGroups: %w", err)
	}

	for i := range affinityGroups.Items {
		ag := &affinityGroups.Items[i]
		if metav1.IsControlledBy(ag, scope.CloudStackFailureDomain) {
			if err := r.Client.Delete(ctx, ag); err != nil && !apierrors.IsNotFound(err) {
				return fmt.Errorf("failed to delete owned CloudStackAffinityGroup %s: %w",
					ag.Name, err)
			}
			scope.Info("Deleted owned CloudStackAffinityGroup", "name", ag.Name)
		}
	}

	return nil
}

// EnsureOwnedObjectsDeleted verifies that all CloudStackAffinityGroup and CloudStackIsolatedNetwork
// resources owned by the CloudStackFailureDomain have been deleted.
func (r *CloudStackFailureDomainReconciler) EnsureOwnedObjectsDeleted(ctx context.Context, scope *scope.FailureDomainScope) error {
	// Check CloudStackIsolatedNetworks
	isoNets := &infrav1.CloudStackIsolatedNetworkList{}
	if err := r.Client.List(ctx, isoNets, client.InNamespace(scope.Namespace())); err != nil {
		return fmt.Errorf("failed to list CloudStackIsolatedNetworks: %w", err)
	}

	for i := range isoNets.Items {
		if metav1.IsControlledBy(&isoNets.Items[i], scope.CloudStackFailureDomain) {
			return fmt.Errorf("waiting for CloudStackIsolatedNetwork %s to be deleted",
				isoNets.Items[i].Name)
		}
	}

	// Check CloudStackAffinityGroups
	affinityGroups := &infrav1.CloudStackAffinityGroupList{}
	if err := r.Client.List(ctx, affinityGroups, client.InNamespace(scope.Namespace())); err != nil {
		return fmt.Errorf("failed to list CloudStackAffinityGroups: %w", err)
	}

	for i := range affinityGroups.Items {
		if metav1.IsControlledBy(&affinityGroups.Items[i], scope.CloudStackFailureDomain) {
			return fmt.Errorf("waiting for CloudStackAffinityGroup %s to be deleted",
				affinityGroups.Items[i].Name)
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackFailureDomainReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackFailureDomain{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(r.Scheme, log.GetLogger(), r.WatchFilterValue)).
		Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}
