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

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/google/go-cmp/cmp"
	"github.com/pkg/errors"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
	clusterv1 "sigs.k8s.io/cluster-api/api/v1beta1"
	capierrors "sigs.k8s.io/cluster-api/errors"
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
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/logger"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/scope"
)

const (
	KindCloudStackMachine = "CloudStackMachine"

	CSMachineCreationSuccess = "Created new CloudStack instance %s"
	CSMachineCreationFailed  = "Failed to create new CloudStack instance: %s"
	MachineInstanceRunning   = "Machine instance is Running..."
)

// CloudStackMachineReconciler reconciles a CloudStackMachine object.
type CloudStackMachineReconciler struct {
	Client           client.Client
	Scheme           *runtime.Scheme
	Recorder         record.EventRecorder
	ScopeFactory     scope.ClientScopeFactory
	WatchFilterValue string
}

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch

func (r *CloudStackMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, reterr error) {
	log := ctrl.LoggerFrom(ctx)

	// Fetch the CloudStackMachine instance
	csMachine := &infrav1.CloudStackMachine{}
	err := r.Client.Get(ctx, req.NamespacedName, csMachine)
	if err != nil {
		if apierrors.IsNotFound(err) {
			log.Info("CloudStackMachine resource not found. Ignoring since object must be deleted")

			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Fetch the Machine.
	machine, err := util.GetOwnerMachine(ctx, r.Client, csMachine.ObjectMeta)
	if err != nil {
		return ctrl.Result{}, err
	}
	if machine == nil {
		log.Info("Machine Controller has not yet set OwnerRef")
		return ctrl.Result{}, nil
	}

	log = log.WithValues("machine", klog.KObj(machine))

	// Fetch the Cluster.
	cluster, err := util.GetClusterFromMetadata(ctx, r.Client, machine.ObjectMeta)
	if err != nil {
		log.Info("Machine is missing cluster label or cluster does not exist")
		return ctrl.Result{}, err
	}

	log = log.WithValues("cluster", klog.KObj(cluster))
	ctx = ctrl.LoggerInto(ctx, log)

	if annotations.IsPaused(cluster, csMachine) {
		log.Info("CloudStackMachine or linked Cluster is marked as paused. Won't reconcile")
		return ctrl.Result{}, nil
	}

	csCluster, err := r.getInfraCluster(ctx, cluster, csMachine)
	if err != nil {
		log.Error(err, "Failed to get CloudStackCluster")
		return ctrl.Result{}, err
	}
	if csCluster == nil {
		log.Info("CloudStackCluster not ready yet")
		return ctrl.Result{}, nil
	}

	var fdName string
	if machine.Spec.FailureDomain != nil {
		fdName = *machine.Spec.FailureDomain
	} else {
		fdName = csMachine.Spec.FailureDomainName
	}
	if fdName == "" {
		log.Info("No failuredomain name found yet, skipping reconciliation")

		return ctrl.Result{}, nil
	}

	clientScope, err := r.ScopeFactory.NewClientScopeForFailureDomainByName(ctx, r.Client, fdName, csMachine.Namespace, cluster.Name)
	if err != nil {
		log.Error(err, "Failed to create client scope")
		return ctrl.Result{}, err
	}

	// Create the machine scope.
	scope, err := scope.NewMachineScope(scope.MachineScopeParams{
		Client:                  r.Client,
		Cluster:                 cluster,
		CloudStackCluster:       csCluster,
		Machine:                 machine,
		CloudStackMachine:       csMachine,
		CloudStackFailureDomain: clientScope.FailureDomain(),
		CSClients:               clientScope.CSClients(),
		ControllerName:          "cloudstackmachine",
	})
	if err != nil {
		log.Error(err, "Failed to create machine scope")
		return ctrl.Result{}, err
	}

	// Always attempt to Patch the CloudStackMachine object and status after each reconciliation.
	defer func() {
		if err := scope.Close(); err != nil && reterr == nil {
			reterr = err
		}
	}()

	if !csMachine.DeletionTimestamp.IsZero() {
		// Handle deletion reconciliation loop.
		return r.reconcileDelete(scope)
	}

	// Handle normal reconciliation loop.
	return r.reconcileNormal(ctx, scope)
}

func (r *CloudStackMachineReconciler) reconcileDelete(scope *scope.MachineScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackMachine deletion")

	vm, err := r.findInstance(scope)
	if err != nil && !errors.Is(err, cloud.ErrNotFound) {
		return ctrl.Result{}, err
	}
	if vm == nil {
		scope.Warn("VM instance not found, skipping deletion")
		r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "NoInstanceFound", "Unable to find matching CloudStack instance")
		controllerutil.RemoveFinalizer(scope.CloudStackMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	}

	scope.Info("Instance found matching deleted CloudStackMachine", "instance-id", vm.Id)

	if err := r.reconcileLBattachments(scope); err != nil {
		return ctrl.Result{}, errors.Errorf("failed to reconcile LB attachment: %+v", err)
	}

	switch vm.State {
	case cloud.InstanceStateStopping:
		scope.Info("Instance is stopping, requeueing", "instance-id", vm.Id)
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	case cloud.InstanceStateExpunging, cloud.InstanceStateDestroyed:
		scope.Info("Instance is expunging or destroyed, removing finalizer", "instance-id", vm.Id)
		controllerutil.RemoveFinalizer(scope.CloudStackMachine, infrav1.MachineFinalizer)
		return ctrl.Result{}, nil
	default:
		scope.Info("Terminating instance", "instance-id", vm.Id)

		// Use CSClient instead of CSUser here to expunge as admin.
		// The CloudStack-Go API does not return an error, but the VM won't delete with Expunge set if requested by
		// non-domain admin user.
		if err := scope.CSClient().DestroyVMInstance(scope.CloudStackMachine); err != nil {
			if err.Error() == "VM deletion in progress" {
				scope.Info("VM deletion in progress, requeueing", "instance-id", vm.Id)

				return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
			}
			scope.Error(err, "Failed to destroy VM instance")
			r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "FailedDestroyVM", "Failed to destroy VM instance %q: %v", vm.Id, err)

			return ctrl.Result{}, err
		}
		scope.Info("VM instance successfully destroyed", "instance-id", vm.Id)
		r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeNormal, "SuccessfullDestroyVM", "Destroyed VM instance %q", vm.Id)

		// Requeue until the VM is expunging or destroyed, or can no longer be found.
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}
}

