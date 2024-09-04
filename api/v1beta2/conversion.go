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

package v1beta2

import (
	machineryconversion "k8s.io/apimachinery/pkg/conversion"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

func Convert_v1beta3_Network_To_v1beta2_Network(in *infrav1.Network, out *Network, s machineryconversion.Scope) error {
	if err := autoConvert_v1beta3_Network_To_v1beta2_Network(in, out, s); err != nil {
		return err
	}

	return nil
}

func Convert_v1beta3_CloudStackIsolatedNetworkSpec_To_v1beta2_CloudStackIsolatedNetworkSpec(in *infrav1.CloudStackIsolatedNetworkSpec, out *CloudStackIsolatedNetworkSpec, s machineryconversion.Scope) error {
	if err := autoConvert_v1beta3_CloudStackIsolatedNetworkSpec_To_v1beta2_CloudStackIsolatedNetworkSpec(in, out, s); err != nil {
		return err
	}

	return nil
}
