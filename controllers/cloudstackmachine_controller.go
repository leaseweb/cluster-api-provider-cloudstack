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

package controllers

import (
	"context"
	"fmt"
	"math/rand"
	"reflect"
	"regexp"
	"strings"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
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
	"sigs.k8s.io/cluster-api-provider-cloudstack/controllers/utils"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
)

var (
	hostnameMatcher      = regexp.MustCompile(`\{\{\s*ds\.meta_data\.hostname\s*\}\}`)
	failuredomainMatcher = regexp.MustCompile(`ds\.meta_data\.failuredomain`)
)

const (
	BootstrapDataNotReady                      = "Bootstrap DataSecretName not yet available"
	CSMachineCreationSuccess                   = "CloudStack instance Created"
	CSMachineCreationFailed                    = "Creating CloudStack machine failed: %s"
	MachineInstanceRunning                     = "Machine instance is Running..."
	MachineInErrorMessage                      = "CloudStackMachine VM in error state. Deleting associated Machine"
	MachineNotReadyMessage                     = "Instance not ready, is %s"
	CSMachineStateCheckerCreationFailed        = "error encountered when creating CloudStackMachineStateChecker"
	CSMachineStateCheckerCreationSuccess       = "CloudStackMachineStateChecker created"
	CSMachineDeletionMessage                   = "Deleting CloudStack Machine %s"
	CSMachineDeletionInstanceIDNotFoundMessage = "Deleting CloudStack Machine %s instanceID not found"
)

// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines/finalizers,verbs=update
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machines;machines/status,verbs=get;list;watch
// +kubebuilder:rbac:groups=cluster.x-k8s.io,resources=machinesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=controlplane.cluster.x-k8s.io,resources=kubeadmcontrolplanes,verbs=get;list;watch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// CloudStackMachineReconciliationRunner is a ReconciliationRunner with extensions specific to CloudStack machine reconciliation.
type CloudStackMachineReconciliationRunner struct {
	*utils.ReconciliationRunner
	ReconciliationSubject *infrav1.CloudStackMachine
	CAPIMachine           *clusterv1.Machine
	StateChecker          *infrav1.CloudStackMachineStateChecker
	FailureDomain         *infrav1.CloudStackFailureDomain
	IsoNet                *infrav1.CloudStackIsolatedNetwork
	AffinityGroup         *infrav1.CloudStackAffinityGroup
}

// CloudStackMachineReconciler reconciles a CloudStackMachine object..
type CloudStackMachineReconciler struct {
	utils.ReconcilerBase
}

