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
	"strings"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/cluster-api/util/annotations"
	"sigs.k8s.io/cluster-api/util/patch"
	"sigs.k8s.io/cluster-api/util/predicates"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/apiutil"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

// CloudStackIsolatedNetworkReconciler reconciles a CloudStackIsolatedNetwork object.
type CloudStackIsolatedNetworkReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	ScopeFactory     scope.ClientScopeFactory
	WatchFilterValue string
}

//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackisolatednetworks,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackisolatednetworks/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackisolatednetworks/finalizers,verbs=update

func (r *CloudStackIsolatedNetworkReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := logger.FromContext(ctx)

	// Fetch the CloudStackIsolatedNetwork instance
	csin := &infrav1.CloudStackIsolatedNetwork{}
	err := r.Client.Get(ctx, req.NamespacedName, csin)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CloudStackIsolatedNetwork resource not found. Ignoring since object must be deleted")

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, csin.ObjectMeta)
	if err != nil {
		log.Info("CloudStackIsolatedNetwork is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}
	if cluster == nil {
		log.Info(fmt.Sprintf("Please associate this isolated network with a cluster using the label %s: <name of cluster>", clusterv1.ClusterNameLabel))
		return ctrl.Result{}, nil
	}

	log = log.WithValues("cluster", klog.KObj(cluster))

	if annotations.IsPaused(cluster, csin) {
		log.Info("Reconciliation is paused for this object")
		return ctrl.Result{}, nil
	}

	csCluster, err := r.getInfraCluster(ctx, cluster, csin)
	if err != nil {
		log.Error(err, "Failed to get CloudStackCluster")
		return ctrl.Result{}, err
	}

	clientScope, err := r.ScopeFactory.NewClientScopeForFailureDomainByName(ctx, r.Client, csin.Spec.FailureDomainName, csin.Namespace, cluster.Name)
	if err != nil {
		log.Error(err, "Failed to create client scope")
		return ctrl.Result{}, err
	}

	// Create the isolated network scope.
	scope, err := scope.NewIsolatedNetworkScope(scope.IsolatedNetworkScopeParams{
		Client:                    r.Client,
		Logger:                    log,
		Cluster:                   cluster,
		CloudStackCluster:         csCluster,
		CloudStackFailureDomain:   clientScope.FailureDomain(),
		CloudStackIsolatedNetwork: csin,
		CSClients:                 clientScope.CSClients(),
		ControllerName:            "cloudstackisolatednetwork",
	})
	if err != nil {
		log.Error(err, "Failed to create isolated network scope")
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the CloudStackIsolatedNetwork object and status after each reconciliation.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !csin.DeletionTimestamp.IsZero() {
		// Handle deletion reconciliation loop.
		return ctrl.Result{}, r.reconcileDelete(ctx, scope)
	}

	// Handle normal reconciliation loop.
	return r.reconcileNormal(ctx, scope)
}

func (r *CloudStackIsolatedNetworkReconciler) reconcileDelete(ctx context.Context, scope *scope.IsolatedNetworkScope) error {
	scope.Info("Reconcile CloudStackIsolatedNetwork deletion")

	if err := scope.CSUser().DisposeIsoNetResources(scope.CloudStackIsolatedNetwork, scope.CloudStackCluster); err != nil {
		if !strings.Contains(strings.ToLower(err.Error()), "no match found") {
			return err
		}
	}

	controllerutil.RemoveFinalizer(scope.CloudStackIsolatedNetwork, infrav1.IsolatedNetworkFinalizer)

	return nil
}

func (r *CloudStackIsolatedNetworkReconciler) reconcileNormal(ctx context.Context, scope *scope.IsolatedNetworkScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackIsolatedNetwork")

	// If the CloudStackIsolatedNetwork doesn't have our finalizer, add it.
	if controllerutil.AddFinalizer(scope.CloudStackIsolatedNetwork, infrav1.IsolatedNetworkFinalizer) {
		// Register the finalizer immediately to avoid orphaning resources on delete
		if err := scope.PatchObject(); err != nil {
			return ctrl.Result{}, err
		}
	}

	// Setup isolated network, endpoint, egress, and load balancing.
	// Set endpoint of CloudStackCluster if it is not currently set. (uses patcher to do so)
	csClusterPatcher, err := patch.NewHelper(scope.CloudStackCluster, r.Client)
	if err != nil {
		return ctrl.Result{}, errors.Wrap(err, "setting up CloudStackCluster patcher")
	}
	if scope.FailureDomainZoneID() == "" {
		scope.Info("Zone ID not resolved yet.")

		return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
	}

	if err := scope.CSUser().GetOrCreateIsolatedNetwork(scope.CloudStackFailureDomain, scope.CloudStackIsolatedNetwork); err != nil {
		return ctrl.Result{}, err
	}
	// Tag the created network.
	if err := scope.CSUser().AddClusterTag(cloud.ResourceTypeNetwork, scope.CloudStackIsolatedNetwork.Spec.ID, scope.CloudStackCluster); err != nil {
		return ctrl.Result{}, errors.Wrapf(err, "tagging network with id %s", scope.CloudStackIsolatedNetwork.Spec.ID)
	}

	// Assign IP and configure API server load balancer, if enabled and this cluster is not externally managed.
	if !annotations.IsExternallyManaged(scope.CloudStackCluster) {
		pubIP, err := scope.CSUser().AssociatePublicIPAddress(scope.CloudStackFailureDomain, scope.CloudStackIsolatedNetwork, scope.CloudStackCluster.Spec.ControlPlaneEndpoint.Host)
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to associate public IP address")
		}
		scope.SetControlPlaneEndpointHost(pubIP.Ipaddress)
		scope.SetPublicIPID(pubIP.Id)
		scope.SetPublicIPAddress(pubIP.Ipaddress)

		if scope.APIServerLoadBalancer() == nil {
			scope.SetAPIServerLoadBalancer(&infrav1.LoadBalancer{})
		}
		scope.APIServerLoadBalancer().IPAddressID = pubIP.Id
		scope.APIServerLoadBalancer().IPAddress = pubIP.Ipaddress
		if err := scope.CSUser().AddClusterTag(cloud.ResourceTypeIPAddress, pubIP.Id, scope.CloudStackCluster); err != nil {
			return ctrl.Result{}, errors.Wrapf(err,
				"adding cluster tag to public IP address with ID %s", pubIP.Id)
		}

		if err := scope.CSUser().ReconcileLoadBalancer(scope.CloudStackFailureDomain, scope.CloudStackIsolatedNetwork, scope.CloudStackCluster); err != nil {
			return ctrl.Result{}, errors.Wrap(err, "reconciling load balancer")
		}
	}

	if err := csClusterPatcher.Patch(ctx, scope.CloudStackCluster); err != nil {
		return ctrl.Result{}, errors.Wrap(err, "patching endpoint update to CloudStackCluster")
	}

	scope.SetReady()

	return ctrl.Result{}, nil
}

// getInfraCluster fetches the CloudStackCluster for the given CloudStackIsolatedNetwork.
func (r *CloudStackIsolatedNetworkReconciler) getInfraCluster(ctx context.Context, cluster *clusterv1.Cluster, cloudStackIsolatedNetwork *infrav1.CloudStackIsolatedNetwork) (*infrav1.CloudStackCluster, error) {
	cloudStackCluster := &infrav1.CloudStackCluster{}
	cloudStackClusterName := client.ObjectKey{
		Namespace: cloudStackIsolatedNetwork.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, cloudStackClusterName, cloudStackCluster); err != nil {
		return nil, err
	}
	return cloudStackCluster, nil
}

// CloudStackClusterToCloudStackIsolatedNetworks is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// of CloudStackIsolatedNetworks.
func (r *CloudStackIsolatedNetworkReconciler) CloudStackClusterToCloudStackIsolatedNetworks(obj client.ObjectList, scheme *runtime.Scheme, log logger.Wrapper) (handler.MapFunc, error) {
	gvk, err := apiutil.GVKForObject(obj, scheme)
	if err != nil {
		return nil, errors.Wrap(err, "failed to find GVK for CloudStackIsolatedNetwork")
	}

	return func(ctx context.Context, o client.Object) []ctrl.Request {
		csCluster, ok := o.(*infrav1.CloudStackCluster)
		if !ok {
			log.Error(fmt.Errorf("expected a CloudStackCluster but got a %T", o), "Error in CloudStackClusterToCloudStackIsolatedNetworks")

			return nil
		}

		log = log.WithValues("objectMapper", "cloudstackClusterToCloudStackIsolatedNetworks", "cluster", klog.KRef(csCluster.Namespace, csCluster.Name))

		// Don't handle deleted CloudStackClusters
		if !csCluster.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Trace("CloudStackCluster has a deletion timestamp, skipping mapping.")

			return nil
		}

		clusterName, ok := GetOwnerClusterName(csCluster.ObjectMeta)
		if !ok {
			log.Info("CloudStackCluster is missing cluster name label or cluster does not exist, skipping mapping.")

			return nil
		}

		isonetList := &infrav1.CloudStackIsolatedNetworkList{}
		isonetList.SetGroupVersionKind(gvk)

		// list all the requested objects within the cluster namespace with the cluster name label
		if err := r.Client.List(ctx, isonetList, client.InNamespace(csCluster.Namespace), client.MatchingLabels{clusterv1.ClusterNameLabel: clusterName}); err != nil {
			return nil
		}

		var results []ctrl.Request
		for _, isonet := range isonetList.Items {
			req := ctrl.Request{
				NamespacedName: types.NamespacedName{
					Namespace: isonet.GetNamespace(),
					Name:      isonet.GetName(),
				},
			}
			results = append(results, req)
		}

		return results
	}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackIsolatedNetworkReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)
	cloudStackClusterToCloudStackIsolatedNetworksMapper, err := r.CloudStackClusterToCloudStackIsolatedNetworks(&infrav1.CloudStackIsolatedNetworkList{}, r.Scheme, log)
	if err != nil {
		return errors.Wrap(err, "failed to create CloudStackClusterToCloudStackIsolatedNetworks mapper")
	}

	err = ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackIsolatedNetwork{}).
		WithOptions(options).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(log.GetLogger(), r.WatchFilterValue)).
		Watches(
			&infrav1.CloudStackCluster{},
			handler.EnqueueRequestsFromMapFunc(cloudStackClusterToCloudStackIsolatedNetworksMapper),
			builder.WithPredicates(
				predicate.GenerationChangedPredicate{},
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						oldCSCluster := e.ObjectOld.(*infrav1.CloudStackCluster)
						newCSCluster := e.ObjectNew.(*infrav1.CloudStackCluster)

						// APIServerLoadBalancer disabled in both new and old
						if oldCSCluster.Spec.APIServerLoadBalancer == nil && newCSCluster.Spec.APIServerLoadBalancer == nil {
							return false
						}
						// APIServerLoadBalancer toggled
						if oldCSCluster.Spec.APIServerLoadBalancer == nil || newCSCluster.Spec.APIServerLoadBalancer == nil {
							return true
						}

						return !cmp.Equal(oldCSCluster, newCSCluster)
					},
				},
			),
		).
		Complete(r)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}
