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

	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

// CloudStackAffinityGroupReconciler reconciles a CloudStackAffinityGroup object.
type CloudStackAffinityGroupReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	ScopeFactory     scope.ClientScopeFactory
	WatchFilterValue string
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackaffinitygroups,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackaffinitygroups/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackaffinitygroups/finalizers,verbs=update
// Need to watch machine templates for creation of an affinity group.
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachinetemplate,verbs=get;list;watch

func (r *CloudStackAffinityGroupReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := logger.FromContext(ctx)

	// Fetch the CloudStackAffinityGroup instance
	csag := &infrav1.CloudStackAffinityGroup{}
	err := r.Client.Get(ctx, req.NamespacedName, csag)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, csag.ObjectMeta)
	if err != nil {
		log.Info("CloudStackAffinityGroup is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this affinity group with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", klog.KObj(cluster))

	if annotations.IsPaused(cluster, csag) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	fd, err := GetFailureDomainByName(ctx, r.Client, csag.Spec.FailureDomainName, csag.Namespace, cluster.Name)
	if err != nil {
		log.Error(err, "Failed to get failure domain", "fdname", csag.Spec.FailureDomainName)
		return ctrl.Result{}, err
	}

	clientScope, err := r.ScopeFactory.NewClientScopeForFailureDomain(ctx, r.Client, fd)
	if err != nil {
		log.Error(err, "Failed to create client scope")
		return ctrl.Result{}, err
	}

	// Create the affinity group scope.
	scope, err := scope.NewAffinityGroupScope(scope.AffinityGroupScopeParams{
		Client:                  r.Client,
		Logger:                  log,
		Cluster:                 cluster,
		CloudStackAffinityGroup: csag,
		CSClients:               clientScope.CSClients(),
		ControllerName:          "cloudstackaffinitygroup",
	})
	if err != nil {
		log.Error(err, "Failed to create affinity group scope")
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the CloudStackAffinityGroup object and status after each reconciliation.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !csag.DeletionTimestamp.IsZero() {
		// Handle deletion reconciliation loop.
		return ctrl.Result{}, r.reconcileDelete(scope)
	}

	// Handle normal reconciliation loop.
	return ctrl.Result{}, r.reconcileNormal(scope)
}

func (r *CloudStackAffinityGroupReconciler) reconcileDelete(scope *scope.AffinityGroupScope) error {
	scope.Info("Reconcile CloudStackAffinityGroup deletion")

	group := &cloud.AffinityGroup{Name: scope.Name()}
	_ = scope.CSClient().FetchAffinityGroup(group)
	// Affinity group not found, must have been deleted.
	if group.ID == "" {
		// Deleting affinity groups on Cloudstack can return error but succeed in
		// deleting the affinity group. Removing finalizer if affinity group is not
		// present on Cloudstack ensures affinity group object deletion is not blocked.
		controllerutil.RemoveFinalizer(scope.CloudStackAffinityGroup, infrav1.AffinityGroupFinalizer)

		return nil
	}
	if err := scope.CSClient().DeleteAffinityGroup(group); err != nil {
		return err
	}
	controllerutil.RemoveFinalizer(scope.CloudStackAffinityGroup, infrav1.AffinityGroupFinalizer)

	return nil
}

func (r *CloudStackAffinityGroupReconciler) reconcileNormal(scope *scope.AffinityGroupScope) error {
	scope.Info("Reconcile CloudStackAffinityGroup")

	// If the CloudStackAffinityGroup doesn't have our finalizer, add it.
	if controllerutil.AddFinalizer(scope.CloudStackAffinityGroup, infrav1.AffinityGroupFinalizer) {
		// Register the finalizer immediately to avoid orphaning resources on delete
		if err := scope.PatchObject(); err != nil {
			return err
		}
	}

	affinityGroup := &cloud.AffinityGroup{Name: scope.Name(), Type: scope.Type()}
	if err := scope.CSUser().GetOrCreateAffinityGroup(affinityGroup); err != nil {
		return err
	}
	scope.SetID(affinityGroup.ID)
	scope.SetReady()

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackAffinityGroupReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackAffinityGroup{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(r.Scheme, log.GetLogger(), r.WatchFilterValue)).
		Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}
