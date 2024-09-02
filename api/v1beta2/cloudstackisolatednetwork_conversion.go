/*
Copyright 2023 The Kubernetes Authors.

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

package v1beta2

import (
	machineryconversion "k8s.io/apimachinery/pkg/conversion"
	"sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/controller-runtime/pkg/conversion"
)

func (src *CloudStackIsolatedNetwork) ConvertTo(dstRaw conversion.Hub) error { // nolint
	dst := dstRaw.(*v1beta3.CloudStackIsolatedNetwork)
	return Convert_v1beta2_CloudStackIsolatedNetwork_To_v1beta3_CloudStackIsolatedNetwork(src, dst, nil)
}

func (dst *CloudStackIsolatedNetwork) ConvertFrom(srcRaw conversion.Hub) error { // nolint
	src := srcRaw.(*v1beta3.CloudStackIsolatedNetwork)
	return Convert_v1beta3_CloudStackIsolatedNetwork_To_v1beta2_CloudStackIsolatedNetwork(src, dst, nil)
}

func Convert_v1beta2_CloudStackIsolatedNetworkStatus_To_v1beta3_CloudStackIsolatedNetworkStatus(in *CloudStackIsolatedNetworkStatus, out *v1beta3.CloudStackIsolatedNetworkStatus, s machineryconversion.Scope) error {
	out.PublicIPID = in.PublicIPID
	out.LBRuleID = in.LBRuleID
	out.LoadBalancerRuleIDs = []string{in.LBRuleID}
	out.Ready = in.Ready
	return nil
}

func Convert_v1beta3_CloudStackIsolatedNetworkStatus_To_v1beta2_CloudStackIsolatedNetworkStatus(in *v1beta3.CloudStackIsolatedNetworkStatus, out *CloudStackIsolatedNetworkStatus, s machineryconversion.Scope) error {
	out.PublicIPID = in.PublicIPID
	out.LBRuleID = in.LBRuleID
	out.Ready = in.Ready
	return nil
}
