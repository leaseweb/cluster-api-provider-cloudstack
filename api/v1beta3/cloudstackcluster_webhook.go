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
	"net"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation"
	"k8s.io/apimachinery/pkg/util/validation/field"
	"sigs.k8s.io/cluster-api/util/annotations"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/webhookutil"
)

type CloudStackClusterWebhook struct{}

func (r *CloudStackClusterWebhook) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(&CloudStackCluster{}).
		WithDefaulter(r).
		WithValidator(r).
		Complete()
}

// +kubebuilder:webhook:verbs=create;update,path=/validate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackcluster,mutating=false,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclusters,versions=v1beta3,name=validation.cloudstackcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1
// +kubebuilder:webhook:verbs=create;update,path=/mutate-infrastructure-cluster-x-k8s-io-v1beta3-cloudstackcluster,mutating=true,failurePolicy=fail,matchPolicy=Equivalent,groups=infrastructure.cluster.x-k8s.io,resources=cloudstackclusters,versions=v1beta3,name=default.cloudstackcluster.infrastructure.cluster.x-k8s.io,sideEffects=None,admissionReviewVersions=v1;v1beta1

var (
	_ webhook.CustomDefaulter = &CloudStackClusterWebhook{}
	_ webhook.CustomValidator = &CloudStackClusterWebhook{}
)

// Default implements webhook.Defaulter so a webhook will be registered for the type.
func (r *CloudStackClusterWebhook) Default(_ context.Context, objRaw runtime.Object) error {
	obj, ok := objRaw.(*CloudStackCluster)
	if !ok {
		return apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackCluster but got a %T", objRaw))
	}

	defaultCloudStackClusterSpec(&obj.Spec)

	return nil
}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterWebhook) ValidateCreate(_ context.Context, objRaw runtime.Object) (admission.Warnings, error) {
	obj, ok := objRaw.(*CloudStackCluster)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackCluster but got a %T", objRaw))
	}

	errorList := validateCloudStackClusterSpec(obj.Spec)

	return nil, webhookutil.AggregateObjErrors(obj.GroupVersionKind().GroupKind(), obj.Name, errorList)
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterWebhook) ValidateUpdate(_ context.Context, oldRaw runtime.Object, newRaw runtime.Object) (admission.Warnings, error) {
	oldObj, ok := oldRaw.(*CloudStackCluster)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackCluster but got a %T", oldRaw))
	}
	newObj, ok := newRaw.(*CloudStackCluster)
	if !ok {
		return nil, apierrors.NewBadRequest(fmt.Sprintf("expected a CloudStackCluster but got a %T", newRaw))
	}
	spec := newObj.Spec

	oldSpec := oldObj.Spec

	errorList := field.ErrorList(nil)

	if err := ValidateFailureDomainUpdates(oldSpec.FailureDomains, spec.FailureDomains); err != nil {
		errorList = append(errorList, err)
	}

	if oldSpec.ControlPlaneEndpoint.Host != "" { // Need to allow one time endpoint setting via CAPC cluster controller.
		errorList = webhookutil.EnsureEqualStrings(
			spec.ControlPlaneEndpoint.Host, oldSpec.ControlPlaneEndpoint.Host, "controlplaneendpoint.host", errorList)
		errorList = webhookutil.EnsureEqualStrings(
			string(spec.ControlPlaneEndpoint.Port), string(oldSpec.ControlPlaneEndpoint.Port),
			"controlplaneendpoint.port", errorList)
	}

	if annotations.IsExternallyManaged(oldObj) && !annotations.IsExternallyManaged(newObj) {
		errorList = append(errorList,
			field.Forbidden(field.NewPath("metadata", "annotations"),
				"removal of externally managed (managed-by) annotation is not allowed"),
		)
	}

	return nil, webhookutil.AggregateObjErrors(newObj.GroupVersionKind().GroupKind(), newObj.Name, errorList)
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type.
func (r *CloudStackClusterWebhook) ValidateDelete(_ context.Context, _ runtime.Object) (admission.Warnings, error) {
	return nil, nil
}

