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

package cloud_test

import (
	"encoding/base64"

	"github.com/apache/cloudstack-go/v2/cloudstack"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/pkg/errors"
	"k8s.io/utils/ptr"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
	dummies "sigs.k8s.io/cluster-api-provider-cloudstack/test/dummies/v1beta3"
)

var _ = Describe("Instance", func() {
	const (
		unknownErrorMessage = "unknown err"
		offeringFakeID      = "123"
		templateFakeID      = "456"
		executableFilter    = "executable"
		diskOfferingFakeID  = "789"

		offeringName = "offering"
		templateName = "template"
	)

	notFoundError := errors.New("no match found")
	unknownError := errors.New(unknownErrorMessage)

	var (
		mockCtrl      *gomock.Controller
		mockClient    *cloudstack.CloudStackClient
		mockFactory   cloud.Factory
		configuration *cloudstack.MockConfigurationServiceIface
		vms           *cloudstack.MockVirtualMachineServiceIface
		sos           *cloudstack.MockServiceOfferingServiceIface
		dos           *cloudstack.MockDiskOfferingServiceIface
		ts            *cloudstack.MockTemplateServiceIface
		vs            *cloudstack.MockVolumeServiceIface
		client        cloud.Client
	)

	BeforeEach(func() {
		mockCtrl = gomock.NewController(GinkgoT())
		mockClient = cloudstack.NewMockClient(mockCtrl)
		mockFactory = cloud.NewFactory()
		configuration = mockClient.Configuration.(*cloudstack.MockConfigurationServiceIface)
		vms = mockClient.VirtualMachine.(*cloudstack.MockVirtualMachineServiceIface)
		sos = mockClient.ServiceOffering.(*cloudstack.MockServiceOfferingServiceIface)
		dos = mockClient.DiskOffering.(*cloudstack.MockDiskOfferingServiceIface)
		ts = mockClient.Template.(*cloudstack.MockTemplateServiceIface)
		vs = mockClient.Volume.(*cloudstack.MockVolumeServiceIface)
		client = mockFactory.NewClientFromCSAPIClient(mockClient, nil)

		dummies.SetDummyVars("default")
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("when fetching a VM instance", func() {
		It("Handles an unknown error when fetching by ID", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, unknownError)
			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).To(MatchError(unknownErrorMessage))
		})

		It("Handles finding more than one VM instance by ID", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, 2, nil)
			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).
				Should(MatchError("found more than one VM Instance with ID " + *dummies.CSMachine1.Spec.InstanceID))
		})

		It("sets dummies.CSMachine1 spec and status values when VM instance found by ID", func() {
			vmsResp := &cloudstack.VirtualMachinesMetric{Id: *dummies.CSMachine1.Spec.InstanceID}
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(vmsResp, 1, nil)
			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).Should(Succeed())
			Ω(dummies.CSMachine1.Spec.ProviderID).Should(Equal(ptr.To("cloudstack:///" + vmsResp.Id)))
			Ω(dummies.CSMachine1.Spec.InstanceID).Should(Equal(ptr.To(vmsResp.Id)))
		})

		It("handles an unknown error when fetching by name", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, notFoundError)
			vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, -1, unknownError)

			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).Should(MatchError(unknownErrorMessage))
		})

		It("handles finding more than one VM instance by Name", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, notFoundError)
			vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, 2, nil)

			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).Should(
				MatchError("found more than one VM Instance with name " + dummies.CSMachine1.Name))
		})

		It("sets dummies.CSMachine1 spec and status values when VM instance found by Name", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, notFoundError)
			vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{Id: *dummies.CSMachine1.Spec.InstanceID}, -1, nil)

			Ω(client.ResolveVMInstanceDetails(dummies.CSMachine1)).Should(Succeed())
			Ω(dummies.CSMachine1.Spec.ProviderID).Should(Equal(
				ptr.To("cloudstack:///" + *dummies.CSMachine1.Spec.InstanceID)))
			Ω(dummies.CSMachine1.Spec.InstanceID).Should(Equal(ptr.To(*dummies.CSMachine1.Spec.InstanceID)))
		})
	})

	Context("when creating a VM instance", func() {
		vmMetricResp := &cloudstack.VirtualMachinesMetric{}

		expectVMNotFound := func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, notFoundError)
			vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, -1, notFoundError)
		}

		It("doesn't re-create if one already exists.", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(vmMetricResp, -1, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				Should(Succeed())
		})

		It("returns unknown error while fetching VM instance", func() {
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, unknownError)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				Should(MatchError(unknownErrorMessage))
		})

		It("returns errors occurring while fetching service offering information", func() {
			expectVMNotFound()
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{}, -1, unknownError)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors if more than one service offering found", func() {
			expectVMNotFound()
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{
				Id:   dummies.CSMachine1.Spec.Offering.ID,
				Name: dummies.CSMachine1.Spec.Offering.Name,
			}, 2, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors while fetching template", func() {
			expectVMNotFound()

			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:   dummies.CSMachine1.Spec.Offering.ID,
					Name: dummies.CSMachine1.Spec.Offering.Name,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
				Return("", -1, unknownError)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors when more than one template found", func() {
			expectVMNotFound()

			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:   dummies.CSMachine1.Spec.Offering.ID,
					Name: dummies.CSMachine1.Spec.Offering.Name,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).Return("", 2, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors when more than one diskoffering found", func() {
			expectVMNotFound()

			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:   dummies.CSMachine1.Spec.Offering.ID,
					Name: dummies.CSMachine1.Spec.Offering.Name,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).Return(dummies.CSMachine1.Spec.Template.ID, 1, nil)
			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 2, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors when fetching diskoffering", func() {
			expectVMNotFound()
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:   dummies.CSMachine1.Spec.Offering.ID,
					Name: dummies.CSMachine1.Spec.Offering.Name,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).Return(dummies.CSMachine1.Spec.Template.ID, 1, nil)
			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
			dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, unknownError)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors when disk size not zero for non-customized disk offering", func() {
			expectVMNotFound()
			dummies.CSMachine1.Spec.DiskOffering.CustomSize = 1
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:   dummies.CSMachine1.Spec.Offering.ID,
					Name: dummies.CSMachine1.Spec.Offering.Name,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).Return(dummies.CSMachine1.Spec.Template.ID, 1, nil)
			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
			dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		It("returns errors when disk size zero for customized disk offering", func() {
			expectVMNotFound()
			dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:        dummies.CSMachine1.Spec.Offering.ID,
					Name:      dummies.CSMachine1.Spec.Offering.Name,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).Return(dummies.CSMachine1.Spec.Template.ID, 1, nil)
			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
			dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: true}, 1, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				ShouldNot(Succeed())
		})

		Context("when account & domains have limits", func() {
			It("returns errors when there are not enough available CPU in account", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "1",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("CPU available .* in account can't fulfil the requirement:.*")))
			})

			It("returns errors when there are not enough available CPU in domain", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "1",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("CPU available .* in domain can't fulfil the requirement:.*")))
			})

			It("returns errors when there are not enough available CPU in project", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "1",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("CPU available .* in project can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in account", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "512",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "1",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("memory available .* in account can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in domain", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "512",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "1",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("memory available .* in domain can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in project", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "20",
						MemoryAvailable: "512",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp("memory available .* in project can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available VM limit in account", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "20",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "0",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "20",
						MemoryAvailable: "512",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError("VM limit in account has reached its maximum value"))
			})

			It("returns errors when there is not enough available VM limit in domain", func() {
				expectVMNotFound()
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        dummies.CSMachine1.Spec.Offering.ID,
						Name:      dummies.CSMachine1.Spec.Offering.Name,
						Cpunumber: 2,
						Memory:    1024,
					}, 1, nil)
				user := &cloud.User{
					Account: cloud.Account{
						Domain: cloud.Domain{
							CPUAvailable:    "20",
							MemoryAvailable: "2048",
							VMAvailable:     "0",
						},
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "10",
					},
					Project: cloud.Project{
						ID:              "123",
						CPUAvailable:    "20",
						MemoryAvailable: "512",
						VMAvailable:     "20",
					},
				}
				c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
				Ω(c.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError("VM limit in domain has reached its maximum value"))
			})
		})

		It("returns errors when there is not enough available VM limit in project", func() {
			expectVMNotFound()
			dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:        dummies.CSMachine1.Spec.Offering.ID,
					Name:      dummies.CSMachine1.Spec.Offering.Name,
					Cpunumber: 2,
					Memory:    1024,
				}, 1, nil)
			user := &cloud.User{
				Account: cloud.Account{
					Domain: cloud.Domain{
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "10",
					},
					CPUAvailable:    "20",
					MemoryAvailable: "2048",
					VMAvailable:     "10",
				},
				Project: cloud.Project{
					ID:              "123",
					CPUAvailable:    "20",
					MemoryAvailable: "2048",
					VMAvailable:     "0",
				},
			}
			c := mockFactory.NewClientFromCSAPIClient(mockClient, user)
			Ω(c.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				Should(MatchError("VM Limit in project has reached it's maximum value"))
		})

		It("handles deployment errors", func() {
			expectVMNotFound()
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Name:      dummies.CSMachine1.Spec.Offering.Name,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
				Return(templateFakeID, 1, nil)
			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
				Return(diskOfferingFakeID, 1, nil)
			dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
				Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)
			vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
				Return(&cloudstack.DeployVirtualMachineParams{})
			vms.EXPECT().DeployVirtualMachine(gomock.Any()).Return(nil, unknownError)
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{}, nil)
			Ω(client.GetOrCreateVMInstance(
				dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
				Should(MatchError(unknownErrorMessage))
		})

		Context("when using UUIDs and/or names to locate service offerings and templates", func() {
			BeforeEach(func() {
				gomock.InOrder(
					vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
						Return(nil, -1, notFoundError),
					vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
						Return(&cloudstack.VirtualMachinesMetric{}, 1, nil))

				vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, -1, notFoundError)
			})

			ActionAndAssert := func() {
				vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
					Return(&cloudstack.DeployVirtualMachineParams{})

				deploymentResp := &cloudstack.DeployVirtualMachineResponse{Id: *dummies.CSMachine1.Spec.InstanceID}

				expectUserData := "my special userdata"

				vms.EXPECT().DeployVirtualMachine(gomock.Any()).Do(
					func(p interface{}) {
						params := p.(*cloudstack.DeployVirtualMachineParams)
						displayName, _ := params.GetDisplayname()
						Ω(displayName).Should(Equal(dummies.CAPIMachine.Name))

						b64UserData, _ := params.GetUserdata()

						userData, err := base64.StdEncoding.DecodeString(b64UserData)
						Ω(err).ToNot(HaveOccurred())

						decompressedUserData, err := decompress(userData)
						Ω(err).ToNot(HaveOccurred())

						Ω(string(decompressedUserData)).To(Equal(expectUserData))
					}).Return(deploymentResp, nil)

				Ω(client.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, expectUserData)).
					Should(Succeed())
			}

			It("works with service offering name and template name", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.ID = ""
				dummies.CSMachine1.Spec.Template.ID = ""
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName

				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)
				ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil)

				ActionAndAssert()
			})

			It("works with service offering name and template name without disk offering", func() {
				dummies.CSMachine1.Spec.Offering.ID = ""
				dummies.CSMachine1.Spec.Template.ID = ""
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName
				dummies.CSMachine1.Spec.DiskOffering = &infrav1.CloudStackResourceDiskOffering{}

				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
				ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil)

				ActionAndAssert()
			})

			It("works with service offering ID and template name", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = ""
				dummies.CSMachine1.Spec.Offering.Name = ""
				dummies.CSMachine1.Spec.Template.Name = templateName

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
				ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)

				ActionAndAssert()
			})

			It("works with service offering name and template ID", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.ID = ""
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = ""

				sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)

				ts.EXPECT().GetTemplateByID(dummies.CSMachine1.Spec.Template.ID, executableFilter, gomock.Any()).Return(&cloudstack.Template{Name: ""}, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)

				ActionAndAssert()
			})

			It("works with service offering ID and template ID", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Offering.Name = ""
				dummies.CSMachine1.Spec.Template.Name = ""

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        offeringFakeID,
						Cpunumber: 1,
						Memory:    1024,
					}, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).
					Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)
				ts.EXPECT().GetTemplateByID(dummies.CSMachine1.Spec.Template.ID, executableFilter, gomock.Any()).
					Return(&cloudstack.Template{Name: templateName}, 1, nil)

				ActionAndAssert()
			})

			It("works with Id and name both provided", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).Return(&cloudstack.ServiceOffering{
					Id:        dummies.CSMachine1.Spec.Offering.ID,
					Name:      dummies.CSMachine1.Spec.Offering.Name,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
				ts.EXPECT().GetTemplateByID(dummies.CSMachine1.Spec.Template.ID, executableFilter, gomock.Any()).Return(&cloudstack.Template{Name: templateName}, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID, 1, nil)
				dos.EXPECT().GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)

				ActionAndAssert()
			})
		})

		Context("when using both UUIDs and names to locate service offerings and templates", func() {
			BeforeEach(func() {
				vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
					Return(nil, -1, notFoundError)
				vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, -1, notFoundError)
			})

			It("works with Id and name both provided, offering name mismatch", func() {
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).Return(&cloudstack.ServiceOffering{Name: "offering-not-match"}, 1, nil)
				requiredRegexp := "offering name %s does not match name %s returned using UUID %s"
				Ω(client.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp(requiredRegexp, dummies.CSMachine1.Spec.Offering.Name, "offering-not-match", offeringFakeID)))
			})

			It("works with Id and name both provided, template name mismatch", func() {
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).Return(&cloudstack.ServiceOffering{Name: offeringName}, 1, nil)
				ts.EXPECT().GetTemplateByID(dummies.CSMachine1.Spec.Template.ID, executableFilter, gomock.Any()).Return(&cloudstack.Template{Name: "template-not-match"}, 1, nil)
				requiredRegexp := "template name %s does not match name %s returned using UUID %s"
				Ω(client.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp(requiredRegexp, dummies.CSMachine1.Spec.Template.Name, "template-not-match", templateFakeID)))
			})

			It("works with Id and name both provided, disk offering id/name mismatch", func() {
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName
				dummies.CSMachine1.Spec.Template.Name = templateName
				dummies.CSMachine1.Spec.DiskOffering.Name = "diskoffering"

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).Return(&cloudstack.ServiceOffering{Name: offeringName}, 1, nil)
				ts.EXPECT().GetTemplateByID(dummies.CSMachine1.Spec.Template.ID, executableFilter, gomock.Any()).Return(&cloudstack.Template{Name: templateName}, 1, nil)
				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).Return(diskOfferingFakeID+"-not-match", 1, nil)
				requiredRegexp := "diskOffering ID %s does not match ID %s returned using name %s"
				Ω(client.GetOrCreateVMInstance(
					dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")).
					Should(MatchError(MatchRegexp(requiredRegexp, dummies.CSMachine1.Spec.DiskOffering.ID, diskOfferingFakeID+"-not-match", dummies.CSMachine1.Spec.DiskOffering.Name)))
			})
		})

		It("doesn't compress user data", func() {
			dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
			dummies.CSMachine1.Spec.Offering.ID = ""
			dummies.CSMachine1.Spec.Template.ID = ""
			dummies.CSMachine1.Spec.Offering.Name = offeringName
			dummies.CSMachine1.Spec.Template.Name = templateName
			dummies.CSMachine1.Spec.UncompressedUserData = ptr.To(true)

			vms.EXPECT().
				GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(nil, -1, notFoundError)
			vms.EXPECT().
				GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{}, 1, nil)
			vms.EXPECT().
				GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).
				Return(nil, -1, notFoundError)

			sos.EXPECT().
				GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)
			dos.EXPECT().
				GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
				Return(diskOfferingFakeID, 1, nil)
			dos.EXPECT().
				GetDiskOfferingByID(dummies.CSMachine1.Spec.DiskOffering.ID, gomock.Any()).
				Return(&cloudstack.DiskOffering{Iscustomized: false}, 1, nil)
			ts.EXPECT().
				GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any()).
				Return(templateFakeID, 1, nil)
			vms.EXPECT().
				NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
				Return(&cloudstack.DeployVirtualMachineParams{})

			deploymentResp := &cloudstack.DeployVirtualMachineResponse{
				Id: *dummies.CSMachine1.Spec.InstanceID,
			}

			expectUserData := "my special userdata"

			vms.EXPECT().DeployVirtualMachine(gomock.Any()).Do(
				func(p interface{}) {
					params := p.(*cloudstack.DeployVirtualMachineParams)
					displayName, _ := params.GetDisplayname()
					Ω(displayName).Should(Equal(dummies.CAPIMachine.Name))

					// Ensure the user data is only base64 encoded.
					b64UserData, _ := params.GetUserdata()
					userData, err := base64.StdEncoding.DecodeString(b64UserData)
					Ω(err).ToNot(HaveOccurred())
					Ω(string(userData)).To(Equal(expectUserData))
				}).Return(deploymentResp, nil)

			err := client.GetOrCreateVMInstance(
				dummies.CSMachine1,
				dummies.CAPIMachine,
				dummies.CSFailureDomain1,
				dummies.CSAffinityGroup,
				expectUserData,
			)
			Ω(err).Should(Succeed())
		})
	})

	Context("when destroying a VM instance", func() {
		listCapabilitiesParams := &cloudstack.ListCapabilitiesParams{}
		expungeDestroyParams := &cloudstack.DestroyVirtualMachineParams{}
		expungeDestroyParams.SetExpunge(true)
		listCapabilitiesResponse := &cloudstack.ListCapabilitiesResponse{
			Capabilities: &cloudstack.Capability{Allowuserexpungerecovervm: true},
		}
		listVolumesParams := &cloudstack.ListVolumesParams{}
		listVolumesResponse := &cloudstack.ListVolumesResponse{
			Volumes: []*cloudstack.Volume{
				{
					Id: "123",
				},
				{
					Id: "456",
				},
			},
		}

		BeforeEach(func() {
			configuration.EXPECT().NewListCapabilitiesParams().Return(listCapabilitiesParams)
			configuration.EXPECT().ListCapabilities(listCapabilitiesParams).Return(listCapabilitiesResponse, nil)
		})

		It("calls destroy and finds VM doesn't exist, then returns nil", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, errors.New("unable to find uuid for id"))
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).
				Should(Succeed())
		})

		It("calls destroy and returns unexpected error", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, errors.New("new error"))
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).Should(MatchError("new error"))
		})

		It("calls destroy without error but cannot resolve VM after", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, nil)
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).Return(nil, -1, notFoundError)
			vms.EXPECT().GetVirtualMachinesMetricByName(dummies.CSMachine1.Name, gomock.Any()).Return(nil, -1, notFoundError)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).
				Should(Succeed())
		})

		It("calls destroy without error and identifies it as expunging", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, nil)
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{
					State: "Expunging",
				}, 1, nil)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).
				Should(Succeed())
		})

		It("calls destroy without error and identifies it as expunged", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, nil)
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{
					State: "Expunged",
				}, 1, nil)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).
				Should(Succeed())
		})

		It("calls destroy without error and identifies it as stopping", func() {
			listVolumesParams.SetVirtualmachineid(*dummies.CSMachine1.Spec.InstanceID)
			listVolumesParams.SetType("DATADISK")
			listVolumesParams.SetKeyword("DATA-")
			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(expungeDestroyParams).Return(nil, nil)
			vs.EXPECT().NewListVolumesParams().Return(listVolumesParams)
			vs.EXPECT().ListVolumes(listVolumesParams).Return(listVolumesResponse, nil)
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{
					State: "Stopping",
				}, 1, nil)
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).Should(MatchError("VM deletion in progress"))
		})

		It("calls destroy without error on a VM without additional diskoffering and does not search for data disks", func() {
			dummies.CSMachine1.Spec.DiskOffering = nil
			expungeDestroyParams := &cloudstack.DestroyVirtualMachineParams{}
			expungeDestroyParams.SetExpunge(true)

			vms.EXPECT().NewDestroyVirtualMachineParams(*dummies.CSMachine1.Spec.InstanceID).
				Return(expungeDestroyParams)
			vms.EXPECT().DestroyVirtualMachine(gomock.Any()).DoAndReturn(
				func(p *cloudstack.DestroyVirtualMachineParams) (*cloudstack.DestroyVirtualMachineResponse, error) {
					value, ok := p.GetExpunge()
					Ω(value).To(BeTrue())
					Ω(ok).To(BeTrue())

					ids, ok := p.GetVolumeids()
					Ω(ids).To(BeEmpty())
					Ω(ok).To(BeFalse())

					return &cloudstack.DestroyVirtualMachineResponse{}, nil
				})
			vs.EXPECT().NewListVolumesParams().Times(0)
			vs.EXPECT().ListVolumes(gomock.Any()).Times(0)
			vms.EXPECT().GetVirtualMachinesMetricByID(*dummies.CSMachine1.Spec.InstanceID, gomock.Any()).
				Return(&cloudstack.VirtualMachinesMetric{
					State: "Expunged",
				}, 1, nil)
			Ω(dummies.CSMachine1.Spec.DiskOffering).Should(BeNil())
			Ω(client.DestroyVMInstance(dummies.CSMachine1)).
				Should(Succeed())
		})
	})
})
