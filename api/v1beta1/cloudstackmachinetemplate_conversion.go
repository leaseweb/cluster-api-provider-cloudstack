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

package v1beta1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	machineryconversion "k8s.io/apimachinery/pkg/conversion"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

func (r *CloudStackMachineTemplate) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta3.CloudStackMachineTemplate)
	if err := Convert_v1beta1_CloudStackMachineTemplate_To_v1beta3_CloudStackMachineTemplate(r, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &v1beta3.CloudStackMachineTemplate{}
	if ok, err := utilconversion.UnmarshalData(r, restored); err != nil || !ok {
		return err
	}
	if restored.Spec.Template.Spec.FailureDomainName != "" {
		dst.Spec.Template.Spec.FailureDomainName = restored.Spec.Template.Spec.FailureDomainName
	}
	if restored.Spec.Template.Spec.UncompressedUserData != nil {
		dst.Spec.Template.Spec.UncompressedUserData = restored.Spec.Template.Spec.UncompressedUserData
	}

	dst.Spec.Template.ObjectMeta = restored.Spec.Template.ObjectMeta

	return nil
}

func (r *CloudStackMachineTemplate) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta3.CloudStackMachineTemplate)
	if err := Convert_v1beta3_CloudStackMachineTemplate_To_v1beta1_CloudStackMachineTemplate(src, r, nil); err != nil {
		return err
	}

	// Preserve Hub data on down-conversion.
	return utilconversion.MarshalData(src, r)
}

func Convert_v1beta1_CloudStackMachineTemplateSpec_To_v1beta3_CloudStackMachineTemplateSpec(in *CloudStackMachineTemplateSpec, out *v1beta3.CloudStackMachineTemplateSpec, s machineryconversion.Scope) error {
	return Convert_v1beta1_CloudStackMachineTemplateResource_To_v1beta3_CloudStackMachineTemplateResource(&in.Spec, &out.Template, s)
}

func Convert_v1beta3_CloudStackMachineTemplateSpec_To_v1beta1_CloudStackMachineTemplateSpec(in *v1beta3.CloudStackMachineTemplateSpec, out *CloudStackMachineTemplateSpec, s machineryconversion.Scope) error {
	return Convert_v1beta3_CloudStackMachineTemplateResource_To_v1beta1_CloudStackMachineTemplateResource(&in.Template, &out.Spec, s)
}

func Convert_v1beta1_ObjectMeta_To_v1_ObjectMeta(in *clusterv1beta1.ObjectMeta, out *metav1.ObjectMeta, _ machineryconversion.Scope) error {
	if in.Annotations != nil {
		out.Annotations = in.Annotations
	}
	if in.Labels != nil {
		out.Labels = in.Labels
	}

	return nil
}

func Convert_v1_ObjectMeta_To_v1beta1_ObjectMeta(in *metav1.ObjectMeta, out *clusterv1beta1.ObjectMeta, _ machineryconversion.Scope) error {
	if in.Annotations != nil {
		out.Annotations = in.Annotations
	}
	if in.Labels != nil {
		out.Labels = in.Labels
	}

	return nil
}
