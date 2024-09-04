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

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

func (r *CloudStackIsolatedNetwork) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*infrav1.CloudStackIsolatedNetwork)
	if err := Convert_v1beta1_CloudStackIsolatedNetwork_To_v1beta3_CloudStackIsolatedNetwork(r, dst, nil); err != nil {
		return err
	}

	// Manually restore data.
	restored := &infrav1.CloudStackIsolatedNetwork{}
	if ok, err := utilconversion.UnmarshalData(r, restored); err != nil || !ok {
		return err
	}
	if restored.Spec.FailureDomainName != "" {
		dst.Spec.FailureDomainName = restored.Spec.FailureDomainName
	}

	return nil
}

func (r *CloudStackIsolatedNetwork) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*infrav1.CloudStackIsolatedNetwork)
	if err := Convert_v1beta3_CloudStackIsolatedNetwork_To_v1beta1_CloudStackIsolatedNetwork(src, r, nil); err != nil {
		return err
	}

	// Preserve Hub data on down-conversion.
	return utilconversion.MarshalData(src, r)
}

func Convert_v1beta3_CloudStackIsolatedNetworkSpec_To_v1beta1_CloudStackIsolatedNetworkSpec(in *infrav1.CloudStackIsolatedNetworkSpec, out *CloudStackIsolatedNetworkSpec, s machineryconversion.Scope) error {
	return autoConvert_v1beta3_CloudStackIsolatedNetworkSpec_To_v1beta1_CloudStackIsolatedNetworkSpec(in, out, s)
}

func Convert_v1beta1_CloudStackIsolatedNetworkStatus_To_v1beta3_CloudStackIsolatedNetworkStatus(in *CloudStackIsolatedNetworkStatus, out *infrav1.CloudStackIsolatedNetworkStatus, s machineryconversion.Scope) error {
	out.PublicIPID = in.PublicIPID
	out.LBRuleID = in.LBRuleID
	out.APIServerLoadBalancer = &infrav1.LoadBalancer{}
	out.LoadBalancerRuleIDs = []string{in.LBRuleID}
	out.Ready = in.Ready

	return nil
}

func Convert_v1beta3_CloudStackIsolatedNetworkStatus_To_v1beta1_CloudStackIsolatedNetworkStatus(in *infrav1.CloudStackIsolatedNetworkStatus, out *CloudStackIsolatedNetworkStatus, s machineryconversion.Scope) error {
	out.PublicIPID = in.PublicIPID
	out.LBRuleID = in.LBRuleID
	out.Ready = in.Ready

	return nil
}
