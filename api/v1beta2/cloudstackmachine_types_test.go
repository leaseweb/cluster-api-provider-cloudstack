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

package v1beta2_test

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"k8s.io/utils/ptr"

	infrav1beta2 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta2"
)

var _ = Describe("CloudStackMachineConfig_CompressUserdata", func() {
	for _, tc := range []struct {
		Name    string
		Machine infrav1beta2.CloudStackMachine
		Expect  bool
	}{
		{
			Name: "is true when uncompressed user data is nil",
			Machine: infrav1beta2.CloudStackMachine{
				Spec: infrav1beta2.CloudStackMachineSpec{
					UncompressedUserData: nil,
				},
			},
			Expect: true,
		},
		{
			Name: "is false when uncompressed user data is true",
			Machine: infrav1beta2.CloudStackMachine{
				Spec: infrav1beta2.CloudStackMachineSpec{
					UncompressedUserData: ptr.To(true),
				},
			},
			Expect: false,
		},
		{
			Name: "Is false when uncompressed user data is false",
			Machine: infrav1beta2.CloudStackMachine{
				Spec: infrav1beta2.CloudStackMachineSpec{
					UncompressedUserData: ptr.To(false),
				},
			},
			Expect: true,
		},
	} {
		tcc := tc
		It(tcc.Name, func() {
			result := tcc.Machine.CompressUserdata()
			Expect(result).To(Equal(tcc.Expect))
		})
	}
})
