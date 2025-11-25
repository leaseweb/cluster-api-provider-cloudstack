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
	unsafe "unsafe"

	machineryconversion "k8s.io/apimachinery/pkg/conversion"
	"k8s.io/utils/ptr"
	clusterv1beta1 "sigs.k8s.io/cluster-api/api/core/v1beta1"
	"sigs.k8s.io/controller-runtime/pkg/conversion"

	"sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

func (r *CloudStackCluster) ConvertTo(dstRaw conversion.Hub) error {
	dst := dstRaw.(*v1beta3.CloudStackCluster)

	return Convert_v1beta2_CloudStackCluster_To_v1beta3_CloudStackCluster(r, dst, nil)
}

func (r *CloudStackCluster) ConvertFrom(srcRaw conversion.Hub) error {
	src := srcRaw.(*v1beta3.CloudStackCluster)

	return Convert_v1beta3_CloudStackCluster_To_v1beta2_CloudStackCluster(src, r, nil)
}

func Convert_v1beta3_CloudStackClusterSpec_To_v1beta2_CloudStackClusterSpec(in *v1beta3.CloudStackClusterSpec, out *CloudStackClusterSpec, s machineryconversion.Scope) error {
	err := autoConvert_v1beta3_CloudStackClusterSpec_To_v1beta2_CloudStackClusterSpec(in, out, s)
	if err != nil {
		return err
	}

	return nil
}

func Convert_v1beta2_CloudStackClusterSpec_To_v1beta3_CloudStackClusterSpec(in *CloudStackClusterSpec, out *v1beta3.CloudStackClusterSpec, s machineryconversion.Scope) error {
	err := autoConvert_v1beta2_CloudStackClusterSpec_To_v1beta3_CloudStackClusterSpec(in, out, s)
	if err != nil {
		return err
	}

	out.APIServerLoadBalancer = &v1beta3.APIServerLoadBalancer{
		Enabled: ptr.To(true),
	}

	return nil
}

func Convert_v1beta3_CloudStackClusterStatus_To_v1beta2_CloudStackClusterStatus(in *v1beta3.CloudStackClusterStatus, out *CloudStackClusterStatus, _ machineryconversion.Scope) error {
	out.FailureDomains = *(*clusterv1beta1.FailureDomains)(unsafe.Pointer(&in.FailureDomains))
	out.Ready = in.Ready

	return nil
}