func (r *CloudStackMachineReconciler) reconcileNormal(ctx context.Context, scope *scope.MachineScope) (ctrl.Result, error) {
	scope.Info("Reconcile CloudStackMachine")

	// If the CloudStackMachine is in a failed state, skip reconciliation.
	if scope.HasFailed() {
		scope.Info("CloudStackMachine has failed, skipping reconciliation")
		return ctrl.Result{}, nil
	}

	if !scope.Cluster.Status.InfrastructureReady {
		scope.Info("Cluster infrastructure is not ready yet")
		return ctrl.Result{}, nil
	}

	// Make sure bootstrap data is available and populated.
	if scope.Machine.Spec.Bootstrap.DataSecretName == nil {
		scope.Info("Bootstrap data secret name is not available yet")
		return ctrl.Result{}, nil
	}

	// Delete any Machine associated with the CloudStackMachine if its failuredomain does not
	// exist or no longer exists.
	if err := r.deleteMachineIfFailuredomainNotExist(ctx, scope); err != nil {
		return ctrl.Result{}, err
	}

	// If the CloudStackMachine doesn't have our finalizer, add it.
	if controllerutil.AddFinalizer(scope.CloudStackMachine, infrav1.MachineFinalizer) {
		// Register the finalizer before we create any resources to avoid orphaning them on delete.
		if err := scope.PatchObject(); err != nil {
			return ctrl.Result{}, err
		}
	}

	if scope.NetworkType() == cloud.NetworkTypeIsolated {
		objectKey := client.ObjectKey{Name: scope.IsolatedNetworkName(), Namespace: scope.Namespace()}

		scope.CloudStackIsolatedNetwork = &infrav1.CloudStackIsolatedNetwork{}
		err := client.IgnoreNotFound(r.Client.Get(ctx, objectKey, scope.CloudStackIsolatedNetwork))
		if err != nil {
			return ctrl.Result{}, errors.Wrap(err, "failed to get CloudStackIsolatedNetwork")
		}
	}

	// Get or create the CloudStackAffinityGroup if affinity is enabled.
	if scope.AffinityEnabled() {
		var agName string
		var err error
		if scope.AffinityGroupRef() != nil {
			agName = scope.AffinityGroupName()
		} else {
			agName, err = GenerateAffinityGroupName(*scope.CloudStackMachine, scope.Machine, scope.Cluster)
			if err != nil {
				return ctrl.Result{}, err
			}
		}

		scope.CloudStackAffinityGroup = &infrav1.CloudStackAffinityGroup{}
		scope.CloudStackAffinityGroup.Spec.FailureDomainName = scope.FailureDomainName()

		if err := r.GetOrCreateAffinityGroup(ctx, agName, scope); err != nil {
			return ctrl.Result{}, err
		}
		scope.SetAffinityGroupRef(&corev1.ObjectReference{
			Kind:      scope.CloudStackAffinityGroup.Kind,
			UID:       scope.CloudStackAffinityGroup.UID,
			Name:      scope.CloudStackAffinityGroup.Name,
			Namespace: scope.CloudStackAffinityGroup.Namespace,
		})

		if !scope.CloudStackAffinityGroup.Status.Ready {
			scope.Info("Required affinity group not ready. Requeueing.")

			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}
	}

	vm, err := r.findInstance(scope)
	if err != nil && !errors.Is(err, cloud.ErrNotFound) {
		return ctrl.Result{}, err
	}
	if vm == nil {
		userData, err := scope.GetBootstrapData()
		if err != nil {
			return ctrl.Result{}, err
		}
		vm, err = scope.CSUser().CreateVMInstance(scope.CloudStackMachine, scope.Machine, scope.CloudStackFailureDomain, scope.CloudStackAffinityGroup, userData)
		if err != nil {
			scope.Error(err, "failed to create VM instance")
			r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "InstanceCreatingError", CSMachineCreationFailed, err.Error())
			scope.SetInstanceState(cloud.InstanceStateError)
			return ctrl.Result{}, err
		}
		r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeNormal, "InstanceCreated", CSMachineCreationSuccess, vm.Name)
		scope.Info("Created a new CloudStack instance", "instance-name", vm.Name, "instance-id", vm.Id)
	}

	scope.SetInstanceID(vm.Id)
	scope.SetProviderID(vm.Id)

	prevState := scope.GetInstanceState()
	scope.SetInstanceState(vm.State)
	if prevState == "" || prevState != vm.State {
		scope.Info("Instance state changed", "state", vm.State, "instance-id", scope.GetInstanceID())
	}

	shouldRequeue := false
	switch vm.State {
	case cloud.InstanceStateStarting:
		scope.SetNotReady()
		shouldRequeue = true
		scope.Info("Instance is starting", "instance-id", scope.GetInstanceID())
	case cloud.InstanceStateStopping, cloud.InstanceStateStopped:
		scope.SetNotReady()
		scope.Info("Instance is stopping or stopped", "instance-id", scope.GetInstanceID())
	case cloud.InstanceStateRunning:
		if !scope.IsReady() {
			scope.Info("Instance is running", "instance-id", scope.GetInstanceID())
			r.Recorder.Event(scope.CloudStackMachine, corev1.EventTypeNormal, cloud.InstanceStateRunning, MachineInstanceRunning)
		}
		scope.SetReady()
	default:
		scope.SetNotReady()
		scope.Info("Instance state is unexpected", "state", vm.State, "instance-id", scope.GetInstanceID())
		r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "InstanceStateUnexpected", "CloudStack instance state is unexpected")
		scope.SetFailureReason(capierrors.UpdateMachineError)
		scope.SetFailureMessage(errors.Errorf("CloudStack instance state %s is unexpected", vm.State))
	}

	// tasks that can only take place during operational instance states
	if scope.InstanceIsOperational() {
		addresses, err := scope.CSUser().GetInstanceAddresses(vm)
		if err != nil {
			return ctrl.Result{}, err
		}
		scope.SetAddresses(addresses)

		// If the instance is not just created (previous state empty and current state stopped) or starting,
		// reconcile the load balancer attachments.
		if !(prevState == "" && vm.State == cloud.InstanceStateStopped) || vm.State != cloud.InstanceStateStarting {
			if err := r.reconcileLBattachments(scope); err != nil {
				return ctrl.Result{}, err
			}
		} else {
			shouldRequeue = true
		}
	}

	scope.Debug("Done reconciling CloudStackMachine", "instance-id", scope.GetInstanceID())
	if shouldRequeue {
		scope.Debug("Instance is still pending, requeueing", "instance-id", scope.GetInstanceID())
		return ctrl.Result{RequeueAfter: 10 * time.Second}, nil
	}

	return ctrl.Result{}, nil
}

