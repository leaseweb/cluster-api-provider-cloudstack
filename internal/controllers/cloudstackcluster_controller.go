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
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

// CloudStackClusterReconciler reconciles a CloudStackCluster object.
type CloudStackClusterReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	WatchFilterValue string
}

// RBAC permissions used in all reconcilers. ConfigMaps, Events and Secrets.
//+kubebuilder:rbac:groups="",resources=configmaps;,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=events,verbs=get;list;watch;create;update;patch
//+kubebuilder:rbac:groups="",resources=secrets;,verbs=get;list;watch

// CloudStackCluster RBAC permissions.
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclusters/status,verbs=create;get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=cluster.x-k8s.io,resources=clusters;clusters/status,verbs=get;list;watch

func (r *CloudStackClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := logger.FromContext(ctx)

	// Fetch the CloudStackCluster instance
	cscluster := &infrav1.CloudStackCluster{}
	err := r.Client.Get(ctx, req.NamespacedName, cscluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CloudStackCluster resource not found. Ignoring since object must be deleted")

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetOwnerCluster(ctx, r.Client, cscluster.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}

	if cluster == nil {
		log.Info("Cluster Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", klog.KObj(cluster))

	if annotations.IsPaused(cluster, cscluster) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	// Create the scope.
	scope, err := scope.NewClusterScope(scope.ClusterScopeParams{
		Client:            r.Client,
		Logger:            log,
		Cluster:           cluster,
		CloudStackCluster: cscluster,
		ControllerName:    "cloudstackcluster",
	})
	if err != nil {
		log.Error(err, "Failed to create cluster scope")
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the CloudStackCluster object and status after each reconciliation.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !cscluster.DeletionTimestamp.IsZero() {
		// Handle deletion reconciliation loop.
		return r.reconcileDelete(ctx, scope)
	}

	// Handle normal reconciliation loop.
	return r.reconcileNormal(ctx, scope)
}

func (r *CloudStackClusterReconciler) reconcileDelete(ctx context.Context, scope *scope.ClusterScope) (ctrl.Result, error) {
	// Skip deletion if finalizer is not present.
	if !controllerutil.ContainsFinalizer(scope.CloudStackCluster, infrav1.ClusterFinalizer) {
		scope.Info("No finalizer on CloudStackCluster, skipping deletion reconciliation")
		return ctrl.Result{}, nil
	}

	scope.Info("Reconcile CloudStackCluster deletion")

	fds := &infrav1.CloudStackFailureDomainList{}
	if err := r.GetFailureDomains(ctx, scope, fds); err != nil {
		return ctrl.Result{}, err
	}
	if len(fds.Items) > 0 {
		for idx := range fds.Items {
			if err := r.Client.Delete(ctx, &fds.Items[idx]); err != nil {
				return ctrl.Result{}, err
			}
		}
		scope.Info("Child FailureDomains still present, requeueing.")

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	controllerutil.RemoveFinalizer(scope.CloudStackCluster, infrav1.ClusterFinalizer)

	return ctrl.Result{}, nil
}

func (r *CloudStackClusterReconciler) reconcileNormal(ctx context.Context, scope *scope.ClusterScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackCluster")

	// If the CloudStackCluster doesn't have our finalizer, add it.
	if controllerutil.AddFinalizer(scope.CloudStackCluster, infrav1.ClusterFinalizer) {
		// Register the finalizer immediately to avoid orphaning resources on delete
		if err := scope.PatchObject(); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Create any non existing failure domain and set the FailureDomains status map in CloudStackCluster,
	// which is used by the CAPI Cluster Controller for machine placement.
	for _, fdSpec := range scope.FailureDomains() {
		metaHashName := infrav1.FailureDomainHashedMetaName(fdSpec.Name, scope.KubernetesClusterName())
		if err := r.CreateFailureDomain(ctx, scope, fdSpec); err != nil {
			if !apierrors.IsAlreadyExists(err) {
				return ctrl.Result{}, errors.Wrap(err, "creating CloudStackFailureDomains")
			}
		}
		scope.SetFailureDomain(fdSpec.Name, clusterv1.FailureDomainSpec{
			ControlPlane: true,
			Attributes: map[string]string{
				"MetaHashName": metaHashName,
			},
		})
	}

	fds := &infrav1.CloudStackFailureDomainList{}
	if err := r.GetFailureDomains(ctx, scope, fds); err != nil {
		return ctrl.Result{}, err
	}

	// Delete any failure domains that are no longer listed under the CloudStackCluster's spec.
	if err := r.DeleteRemovedFailureDomains(ctx, scope, fds); err != nil {
		return ctrl.Result{}, err
	}

	// Verify that all required failure domains are present and ready.
	if err := r.VerifyFailureDomainsExist(scope, fds); err != nil {
		scope.Info(fmt.Sprintf("%s, requeueing.", err.Error()))

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	scope.SetReady()

	return ctrl.Result{}, nil
}

// CreateFailureDomain creates a CloudStackFailureDomain owned by CloudStackCluster.
func (r *CloudStackClusterReconciler) CreateFailureDomain(ctx context.Context, clusterScope *scope.ClusterScope, fdSpec infrav1.CloudStackFailureDomainSpec) error {
	metaHashName := infrav1.FailureDomainHashedMetaName(fdSpec.Name, clusterScope.KubernetesClusterName())
	ownerGVK := clusterScope.OwnerGVK()
	csFD := &infrav1.CloudStackFailureDomain{
		ObjectMeta: metav1.ObjectMeta{
			Name:      metaHashName,
			Namespace: clusterScope.Namespace(),
			Labels:    map[string]string{clusterv1.ClusterNameLabel: clusterScope.KubernetesClusterName()},
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(clusterScope.CloudStackCluster, ownerGVK),
			},
		},
		Spec: fdSpec,
	}

	return r.Client.Create(ctx, csFD)
}

// GetFailureDomains gets CloudStackFailureDomains owned by a CloudStackCluster.
func (r *CloudStackClusterReconciler) GetFailureDomains(ctx context.Context, clusterScope *scope.ClusterScope, fds *infrav1.CloudStackFailureDomainList) error {
	capiClusterLabel := map[string]string{clusterv1.ClusterNameLabel: clusterScope.CloudStackCluster.GetLabels()[clusterv1.ClusterNameLabel]}

	if err := r.Client.List(
		ctx,
		fds,
		client.InNamespace(clusterScope.CloudStackCluster.Namespace),
		client.MatchingLabels(capiClusterLabel),
	); err != nil {
		return errors.Wrap(err, "failed to list failure domains")
	}

	return nil
}

// VerifyFailureDomainsExist verifies that all required failure domains are present.
func (r *CloudStackClusterReconciler) VerifyFailureDomainsExist(clusterScope *scope.ClusterScope, fds *infrav1.CloudStackFailureDomainList) error {
	// Check that all required failure domains are present and ready.
	for _, requiredFdSpec := range clusterScope.FailureDomains() {
		found := false
		for _, fd := range fds.Items {
			if requiredFdSpec.Name == fd.Spec.Name {
				found = true
				if !fd.Status.Ready {
					return fmt.Errorf("required CloudStackFailureDomain %s not ready", fd.Spec.Name)
				}

				break
			}
		}
		if !found {
			return fmt.Errorf("required CloudStackFailureDomain %s not found", requiredFdSpec.Name)
		}
	}

	return nil
}

// DeleteRemovedFailureDomains deletes failure domains no longer listed under the CloudStackCluster's spec.
func (r *CloudStackClusterReconciler) DeleteRemovedFailureDomains(ctx context.Context, clusterScope *scope.ClusterScope, fds *infrav1.CloudStackFailureDomainList) error {
	// Create a map of failure domains that are present.
	fdPresenceByName := map[string]bool{}
	for _, fdSpec := range clusterScope.FailureDomains() {
		name := fdSpec.Name
		fdPresenceByName[name] = true
	}

	// Delete each failure domain that is not present in the spec.
	for _, fd := range fds.Items {
		if _, present := fdPresenceByName[fd.Spec.Name]; !present {
			toDelete := fd
			if err := r.Client.Delete(ctx, &toDelete); err != nil {
				return errors.Wrap(err, "failed to delete obsolete failure domain")
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackClusterReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackCluster{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(log.GetLogger(), r.WatchFilterValue)).
		WithEventFilter(
			predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Avoid reconciling if the event triggering the reconciliation is related to incremental status updates
					// for CloudStackCluster resources only
					if e.ObjectOld.GetObjectKind().GroupVersionKind().Kind != "CloudStackCluster" {
						return true
					}

					oldCluster := e.ObjectOld.(*infrav1.CloudStackCluster).DeepCopy()
					newCluster := e.ObjectNew.(*infrav1.CloudStackCluster).DeepCopy()
					// Ignore resource version because they are unique
					oldCluster.ObjectMeta.ResourceVersion = ""
					newCluster.ObjectMeta.ResourceVersion = ""
					// Ignore incremental status updates
					oldCluster.Status = infrav1.CloudStackClusterStatus{}
					newCluster.Status = infrav1.CloudStackClusterStatus{}

					return !cmp.Equal(oldCluster, newCluster)
				},
			},
		).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(util.ClusterToInfrastructureMapFunc(ctx, infrav1.GroupVersion.WithKind("CloudStackCluster"), mgr.GetClient(), &infrav1.CloudStackCluster{})),
			builder.WithPredicates(
				predicates.ClusterUnpaused(log.GetLogger()),
			),
		).
		WithEventFilter(predicates.ResourceIsNotExternallyManaged(log.GetLogger())).
		Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}
