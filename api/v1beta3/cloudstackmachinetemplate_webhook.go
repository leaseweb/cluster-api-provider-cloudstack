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
	"fmt"
	"reflect"
	"strings"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/webhookutil"
)

const cloudStackMachineTemplateImmutableMsg = "CloudStackMachineTemplate spec.template.spec field is immutable. Please create new resource instead."

// log is for logging in this package.
var cloudstackmachinetemplatelog = logf.Log.WithName("cloudstackmachinetemplate-resource")

func (r *CloudStackMachineTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackmachinetemplate,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachinetemplates,versions=v1beta3,name=validation.cloudstackmachinetemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackmachinetemplate,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackmachinetemplates,versions=v1beta3,name=default.cloudstackmachinetemplate.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var (
	_ webhook.Defaulter = &CloudStackMachineTemplate{}
	_ webhook.Validator = &CloudStackMachineTemplate{}
)

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *CloudStackMachineTemplate) Default() {
	cloudstackmachinetemplatelog.V(1).Info("entered default setting webhook", "api resource name", r.Name)
	// No defaulted values supported yet.
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineTemplate) ValidateCreate() (admission.Warnings, error) {
	cloudstackmachinetemplatelog.V(1).Info("entered validate create webhook", "api resource name", r.Name)

	var errorList field.ErrorList

	// CloudStackMachineTemplateSpec.CloudStackMachineSpec.
	spec := r.Spec.Template.Spec

	affinity := strings.ToLower(spec.Affinity)
	if !(affinity == "" || affinity == "no" || affinity == "pro" || affinity == "anti" || affinity == "soft-pro" || affinity == "soft-anti") {
		errorList = append(errorList, field.Invalid(field.NewPath("spec", "Affinity"), spec.Affinity,
			`Affinity must be "no", "pro", "anti", "soft-pro", "soft-anti", or unspecified.`))
	}
	if affinity != "no" && affinity != "" && len(spec.AffinityGroupIDs) > 0 {
		errorList = append(errorList, field.Forbidden(field.NewPath("spec", "AffinityGroupIDs"),
			"AffinityGroupIDs cannot be specified when Affinity is specified as anything but `no`"))
	}

	errorList = webhookutil.EnsureAtLeastOneFieldExists(spec.Offering.ID, spec.Offering.Name, "Offering", errorList)
	errorList = webhookutil.EnsureAtLeastOneFieldExists(spec.Template.ID, spec.Template.Name, "Template", errorList)

	return nil, webhookutil.AggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, errorList)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineTemplate) ValidateUpdate(oldRaw runtime.Object) (admission.Warnings, error) {
	cloudstackmachinetemplatelog.V(1).Info("entered validate update webhook", "api resource name", r.Name)

	var allErrs field.ErrorList
	old, ok := oldRaw.(*CloudStackMachineTemplate)
	if !ok {
		return nil, errors.NewBadRequest(fmt.Sprintf("expected a CloudStackMachineTemplate but got a %T", oldRaw))
	}

	if !reflect.DeepEqual(r.Spec.Template.Spec, old.Spec.Template.Spec) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec"), r,
			cloudStackMachineTemplateImmutableMsg))
	}

	return nil, webhookutil.AggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackMachineTemplate) ValidateDelete() (admission.Warnings, error) {
	cloudstackmachinetemplatelog.V(1).Info("entered validate delete webhook", "api resource name", r.Name)
	// No deletion validations.  Deletion webhook not enabled.
	return nil, nil
}