// reconcileLBattachments reconciles the load balancer attachments/detachments for the CloudStackMachine.
func (r *CloudStackMachineReconciler) reconcileLBattachments(scope *scope.MachineScope) error {
	if !scope.IsExternallyManaged() && scope.IsControlPlane() && scope.NetworkType() == cloud.NetworkTypeIsolated && scope.IsLBEnabled() {
		if scope.IsolatedNetworkName() == "" {
			return errors.New("Could not get required Isolated Network for VM")
		}

		if scope.CloudStackMachineIsDeleted() || scope.MachineIsDeleted() || !scope.InstanceIsRunning() {
			scope.Info("Removing VM from load balancer rule.", "instance-id", scope.GetInstanceID())
			removed, err := scope.CSUser().RemoveVMFromLoadBalancerRules(scope.CloudStackIsolatedNetwork, *scope.CloudStackMachine.Spec.InstanceID)
			if err != nil {
				r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "FailedDetachControlPlaneLB",
					"Failed to unregister control plane instance %q from load balancer: %v", scope.GetInstanceID(), err)
				return err
			}
			if removed {
				scope.Info("VM detached from load balancer rule", "instance-id", scope.GetInstanceID())
				r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeNormal, "SuccessfulDetachControlPlaneLB",
					"Control plane instance %q is de-registered from load balancer", scope.GetInstanceID())
			}
		} else {
			scope.Info("Assigning VM to load balancer rule.", "instance-id", scope.GetInstanceID())
			assigned, err := scope.CSUser().AssignVMToLoadBalancerRules(scope.CloudStackIsolatedNetwork, *scope.CloudStackMachine.Spec.InstanceID)
			if err != nil {
				r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeWarning, "FailedAttachControlPlaneLB",
					"Failed to register control plane instance %q with load balancer: %v", scope.GetInstanceID(), err)
				return err
			}
			if assigned {
				scope.Info("VM attached to load balancer rule", "instance-id", scope.GetInstanceID())
				r.Recorder.Eventf(scope.CloudStackMachine, corev1.EventTypeNormal, "SuccessfulAttachControlPlaneLB",
					"Control plane instance %q is registered with load balancer", scope.GetInstanceID())
			}
		}
	}

	return nil
}

