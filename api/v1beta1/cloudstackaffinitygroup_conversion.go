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
	machineryconversion "k8s.io/apimachinery/pkg/conversion"
	utilconversion "sigs.k8s.io/cluster-api/util/conversion"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

func (r *CloudStackAffinityGroup) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta3.CloudStackAffinityGroup)
	if err := Convert_v1beta1_CloudStackAffinityGroup_To_v1beta3_CloudStackAffinityGroup(r, dst, nil); err != nil {
		return err
	}

	// Manually restore data
	restored := &v1beta3.CloudStackAffinityGroup{}
	if ok, err := utilconversion.UnmarshalData(r, restored); err != nil || !ok {
		return err
	}
	if restored.Spec.FailureDomainName != "" {
		dst.Spec.FailureDomainName = restored.Spec.FailureDomainName
	}

	return nil
}

func (r *CloudStackAffinityGroup) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta3.CloudStackAffinityGroup)
	if err := Convert_v1beta3_CloudStackAffinityGroup_To_v1beta1_CloudStackAffinityGroup(src, r, nil); err != nil {
		return err
	}

	// Preserve Hub data on down-conversion.
	return utilconversion.MarshalData(src, r)
}

func Convert_v1beta3_CloudStackAffinityGroupSpec_To_v1beta1_CloudStackAffinityGroupSpec(in *v1beta3.CloudStackAffinityGroupSpec, out *CloudStackAffinityGroupSpec, s machineryconversion.Scope) error {
	return autoConvert_v1beta3_CloudStackAffinityGroupSpec_To_v1beta1_CloudStackAffinityGroupSpec(in, out, s)
}
