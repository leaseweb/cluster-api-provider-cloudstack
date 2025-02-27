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

package v1beta3

import (
	"context"
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/webhookutil"
)

type CloudStackMachineWebhook struct{}

func (r *CloudStackMachineWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&CloudStackMachine{}).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackmachine,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachines,versions=v1beta3,name=validation.cloudstackmachine.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var _ webhook.CustomValidator = &CloudStackMachineWebhook{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineWebhook) ValidateCreate(_ context.Context, objRaw runtime.Object) (admission.Warnings, error) {
	obj, ok := objRaw.(*CloudStackMachine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackMachine but got a %T", objRaw))
	}

	var errorList field.ErrorList

	errorList = webhookutil.EnsureAtLeastOneFieldExists(obj.Spec.Offering.ID, obj.Spec.Offering.Name, "Offering", errorList)
	errorList = webhookutil.EnsureAtLeastOneFieldExists(obj.Spec.Template.ID, obj.Spec.Template.Name, "Template", errorList)
	if obj.Spec.DiskOffering != nil && (obj.Spec.DiskOffering.ID != "" || obj.Spec.DiskOffering.Name != "") {
		errorList = webhookutil.EnsureIntFieldsAreNotNegative(obj.Spec.DiskOffering.CustomSize, "customSizeInGB", errorList)
	}

	return nil, webhookutil.AggregateObjErrors(obj.GroupVersionKind().GroupKind(), obj.Name, errorList)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineWebhook) ValidateUpdate(_ context.Context, oldRaw runtime.Object, newRaw runtime.Object) (admission.Warnings, error) {
	oldObj, ok := oldRaw.(*CloudStackMachine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackMachine but got a %T", oldRaw))
	}
	newObj, ok := newRaw.(*CloudStackMachine)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackMachine but got a %T", newRaw))
	}
	var errorList field.ErrorList

	oldSpec := oldObj.Spec

	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.Offering.ID, oldSpec.Offering.ID, "offering", errorList)
	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.Offering.Name, oldSpec.Offering.Name, "offering", errorList)
	if newObj.Spec.DiskOffering != nil {
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.ID, oldSpec.DiskOffering.ID, "diskOffering", errorList)
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.Name, oldSpec.DiskOffering.Name, "diskOffering", errorList)
		errorList = webhookutil.EnsureIntFieldsAreNotNegative(newObj.Spec.DiskOffering.CustomSize, "customSizeInGB", errorList)
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.MountPath, oldSpec.DiskOffering.MountPath, "mountPath", errorList)
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.Device, oldSpec.DiskOffering.Device, "device", errorList)
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.Filesystem, oldSpec.DiskOffering.Filesystem, "filesystem", errorList)
		errorList = webhookutil.EnsureEqualStrings(newObj.Spec.DiskOffering.Label, oldSpec.DiskOffering.Label, "label", errorList)
	}
	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.SSHKey, oldSpec.SSHKey, "sshkey", errorList)
	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.Template.ID, oldSpec.Template.ID, "template", errorList)
	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.Template.Name, oldSpec.Template.Name, "template", errorList)
	errorList = webhookutil.EnsureEqualMapStringString(newObj.Spec.Details, oldSpec.Details, "details", errorList)
	errorList = webhookutil.EnsureEqualStrings(newObj.Spec.Affinity, oldSpec.Affinity, "affinity", errorList)

	if !reflect.DeepEqual(newObj.Spec.AffinityGroupIDs, oldSpec.AffinityGroupIDs) { // Equivalent to other Ensure funcs.
		errorList = append(errorList, field.Forbidden(field.NewPath("spec", "AffinityGroupIDs"), "AffinityGroupIDs"))
	}

	return nil, webhookutil.AggregateObjErrors(newObj.GroupVersionKind().GroupKind(), newObj.Name, errorList)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