// findInstance finds the VM instance for the given CloudStackMachine, either by instance ID or name.
// If InstanceID is empty, it will be retrieved by name.
// If it still cannot find the instance, it will return an error.
func (r *CloudStackMachineReconciler) findInstance(scope *scope.MachineScope) (*cloudstack.VirtualMachine, error) {
	var instance *cloudstack.VirtualMachine
	var err error

	if scope.GetInstanceID() != "" {
		instance, err = scope.CSUser().GetVMInstanceByID(scope.GetInstanceID())
		if err != nil && !errors.Is(err, cloud.ErrNotFound) {
			return nil, err
		}
		if instance != nil {
			return instance, nil
		}
	}
	instance, err = scope.CSUser().GetVMInstanceByName(scope.Name())
	if err != nil {
		return nil, err
	}

	return instance, nil
}

// GenerateAffinityGroupName generates the affinity group name relevant to this machine.
func GenerateAffinityGroupName(csMachine infrav1.CloudStackMachine, capiMachine *clusterv1.Machine, capiCluster *clusterv1.Cluster) (string, error) {
	managerOwnerRef := GetManagementOwnerRef(capiMachine)
	if managerOwnerRef == nil {
		return "", errors.Errorf("could not find owner UID for %s/%s", capiMachine.Namespace, capiMachine.Name)
	}
	titleCaser := cases.Title(language.English)

	// If the machine's owner is KubeadmControlPlane or EtcdadmCluster, then we don't consider the name and UID of the
	// owner, since there will only be one of each of those per cluster.
	if managerOwnerRef.Kind == "KubeadmControlPlane" || managerOwnerRef.Kind == "EtcdadmCluster" {
		return fmt.Sprintf("%s-%s-%sAffinity-%s-%s",
			capiCluster.Name, capiCluster.UID, titleCaser.String(csMachine.Spec.Affinity), managerOwnerRef.Kind, csMachine.Spec.FailureDomainName), nil
	}

	return fmt.Sprintf("%s-%s-%sAffinity-%s-%s-%s",
		capiCluster.Name, capiCluster.UID, titleCaser.String(csMachine.Spec.Affinity), managerOwnerRef.Name, managerOwnerRef.UID, csMachine.Spec.FailureDomainName), nil
}