// Initialize a new CloudStackMachine reconciliation runner with concrete types and initialized member fields.
func NewCSMachineReconciliationRunner() *CloudStackMachineReconciliationRunner {
	// Set concrete type and init pointers.
	r := &CloudStackMachineReconciliationRunner{ReconciliationSubject: &infrav1.CloudStackMachine{}}
	r.CAPIMachine = &clusterv1.Machine{}
	r.StateChecker = &infrav1.CloudStackMachineStateChecker{}
	r.IsoNet = &infrav1.CloudStackIsolatedNetwork{}
	r.AffinityGroup = &infrav1.CloudStackAffinityGroup{}
	r.FailureDomain = &infrav1.CloudStackFailureDomain{}
	// Set up the base runner. Initializes pointers and links reconciliation methods.
	r.ReconciliationRunner = utils.NewRunner(r, r.ReconciliationSubject, "CloudStackMachine")

	return r
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (reconciler *CloudStackMachineReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r := NewCSMachineReconciliationRunner()
	r.UsingBaseReconciler(reconciler.ReconcilerBase).ForRequest(req).WithRequestCtx(ctx)
	r.WithAdditionalCommonStages(
		r.RunIf(func() bool { return r.ReconciliationSubject.GetDeletionTimestamp().IsZero() }, r.GetParent(r.ReconciliationSubject, r.CAPIMachine)),
		r.RequeueIfCloudStackClusterNotReady,
		r.SetFailureDomainOnCSMachine,
		r.GetFailureDomainByName(func() string { return r.ReconciliationSubject.Spec.FailureDomainName }, r.FailureDomain),
		r.AsFailureDomainUser(&r.FailureDomain.Spec))
	res, err := r.RunBaseReconciliationStages()
	r.Log.V(1).Info("Reconciliation finished.")

	return res, err
}

func (r *CloudStackMachineReconciliationRunner) Reconcile() (ctrl.Result, error) {
	return r.RunReconciliationStages(
		r.DeleteMachineIfFailuredomainNotExist,
		r.GetObjectByName("placeholder", r.IsoNet,
			func() string { return r.IsoNetMetaName(r.FailureDomain.Spec.Zone.Network.Name) }),
		r.RunIf(func() bool { return r.FailureDomain.Spec.Zone.Network.Type == cloud.NetworkTypeIsolated },
			r.CheckPresent(map[string]client.Object{"CloudStackIsolatedNetwork": r.IsoNet})),
		r.ConsiderAffinity,
		r.GetOrCreateVMInstance,
		r.RequeueIfInstanceNotRunning,
		r.RunIf(func() bool { return !annotations.IsExternallyManaged(r.CSCluster) }, r.AddToLBIfNeeded),
		r.GetOrCreateMachineStateChecker,
	)
}

// ConsiderAffinity sets machine affinity if needed. It also creates or gets an affinity group resource if required and
// checks it for readiness.
func (r *CloudStackMachineReconciliationRunner) ConsiderAffinity() (ctrl.Result, error) {
	if r.ReconciliationSubject.Spec.Affinity == infrav1.AffinityTypeNo ||
		r.ReconciliationSubject.Spec.Affinity == "" { // No managed affinity.
		return ctrl.Result{}, nil
	}
	var agName string
	var err error

	if r.ReconciliationSubject.Spec.AffinityGroupRef != nil {
		agName = r.ReconciliationSubject.Spec.AffinityGroupRef.Name
	} else {
		agName, err = utils.GenerateAffinityGroupName(*r.ReconciliationSubject, r.CAPIMachine, r.CAPICluster)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	// Set failure domain name and owners.
	r.AffinityGroup.Spec.FailureDomainName = r.ReconciliationSubject.Spec.FailureDomainName
	res, err := r.GetOrCreateAffinityGroup(
		agName, r.ReconciliationSubject.Spec.Affinity, r.AffinityGroup, r.FailureDomain)()
	if r.ShouldReturn(res, err) {
		return res, err
	}
	// Set affinity group reference.
	r.ReconciliationSubject.Spec.AffinityGroupRef = &corev1.ObjectReference{
		Kind:      r.AffinityGroup.Kind,
		UID:       r.AffinityGroup.UID,
		Name:      r.AffinityGroup.Name,
		Namespace: r.AffinityGroup.Namespace,
	}
	if !r.AffinityGroup.Status.Ready {
		return r.RequeueWithMessage("Required affinity group not ready.")
	}

	return ctrl.Result{}, nil
}

// SetFailureDomainOnCSMachine sets the failure domain the machine should launch in.
func (r *CloudStackMachineReconciliationRunner) SetFailureDomainOnCSMachine() (ctrl.Result, error) {
	if r.ReconciliationSubject.Spec.FailureDomainName == "" {
		var name string
		// CAPIMachine is null if it's been deleted but we're still reconciling the CS machine.
		if r.CAPIMachine != nil && r.CAPIMachine.Spec.FailureDomain != nil &&
			(util.IsControlPlaneMachine(r.CAPIMachine) || // Is control plane machine -- CAPI will specify.
				*r.CAPIMachine.Spec.FailureDomain != "") { // Or potentially another machine controller specified.
			name = *r.CAPIMachine.Spec.FailureDomain
			r.ReconciliationSubject.Spec.FailureDomainName = *r.CAPIMachine.Spec.FailureDomain
		} else { // Not a control plane machine. Place randomly.
			rand := rand.New(rand.NewSource(time.Now().Unix()))            // #nosec G404 -- weak crypt rand doesn't matter here.
			randNum := (rand.Int() % len(r.CSCluster.Spec.FailureDomains)) // #nosec G404 -- weak crypt rand doesn't matter here.
			name = r.CSCluster.Spec.FailureDomains[randNum].Name
		}
		r.ReconciliationSubject.Spec.FailureDomainName = name
		r.ReconciliationSubject.Labels[infrav1.FailureDomainLabelName] = infrav1.FailureDomainHashedMetaName(name, r.CAPICluster.Name)
	}

	return ctrl.Result{}, nil
}

// DeleteMachineIfFailuredomainNotExist delete CAPI machine if machine is deployed in a failuredomain that does not exist anymore.
func (r *CloudStackMachineReconciliationRunner) DeleteMachineIfFailuredomainNotExist() (ctrl.Result, error) {
	if r.CAPIMachine.Spec.FailureDomain == nil {
		return ctrl.Result{}, nil
	}
	capiAssignedFailuredomainName := *r.CAPIMachine.Spec.FailureDomain
	exist := false
	for _, fd := range r.CSCluster.Spec.FailureDomains {
		if capiAssignedFailuredomainName == fd.Name {
			exist = true

			break
		}
	}
	if !exist {
		r.Log.Info("CAPI Machine in non-existent failuredomain. Deleting associated Machine.", "csMachine", r.ReconciliationSubject.GetName(), "failuredomain", capiAssignedFailuredomainName)
		if err := r.K8sClient.Delete(r.RequestCtx, r.CAPIMachine); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// GetOrCreateVMInstance gets or creates a VM instance.
// Implicitly it also fetches its bootstrap secret in order to create said instance.
func (r *CloudStackMachineReconciliationRunner) GetOrCreateVMInstance() (ctrl.Result, error) {
	if r.CAPIMachine.Spec.Bootstrap.DataSecretName == nil {
		r.Recorder.Event(r.ReconciliationSubject, "Normal", "Creating", BootstrapDataNotReady)

		return r.RequeueWithMessage(BootstrapDataNotReady + ".")
	}

	// Get the kubeadm bootstrap secret for this machine.
	secret := &corev1.Secret{}
	key := types.NamespacedName{Namespace: r.CAPIMachine.Namespace, Name: *r.CAPIMachine.Spec.Bootstrap.DataSecretName}
	if err := r.K8sClient.Get(context.TODO(), key, secret); err != nil {
		return ctrl.Result{}, err
	}
	data, present := secret.Data["value"]
	if !present {
		return ctrl.Result{}, errors.New("bootstrap secret data not yet set")
	}

	userData := processCustomMetadata(data, r)
	err := r.CSUser.GetOrCreateVMInstance(r.ReconciliationSubject, r.CAPIMachine, r.FailureDomain, r.AffinityGroup, userData)
	if err != nil {
		r.Log.Error(err, "GetOrCreateVMInstance returned error")
		r.Recorder.Eventf(r.ReconciliationSubject, "Warning", "Creating", CSMachineCreationFailed, err.Error())
	}
	if err == nil && !controllerutil.ContainsFinalizer(r.ReconciliationSubject, infrav1.MachineFinalizer) { // Fetched or Created?
		// Adding a finalizer will make reconcile-delete try to destroy the associated VM through instanceID.
		// If err is not nil, it means CAPC could not get an associated VM through instanceID or name, so we should not add a finalizer to this CloudStackMachine,
		// Otherwise, reconcile-delete will be stuck trying to wait for instanceID to be available.
		controllerutil.AddFinalizer(r.ReconciliationSubject, infrav1.MachineFinalizer)
		r.Recorder.Eventf(r.ReconciliationSubject, "Normal", "Created", CSMachineCreationSuccess)
		r.Log.Info(CSMachineCreationSuccess, "instanceStatus", r.ReconciliationSubject.Status)
	}

	return ctrl.Result{}, err
}

func processCustomMetadata(data []byte, r *CloudStackMachineReconciliationRunner) string {
	// since cloudstack metadata does not allow custom data added into meta_data, following line is a workaround to specify a hostname name
	// {{ ds.meta_data.hostname }} is expected to be used as a node name when kubelet register a node
	userData := hostnameMatcher.ReplaceAllString(string(data), r.CAPIMachine.Name)
	userData = failuredomainMatcher.ReplaceAllString(userData, r.FailureDomain.Spec.Name)

	return userData
}

// RequeueIfInstanceNotRunning checks the Instance's status for running state and requeues otherwise.
func (r *CloudStackMachineReconciliationRunner) RequeueIfInstanceNotRunning() (ctrl.Result, error) {
	switch r.ReconciliationSubject.Status.InstanceState {
	case cloud.VMStateRunning:
		// Only emit this event when relevant.
		if !r.ReconciliationSubject.Status.Ready {
			r.Recorder.Event(r.ReconciliationSubject, "Normal", cloud.VMStateRunning, MachineInstanceRunning)
			r.Log.Info(MachineInstanceRunning)
		}
		r.ReconciliationSubject.Status.Ready = true
	case cloud.VMStateError:
		r.Recorder.Event(r.ReconciliationSubject, "Warning", cloud.VMStateError, MachineInErrorMessage)
		r.Log.Info(MachineInErrorMessage, "csMachine", r.ReconciliationSubject.GetName())
		if err := r.K8sClient.Delete(r.RequestCtx, r.CAPIMachine); err != nil {
			return ctrl.Result{}, err
		}

		return ctrl.Result{RequeueAfter: utils.RequeueTimeout}, nil
	default:
		r.Recorder.Eventf(r.ReconciliationSubject, "Warning", r.ReconciliationSubject.Status.InstanceState, MachineNotReadyMessage, r.ReconciliationSubject.Status.InstanceState)
		r.Log.Info(fmt.Sprintf(MachineNotReadyMessage, r.ReconciliationSubject.Status.InstanceState))

		return ctrl.Result{RequeueAfter: utils.RequeueTimeout}, nil
	}

	return ctrl.Result{}, nil
}

// AddToLBIfNeeded adds instance to load balancer if it is a control plane node in an isolated network, and the load balancer is enabled.
func (r *CloudStackMachineReconciliationRunner) AddToLBIfNeeded() (ctrl.Result, error) {
	if util.IsControlPlaneMachine(r.CAPIMachine) &&
		r.FailureDomain.Spec.Zone.Network.Type == cloud.NetworkTypeIsolated &&
		r.CSCluster.Spec.APIServerLoadBalancer.IsEnabled() {
		r.Log.V(4).Info("Assigning VM to load balancer rule.")
		if r.IsoNet.Spec.Name == "" {
			return r.RequeueWithMessage("Could not get required Isolated Network for VM, requeueing.")
		}
		err := r.CSUser.AssignVMToLoadBalancerRules(r.IsoNet, *r.ReconciliationSubject.Spec.InstanceID)
		if err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

// GetOrCreateMachineStateChecker gets or creates a CloudStackMachineStateChecker object.
func (r *CloudStackMachineReconciliationRunner) GetOrCreateMachineStateChecker() (ctrl.Result, error) {
	checkerName := r.ReconciliationSubject.Spec.InstanceID
	if checkerName == nil {
		return ctrl.Result{}, errors.New(CSMachineStateCheckerCreationFailed)
	}

	csMachineStateChecker := &infrav1.CloudStackMachineStateChecker{}
	objectKey := client.ObjectKey{Name: strings.ToLower(*checkerName), Namespace: r.Request.Namespace}
	err := r.K8sClient.Get(r.RequestCtx, objectKey, csMachineStateChecker)
	if err != nil {
		if k8serrors.IsNotFound(err) {
			csMachineStateChecker.ObjectMeta = r.NewChildObjectMeta(*checkerName)
			csMachineStateChecker.Spec = infrav1.CloudStackMachineStateCheckerSpec{InstanceID: *checkerName}
			csMachineStateChecker.Status = infrav1.CloudStackMachineStateCheckerStatus{Ready: false}

			if err := r.K8sClient.Create(r.RequestCtx, csMachineStateChecker); err != nil {
				r.Recorder.Eventf(r.ReconciliationSubject, "Warning", "Machine State Checker", CSMachineStateCheckerCreationFailed)

				return r.ReturnWrappedError(err, CSMachineStateCheckerCreationFailed)
			}
			r.Recorder.Eventf(r.ReconciliationSubject, "Normal", "Machine State Checker", CSMachineStateCheckerCreationSuccess)
		} else {
			r.Recorder.Eventf(r.ReconciliationSubject, "Warning", "Machine State Checker", CSMachineStateCheckerCreationFailed)

			return ctrl.Result{}, errors.Wrap(err, CSMachineStateCheckerCreationFailed)
		}
	}
	r.StateChecker = csMachineStateChecker

	return ctrl.Result{}, nil
}

func (r *CloudStackMachineReconciliationRunner) ReconcileDelete() (ctrl.Result, error) {
	if r.ReconciliationSubject.Spec.InstanceID == nil {
		// InstanceID is not set until deploying VM finishes which can take minutes, and CloudStack Machine can be deleted before VM deployment complete.
		// ResolveVMInstanceDetails can get InstanceID by CS machine name
		err := r.CSClient.ResolveVMInstanceDetails(r.ReconciliationSubject)
		if err != nil {
			r.ReconciliationSubject.Status.Status = ptr.To(metav1.StatusFailure)
			r.ReconciliationSubject.Status.Reason = ptr.To(err.Error() +
				fmt.Sprintf(" If this VM has already been deleted, please remove the finalizer named %s from object %s",
					"cloudstackmachine.infrastructure.cluster.x-k8s.io", r.ReconciliationSubject.Name))
			// Cloudstack VM may be not found or more than one found by name
			r.Recorder.Eventf(r.ReconciliationSubject, "Warning", "Deleting", CSMachineDeletionInstanceIDNotFoundMessage, r.ReconciliationSubject.Name)
			r.Log.Error(err, fmt.Sprintf(CSMachineDeletionInstanceIDNotFoundMessage, r.ReconciliationSubject.Name))

			return ctrl.Result{}, err
		}
	}
	r.Recorder.Eventf(r.ReconciliationSubject, "Normal", "Deleting", CSMachineDeletionMessage, r.ReconciliationSubject.Name)
	r.Log.Info("Deleting instance", "instance-id", r.ReconciliationSubject.Spec.InstanceID)
	// Use CSClient instead of CSUser here to expunge as admin.
	// The CloudStack-Go API does not return an error, but the VM won't delete with Expunge set if requested by
	// non-domain admin user.
	if err := r.CSClient.DestroyVMInstance(r.ReconciliationSubject); err != nil {
		if err.Error() == "VM deletion in progress" {
			r.Log.Info(err.Error())

			return ctrl.Result{RequeueAfter: utils.DestroyVMRequeueInterval}, nil
		}

		return ctrl.Result{}, err
	}

	controllerutil.RemoveFinalizer(r.ReconciliationSubject, infrav1.MachineFinalizer)
	r.Log.Info("VM Deleted", "instanceID", r.ReconciliationSubject.Spec.InstanceID)

	return ctrl.Result{}, nil
}

// SetupWithManager registers the machine reconciler to the CAPI controller manager.
func (reconciler *CloudStackMachineReconciler) SetupWithManager(ctx context.Context, mgr ctrl.Manager, opts controller.Options) error {
	log := ctrl.LoggerFrom(ctx)

	reconciler.Recorder = mgr.GetEventRecorderFor("capc-machine-controller")

	cloudStackClusterToCloudStackMachines := utils.CloudStackClusterToCloudStackMachines(reconciler.K8sClient, log)

	csMachineMapper, err := util.ClusterToTypedObjectsMapper(reconciler.K8sClient, &infrav1.CloudStackMachineList{}, reconciler.Scheme)
	if err != nil {
		return errors.Wrap(err, "failed to create mapper for Cluster to CloudStackMachines")
	}

	cloudStackIsolatedNetworkToControlPlaneCloudStackMachines := utils.CloudStackIsolatedNetworkToControlPlaneCloudStackMachines(reconciler.K8sClient, log)

	err = ctrl.NewControllerManagedBy(mgr).
		WithOptions(opts).
		For(&infrav1.CloudStackMachine{}).
		Watches(
			&clusterv1.Machine{},
			handler.EnqueueRequestsFromMapFunc(util.MachineToInfrastructureMapFunc(infrav1.GroupVersion.WithKind("CloudStackMachine"))),
			builder.WithPredicates(
				predicate.Funcs{
					UpdateFunc: func(e event.UpdateEvent) bool {
						oldMachine, ok := e.ObjectOld.(*clusterv1.Machine)
						if !ok {
							log.V(4).Info("Expected Machine", "type", fmt.Sprintf("%T", e.ObjectOld))

							return false
						}
						newMachine := e.ObjectNew.(*clusterv1.Machine)

						return (oldMachine.Spec.Bootstrap.DataSecretName == nil && newMachine.Spec.Bootstrap.DataSecretName != nil)
					},
				},
			),
		).
		Watches(
			&infrav1.CloudStackCluster{},
			handler.EnqueueRequestsFromMapFunc(cloudStackClusterToCloudStackMachines),
		).
		WithEventFilter(predicates.ResourceNotPausedAndHasFilterLabel(ctrl.LoggerFrom(ctx), reconciler.WatchFilterValue)).
		WithEventFilter(
			predicate.Funcs{
				UpdateFunc: func(e event.UpdateEvent) bool {
					// Avoid reconciling if the event triggering the reconciliation is related to incremental status updates
					// for CloudStackMachine resources only
					if _, ok := e.ObjectOld.(*infrav1.CloudStackMachine); !ok {
						return true
					}

					oldMachine := e.ObjectOld.(*infrav1.CloudStackMachine).DeepCopy()
					newMachine := e.ObjectNew.(*infrav1.CloudStackMachine).DeepCopy()
					// Ignore resource version because they are unique
					oldMachine.ObjectMeta.ResourceVersion = ""
					newMachine.ObjectMeta.ResourceVersion = ""
					// Ignore generation because it's not used in reconcile
					oldMachine.ObjectMeta.Generation = 0
					newMachine.ObjectMeta.Generation = 0
					// Ignore finalizers updates
					oldMachine.ObjectMeta.Finalizers = nil
					newMachine.ObjectMeta.Finalizers = nil
					// Ignore ManagedFields because they are mirror of ObjectMeta
					oldMachine.ManagedFields = nil
					newMachine.ManagedFields = nil
					// Ignore incremental status updates
					oldMachine.Status = infrav1.CloudStackMachineStatus{}
					newMachine.Status = infrav1.CloudStackMachineStatus{}
					// Ignore provider ID
					oldMachine.Spec.ProviderID = nil
					newMachine.Spec.ProviderID = nil
					// Ignore instance ID
					oldMachine.Spec.InstanceID = nil
					newMachine.Spec.InstanceID = nil

					return !reflect.DeepEqual(oldMachine, newMachine)
				},
			},
		).
		Watches(
			&clusterv1.Cluster{},
			handler.EnqueueRequestsFromMapFunc(csMachineMapper),
			builder.WithPredicates(
				predicates.ClusterUnpausedAndInfrastructureReady(ctrl.LoggerFrom(ctx)),
			),
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
							log.V(4).Info("Expected CloudStackIsolatedNetwork", "type", fmt.Sprintf("%T", e.ObjectOld))

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
		Complete(reconciler)
	if err != nil {
		return errors.Wrap(err, "failed setting up with a controller manager")
	}

	return nil
}
