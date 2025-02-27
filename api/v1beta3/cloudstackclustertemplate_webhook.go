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

package v1beta3

import (
	"fmt"
	"reflect"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/webhookutil"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

const cloudStackClusterTemplateImmutableMsg = "CloudStackClusterTemplate spec.template.spec field is immutable. Please create new resource instead."

func (r *CloudStackClusterTemplate) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackclustertemplate,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclustertemplates,verbs=create;update,versions=v1beta3,name=mcloudstackclustertemplate.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &CloudStackClusterTemplate{}

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *CloudStackClusterTemplate) Default() {
	defaultCloudStackClusterSpec(&r.Spec.Template.Spec)
}

//+kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackclustertemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclustertemplates,verbs=create;update,versions=v1beta3,name=vcloudstackclustertemplate.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &CloudStackClusterTemplate{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplate) ValidateCreate() (admission.Warnings, error) {
	allErrs := validateCloudStackClusterSpec(r.Spec.Template.Spec)

	return nil, webhookutil.AggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplate) ValidateUpdate(oldRaw runtime.Object) (admission.Warnings, error) {
	var allErrs field.ErrorList
	old, ok := oldRaw.(*CloudStackClusterTemplate)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackClusterTemplate but got a %T", oldRaw))
	}

	if !reflect.DeepEqual(r.Spec.Template.Spec, old.Spec.Template.Spec) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec"), r,
			cloudStackClusterTemplateImmutableMsg))
	}

	return nil, webhookutil.AggregateObjErrors(r.GroupVersionKind().GroupKind(), r.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplate) ValidateDelete() (admission.Warnings, error) {
	return nil, nil
}