// GetOrCreateAffinityGroup of the passed name that's owned by the failure domain of the reconciliation subject and
// the control plane that manages it.
func (r *CloudStackMachineReconciler) GetOrCreateAffinityGroup(ctx context.Context, name string, scope *scope.MachineScope) error {
	// Start by attempting a fetch.
	lowerName := strings.ToLower(name)
	namespace := scope.Namespace()
	objKey := client.ObjectKey{Namespace: namespace, Name: lowerName}
	if err := r.Client.Get(ctx, objKey, scope.CloudStackAffinityGroup); client.IgnoreNotFound(err) != nil {
		return err
	} else if scope.CloudStackAffinityGroup.Name != "" {
		return nil
	} // Didn't find a group, so create instead.

	// Set affinity group type.
	switch scope.AffinityType() {
	case infrav1.AffinityTypePro:
		scope.CloudStackAffinityGroup.Spec.Type = "host affinity"
	case infrav1.AffinityTypeAnti:
		scope.CloudStackAffinityGroup.Spec.Type = "host anti-affinity"
	case infrav1.AffinityTypeSoftPro:
		scope.CloudStackAffinityGroup.Spec.Type = "non-strict host affinity"
	case infrav1.AffinityTypeSoftAnti:
		scope.CloudStackAffinityGroup.Spec.Type = "non-strict host anti-affinity"
	default:
		return errors.Errorf("unrecognized affinity type %s", scope.AffinityType())
	}

	// Setup basic metadata.
	scope.CloudStackAffinityGroup.Name = name
	scope.CloudStackAffinityGroup.Spec.Name = name
	ownerGVK := scope.CloudStackMachine.GetObjectKind().GroupVersionKind()
	scope.CloudStackAffinityGroup.ObjectMeta = metav1.ObjectMeta{
		Name:      lowerName,
		Namespace: scope.Namespace(),
		Labels:    map[string]string{clusterv1.ClusterNameLabel: scope.KubernetesClusterName()},
		OwnerReferences: []metav1.OwnerReference{
			*metav1.NewControllerRef(scope.CloudStackMachine, ownerGVK),
		},
	}
	scope.CloudStackAffinityGroup.OwnerReferences = append(scope.CloudStackAffinityGroup.OwnerReferences,
		metav1.OwnerReference{
			Name:       scope.FailureDomainName(),
			Kind:       scope.CloudStackFailureDomain.Kind,
			APIVersion: scope.CloudStackFailureDomain.APIVersion,
			UID:        scope.CloudStackFailureDomain.UID,
		})

	if err := r.Client.Create(ctx, scope.CloudStackAffinityGroup); err != nil && !strings.Contains(strings.ToLower(err.Error()), "already exists") {
		return errors.Wrap(err, "creating CloudStackAffinityGroup CRD")
	}

	return nil
}