// ValidateFailureDomainUpdates verifies that at least one failure domain has not been deleted, and
// failure domains that are held over have not been modified.
func ValidateFailureDomainUpdates(oldFDs, newFDs []CloudStackFailureDomainSpec) *field.Error {
	newFDsByName := map[string]CloudStackFailureDomainSpec{}
	for _, newFD := range newFDs {
		newFDsByName[newFD.Name] = newFD
	}

	atLeastOneRemains := false
	for _, oldFD := range oldFDs {
		if newFD, present := newFDsByName[oldFD.Name]; present {
			atLeastOneRemains = true
			if !FailureDomainsEqual(newFD, oldFD) {
				return field.Forbidden(field.NewPath("spec", "FailureDomains"),
					"Cannot change FailureDomain "+oldFD.Name)
			}
		}
	}
	if !atLeastOneRemains {
		return field.Forbidden(field.NewPath("spec", "FailureDomains"), "At least one FailureDomain must be unchanged on update.")
	}

	return nil
}

// FailureDomainsEqual is a manual deep equal on failure domains.
func FailureDomainsEqual(fd1, fd2 CloudStackFailureDomainSpec) bool {
	return fd1.Name == fd2.Name &&
		fd1.ACSEndpoint == fd2.ACSEndpoint &&
		fd1.Account == fd2.Account &&
		fd1.Domain == fd2.Domain &&
		fd1.Zone.Name == fd2.Zone.Name &&
		fd1.Zone.ID == fd2.Zone.ID &&
		fd1.Zone.Network.Name == fd2.Zone.Network.Name &&
		fd1.Zone.Network.ID == fd2.Zone.Network.ID &&
		fd1.Zone.Network.Type == fd2.Zone.Network.Type &&
		fd1.Zone.Network.Domain == fd2.Zone.Network.Domain
}

// ValidateCIDR validates whether a CIDR matches the conventions expected by net.ParseCIDR.
func ValidateCIDR(cidr string) (*net.IPNet, error) {
	_, net, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}

	return net, nil
}

func defaultCloudStackClusterSpec(s *CloudStackClusterSpec) {
	if s.ControlPlaneEndpoint.Port == 0 {
		s.ControlPlaneEndpoint.Port = 6443
	}
}

func validateCloudStackClusterSpec(s CloudStackClusterSpec) field.ErrorList {
	var errorList field.ErrorList

	// Require FailureDomains and their respective sub-fields.
	if len(s.FailureDomains) == 0 {
		errorList = append(errorList, field.Required(field.NewPath("spec", "FailureDomains"), "FailureDomains"))
	} else {
		for _, fdSpec := range s.FailureDomains { // Require failureDomain names meet k8s qualified name spec.
			for _, errMsg := range validation.IsDNS1123Subdomain(fdSpec.Name) {
				errorList = append(errorList, field.Invalid(
					field.NewPath("spec", "failureDomains", "name"), fdSpec.Name, errMsg))
			}
			if fdSpec.Zone.Network.Name == "" && fdSpec.Zone.Network.ID == "" {
				errorList = append(errorList, field.Required(
					field.NewPath("spec", "failureDomains", "Zone", "Network"),
					"each Zone requires a Network specification"))
			}
			if fdSpec.ACSEndpoint.Name == "" || fdSpec.ACSEndpoint.Namespace == "" {
				errorList = append(errorList, field.Required(
					field.NewPath("spec", "failureDomains", "ACSEndpoint"),
					"Name and Namespace are required"))
			}
			if fdSpec.Zone.Network.CIDR != "" {
				if _, errMsg := ValidateCIDR(fdSpec.Zone.Network.CIDR); errMsg != nil {
					errorList = append(errorList, field.Invalid(
						field.NewPath("spec", "failureDomains", "Zone", "Network"), fdSpec.Zone.Network.CIDR, "must be valid CIDR: "+errMsg.Error()))
				}
			}
			if fdSpec.Zone.Network.Domain != "" {
				for _, errMsg := range validation.IsDNS1123Subdomain(fdSpec.Zone.Network.Domain) {
					errorList = append(errorList, field.Invalid(
						field.NewPath("spec", "failureDomains", "Zone", "Network"), fdSpec.Zone.Network.Domain, errMsg))
				}
			}
		}
	}

	return errorList
}
