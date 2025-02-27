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
	"context"
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

type CloudStackClusterTemplateWebhook struct{}

func (r *CloudStackClusterTemplateWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&CloudStackClusterTemplate{}).
		WithDefaulter(r).
		WithValidator(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackclustertemplate,mutating=true,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclustertemplates,verbs=create;update,versions=v1beta3,name=mcloudstackclustertemplate.kb.io,admissionReviewVersions=v1

var _ webhook.CustomDefaulter = &CloudStackClusterTemplateWebhook{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the type.
func (r *CloudStackClusterTemplateWebhook) Default(ctx context.Context, objRaw runtime.Object) error {
	obj, ok := objRaw.(*CloudStackClusterTemplate)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackClusterTemplate but got a %T", objRaw))
	}
	defaultCloudStackClusterSpec(&obj.Spec.Template.Spec)

	return nil
}

//+kubebuilder:webhook:path=/validate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackclustertemplate,mutating=false,failurePolicy=fail,sideEffects=None,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclustertemplates,verbs=create;update,versions=v1beta3,name=vcloudstackclustertemplate.kb.io,admissionReviewVersions=v1

var _ webhook.CustomValidator = &CloudStackClusterTemplateWebhook{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplateWebhook) ValidateCreate(ctx context.Context, objRaw runtime.Object) (admission.Warnings, error) {
	obj, ok := objRaw.(*CloudStackClusterTemplate)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackClusterTemplate but got a %T", objRaw))
	}
	allErrs := validateCloudStackClusterSpec(obj.Spec.Template.Spec)

	return nil, webhookutil.AggregateObjErrors(obj.GroupVersionKind().GroupKind(), obj.Name, allErrs)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplateWebhook) ValidateUpdate(ctx context.Context, oldRaw runtime.Object, newRaw runtime.Object) (admission.Warnings, error) {
	var allErrs field.ErrorList
	newObj, ok := newRaw.(*CloudStackClusterTemplate)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackClusterTemplate but got a %T", newRaw))
	}
	oldObj, ok := oldRaw.(*CloudStackClusterTemplate)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackClusterTemplate but got a %T", oldRaw))
	}

	if !reflect.DeepEqual(newObj.Spec.Template.Spec, oldObj.Spec.Template.Spec) {
		allErrs = append(allErrs, field.Invalid(field.NewPath("spec", "template", "spec"), newObj,
			cloudStackClusterTemplateImmutableMsg))
	}

	return nil, webhookutil.AggregateObjErrors(newObj.GroupVersionKind().GroupKind(), newObj.Name, allErrs)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterTemplateWebhook) ValidateDelete(ctx context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}