// deleteMachineIfFailuredomainNotExist deletes the Machine associated with the CloudStackMachine if its failuredomain does not exist.
func (r *CloudStackMachineReconciler) deleteMachineIfFailuredomainNotExist(ctx context.Context, scope *scope.MachineScope) error {
	if scope.Machine.Spec.FailureDomain == nil {
		return nil
	}
	capiAssignedFailuredomainName := *scope.Machine.Spec.FailureDomain
	exist := false
	for _, fd := range scope.CloudStackCluster.Spec.FailureDomains {
		if capiAssignedFailuredomainName == fd.Name {
			exist = true

			break
		}
	}
	if !exist {
		scope.Info("CAPI Machine in non-existent failuredomain. Deleting associated Machine.", "csMachine", scope.CloudStackMachine.GetName(), "failuredomain", capiAssignedFailuredomainName)
		if err := r.Client.Delete(ctx, scope.Machine); err != nil {
			return err
		}
	}

	return nil
}

// getInfraCluster fetches the CloudStackCluster for the given CloudStackMachine.
func (r *CloudStackMachineReconciler) getInfraCluster(ctx context.Context, cluster *clusterv1.Cluster, cloudStackMachine *infrav1.CloudStackMachine) (*infrav1.CloudStackCluster, error) {
	cloudStackCluster := &infrav1.CloudStackCluster{}
	cloudStackClusterName := client.ObjectKey{
		Namespace: cloudStackMachine.Namespace,
		Name:      cluster.Spec.InfrastructureRef.Name,
	}
	if err := r.Client.Get(ctx, cloudStackClusterName, cloudStackCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return cloudStackCluster, nil
}

// CloudStackClusterToCloudStackMachines is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// of CloudStackMachines.
func (r *CloudStackMachineReconciler) CloudStackClusterToCloudStackMachines(log logger.Wrapper) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []ctrl.Request {
		csCluster, ok := o.(*infrav1.CloudStackCluster)
		if !ok {
			klog.Errorf("expected a CloudStackCluster but got a %T", o)
		}

		log := log.WithValues("objectMapper", "cloudstackClusterToCloudStackMachine", "cluster", klog.KRef(csCluster.Namespace, csCluster.Name))

		// Don't handle deleted CloudStackClusters
		if !csCluster.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Trace("CloudStackCluster has a deletion timestamp, skipping mapping.")

			return nil
		}

		cluster, err := util.GetOwnerCluster(ctx, r.Client, csCluster.ObjectMeta)
		switch {
		case apierrors.IsNotFound(err) || cluster == nil:
			log.Trace("Cluster for CloudStackCluster not found, skipping mapping.")
			return nil
		case err != nil:
			log.Error(err, "Failed to get owning cluster, skipping mapping.")
			return nil
		}

		return r.requestsForCluster(ctx, log, cluster.Namespace, cluster.Name)
	}
}

func (r *CloudStackMachineReconciler) requeueCloudStackMachinesForUnpausedCluster(log logger.Wrapper) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []ctrl.Request {
		cluster, ok := o.(*clusterv1.Cluster)
		if !ok {
			klog.Errorf("expected a Cluster but got a %T", o)
		}

		log := log.WithValues("objectMapper", "clusterToCloudStackMachine", "cluster", klog.KRef(cluster.Namespace, cluster.Name))

		// Don't handle deleted clusters
		if !cluster.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Trace("Cluster has a deletion timestamp, skipping mapping.")
			return nil
		}

		return r.requestsForCluster(ctx, log, cluster.Namespace, cluster.Name)
	}
}

func (r *CloudStackMachineReconciler) requestsForCluster(ctx context.Context, log logger.Wrapper, namespace, name string) []ctrl.Request {
	machineList := &clusterv1.MachineList{}
	// list all the requested objects within the cluster namespace with the cluster name label
	if err := r.Client.List(ctx, machineList, client.InNamespace(namespace), client.MatchingLabels{clusterv1.ClusterNameLabel: name}); err != nil {
		log.Error(err, "Failed to get owned Machines, skipping mapping.")

		return nil
	}

	results := make([]ctrl.Request, 0, len(machineList.Items))
	for _, machine := range machineList.Items {
		m := machine
		log.WithValues("machine", klog.KObj(&m))
		if m.Spec.InfrastructureRef.GroupVersionKind().Kind != KindCloudStackMachine {
			log.Trace("Machine has an InfrastructureRef for a different type, will not add to reconciliation request.")

			continue
		}
		if m.Spec.InfrastructureRef.Name == "" {
			log.Trace("Machine has an InfrastructureRef with an empty name, will not add to reconciliation request.")

			continue
		}
		log.WithValues("cloudStackMachine", klog.KRef(m.Spec.InfrastructureRef.Namespace, m.Spec.InfrastructureRef.Name))
		log.Trace("Adding CloudStackMachine to reconciliation request.")
		results = append(results, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}})
	}

	return results
}

// CloudStackIsolatedNetworkToControlPlaneCloudStackMachines is a handler.ToRequestsFunc to be used to enqueue requests for reconciliation
// of CloudStackMachines that are part of the control plane.
func (r *CloudStackMachineReconciler) CloudStackIsolatedNetworkToControlPlaneCloudStackMachines(log logger.Wrapper) handler.MapFunc {
	return func(ctx context.Context, o client.Object) []ctrl.Request {
		csIsoNet, ok := o.(*infrav1.CloudStackIsolatedNetwork)
		if !ok {
			klog.Errorf("expected a CloudStackIsolatedNetwork but got a %T", o)

			return nil
		}

		log := log.WithValues("objectMapper", "cloudStackIsolatedNetworkToControlPlaneCloudStackMachines", "isonet", klog.KRef(csIsoNet.Namespace, csIsoNet.Name))

		// Don't handle deleted CloudStackIsolatedNetworks
		if !csIsoNet.ObjectMeta.DeletionTimestamp.IsZero() {
			log.Trace("CloudStackIsolatedNetwork has a deletion timestamp, skipping mapping.")

			return nil
		}

		clusterName, ok := csIsoNet.GetLabels()[clusterv1.ClusterNameLabel]
		if !ok {
			log.Error(errors.New("failed to find cluster name label"), "CloudStackIsolatedNetwork is missing cluster name label or cluster does not exist, skipping mapping.")
		}

		machineList := &clusterv1.MachineList{}
		// list all the requested objects within the cluster namespace with the cluster name and control plane label.
		if err := r.Client.List(ctx, machineList, client.InNamespace(csIsoNet.Namespace), client.MatchingLabels{
			clusterv1.ClusterNameLabel:         clusterName,
			clusterv1.MachineControlPlaneLabel: "",
		}); err != nil {
			log.Error(err, "Failed to get owned control plane Machines, skipping mapping.")

			return nil
		}

		log.Trace("Looked up members with control plane label", "found", len(machineList.Items))

		results := make([]ctrl.Request, 0, len(machineList.Items))
		for _, machine := range machineList.Items {
			m := machine
			log.WithValues("machine", klog.KObj(&m))
			if m.Spec.InfrastructureRef.GroupVersionKind().Kind != KindCloudStackMachine {
				log.Trace("Machine has an InfrastructureRef for a different type, will not add to reconciliation request.")

				continue
			}
			if m.Spec.InfrastructureRef.Name == "" {
				log.Trace("Machine has an InfrastructureRef with an empty name, will not add to reconciliation request.")

				continue
			}
			log.WithValues("cloudStackMachine", klog.KRef(m.Spec.InfrastructureRef.Namespace, m.Spec.InfrastructureRef.Name))
			log.Trace("Adding CloudStackMachine to reconciliation request.")
			results = append(results, ctrl.Request{NamespacedName: client.ObjectKey{Namespace: m.Namespace, Name: m.Spec.InfrastructureRef.Name}})
		}

		return results
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *CloudStackMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, options controller.Options) error {
	log := logger.FromContext(ctx)
	cloudStackClusterToCloudStackMachinesMapper := r.CloudStackClusterToCloudStackMachines(log)
	requeueCloudStackMachinesForUnpausedCluster := r.requeueCloudStackMachinesForUnpausedCluster(log)
	cloudStackIsolatedNetworkToControlPlaneCloudStackMachines := r.CloudStackIsolatedNetworkToControlPlaneCloudStackMachines(log)

	err := ctrl.NewControllerManagedBy(mgr).
		For(&infrav1.CloudStackMachine{}).
		WithOptions(options).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind(KindCloudStackMachine))),
		).
		Watches(
			&infrav1.CloudStackCluster{},
			handler.EnqueueRequestsFromMapFunc(cloudStackClusterToCloudStackMachinesMapper),
		).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(log.GetLogger(), r.WatchFilterValue)).
		WithEventFilter(
			predicate.Funcs{
				// Avoid reconciling if the event triggering the reconciliation is related to incremental status updates
				// for CloudStackMachine resources only
				UpdateFunc: func(e event.UpdateEvent) bool {
					if e.ObjectOld.GetObjectKind().GroupVersionKind().Kind != KindCloudStackMachine {
						return true
					}

					oldMachine := e.ObjectOld.(*infrav1.CloudStackMachine).DeepCopy()
					newMachine := e.ObjectNew.(*infrav1.CloudStackMachine).DeepCopy()

					oldMachine.Status = infrav1.CloudStackMachineStatus{}
					newMachine.Status = infrav1.CloudStackMachineStatus{}

					oldMachine.ObjectMeta.ResourceVersion = ""
					newMachine.ObjectMeta.ResourceVersion = ""

					return !cmp.Equal(oldMachine, newMachine)
				},
			},
		).
		Watches(
			// Watch for cluster pause/unpause events
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(requeueCloudStackMachinesForUnpausedCluster),
			builder.WithPredicates(predicates.ClusterUnpausedAndInfrastructureReady(log.GetLogger())),
		).
		Watches(
			// This watch is here to assign VM's to loadbalancer rules
			&infrav1.CloudStackIsolatedNetwork{},
			handler.EnqueueRequestsFromMapFunc(cloudStackIsolatedNetworkToControlPlaneCloudStackMachines),
			builder.WithPredicates(
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						oldCSIsoNet, ok := e.ObjectOld.(*infrav1.CloudStackIsolatedNetwork)
						if !ok {
							log.Trace("Expected CloudStackIsolatedNetwork", "type", fmt.Sprintf("%T", e.ObjectOld))

							return false
						}

						newCSIsoNet := e.ObjectNew.(*infrav1.CloudStackIsolatedNetwork)

						// We're only interested in status updates, not Spec updates
						if oldCSIsoNet.Generation != newCSIsoNet.Generation {
							return false
						}

						// Only trigger a CloudStackMachine reconcile if the loadbalancer rules changed.
						return len(oldCSIsoNet.Status.LoadBalancerRuleIDs) != len(newCSIsoNet.Status.LoadBalancerRuleIDs)
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
