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
		configuration = mockClient.Configuration.(*cloudstack.MockConfigurationServiceIface)
		vms = mockClient.VirtualMachine.(*cloudstack.MockVirtualMachineServiceIface)
		sos = mockClient.ServiceOffering.(*cloudstack.MockServiceOfferingServiceIface)
		dos = mockClient.DiskOffering.(*cloudstack.MockDiskOfferingServiceIface)
		ts = mockClient.Template.(*cloudstack.MockTemplateServiceIface)
		vs = mockClient.Volume.(*cloudstack.MockVolumeServiceIface)
		client = cloud.NewClientFromCSAPIClient(mockClient, nil)

		dummies.SetDummyVars("default")
	})

	AfterEach(func() {
		mockCtrl.Finish()
	})

	Context("when fetching a VM instance", func() {
		It("returns error when instance ID is empty", func() {
			dummies.CSMachine1.Spec.InstanceID = ptr.To("")
			_, err := client.GetVMInstanceByID(*dummies.CSMachine1.Spec.InstanceID)
			Ω(err).Should(MatchError("instance ID is required"))
		})

		It("returns error when instance name is empty", func() {
			dummies.CSMachine1.Name = ""
			_, err := client.GetVMInstanceByName(dummies.CSMachine1.Name)
			Ω(err).Should(MatchError("instance name is required"))
		})

		It("Handles an unknown error when fetching by ID", func() {
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(nil, unknownError)
			_, err := client.GetVMInstanceByID(*dummies.CSMachine1.Spec.InstanceID)
			Ω(err).Should(MatchError(unknownErrorMessage))
		})

		It("Handles finding more than one VM instance by ID", func() {
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
				Count: 2,
				VirtualMachines: []*cloudstack.VirtualMachine{
					{Id: *dummies.CSMachine1.Spec.InstanceID},
					{Id: *dummies.CSMachine1.Spec.InstanceID},
				},
			}, nil)
			_, err := client.GetVMInstanceByID(*dummies.CSMachine1.Spec.InstanceID)
			Ω(err).Should(MatchError("found more than one VM Instance with ID " + *dummies.CSMachine1.Spec.InstanceID))
		})

		It("successfully gets VM instance by ID", func() {
			expectedVM := &cloudstack.VirtualMachine{Id: *dummies.CSMachine1.Spec.InstanceID}
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
				Count:           1,
				VirtualMachines: []*cloudstack.VirtualMachine{expectedVM},
			}, nil)
			vm, err := client.GetVMInstanceByID(*dummies.CSMachine1.Spec.InstanceID)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(vm).Should(Equal(expectedVM))
		})

		It("handles an unknown error when fetching by name", func() {
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(nil, unknownError)
			_, err := client.GetVMInstanceByName(dummies.CSMachine1.Name)
			Ω(err).Should(MatchError(unknownErrorMessage))
		})

		It("handles finding more than one VM instance by Name", func() {
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
				Count: 2,
				VirtualMachines: []*cloudstack.VirtualMachine{
					{Name: dummies.CSMachine1.Name},
					{Name: dummies.CSMachine1.Name},
				},
			}, nil)
			_, err := client.GetVMInstanceByName(dummies.CSMachine1.Name)
			Ω(err).Should(MatchError("found more than one VM Instance with name " + dummies.CSMachine1.Name))
		})

		It("successfully gets VM instance by name", func() {
			expectedVM := &cloudstack.VirtualMachine{Name: dummies.CSMachine1.Name}
			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
				Count:           1,
				VirtualMachines: []*cloudstack.VirtualMachine{expectedVM},
			}, nil)
			vm, err := client.GetVMInstanceByName(dummies.CSMachine1.Name)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(vm).Should(Equal(expectedVM))
		})
	})

	Context("when creating a VM instance", func() {
		It("returns errors occurring while fetching service offering information", func() {
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{}, -1, unknownError)
			_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
			Ω(err).ShouldNot(Succeed())
		})

		It("returns errors if more than one service offering found", func() {
			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).Return(&cloudstack.ServiceOffering{
				Id:   dummies.CSMachine1.Spec.Offering.ID,
				Name: dummies.CSMachine1.Spec.Offering.Name,
			}, 2, nil)
			_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
			Ω(err).ShouldNot(Succeed())
		})

		It("successfully creates a VM instance", func() {
			expectedVM := &cloudstack.VirtualMachine{
				Id: *dummies.CSMachine1.Spec.InstanceID,
				Nic: []cloudstack.Nic{
					{Ipaddress: "10.0.0.1", Isdefault: true},
				},
			}

			dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
				Return(diskOfferingFakeID, 1, nil)

			dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
				Return(&cloudstack.DiskOffering{
					Id:           diskOfferingFakeID,
					Iscustomized: false,
				}, 1, nil)

			sos.EXPECT().GetServiceOfferingByName(dummies.CSMachine1.Spec.Offering.Name, gomock.Any()).
				Return(&cloudstack.ServiceOffering{
					Id:        offeringFakeID,
					Name:      dummies.CSMachine1.Spec.Offering.Name,
					Cpunumber: 1,
					Memory:    1024,
				}, 1, nil)

			ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
				Return(templateFakeID, 1, nil)

			vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
				Return(&cloudstack.DeployVirtualMachineParams{})

			expectUserData := "my special userdata"
			vms.EXPECT().DeployVirtualMachine(gomock.Any()).Do(
				func(params *cloudstack.DeployVirtualMachineParams) {
					displayName, _ := params.GetDisplayname()
					Ω(displayName).Should(Equal(dummies.CAPIMachine.Name))

					b64UserData, _ := params.GetUserdata()

					userData, err := base64.StdEncoding.DecodeString(b64UserData)
					Ω(err).ToNot(HaveOccurred())

					decompressedUserData, err := decompress(userData)
					Ω(err).ToNot(HaveOccurred())

					Ω(string(decompressedUserData)).To(Equal(expectUserData))
				},
			).Return(&cloudstack.DeployVirtualMachineResponse{
				Id: *dummies.CSMachine1.Spec.InstanceID,
			}, nil)

			vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
			vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
				Count:           1,
				VirtualMachines: []*cloudstack.VirtualMachine{expectedVM},
			}, nil)

			vm, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, expectUserData)
			Ω(err).ShouldNot(HaveOccurred())
			Ω(vm).Should(Equal(expectedVM))
		})

		Context("when using UUIDs and/or names to locate service offerings", func() {
			It("works with service offering ID", func() {
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Offering.Name = ""

				sos.EXPECT().GetServiceOfferingByID(dummies.CSMachine1.Spec.Offering.ID, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        offeringFakeID,
						Name:      offeringName,
						Cpunumber: 1,
						Memory:    1024,
					}, 1, nil)

				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: false,
					}, 1, nil)

				ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil)

				vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
					Return(&cloudstack.DeployVirtualMachineParams{})

				vms.EXPECT().DeployVirtualMachine(gomock.Any()).Return(&cloudstack.DeployVirtualMachineResponse{
					Id: *dummies.CSMachine1.Spec.InstanceID,
				}, nil)

				vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
				vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
					Count: 1,
					VirtualMachines: []*cloudstack.VirtualMachine{{
						Id: *dummies.CSMachine1.Spec.InstanceID,
					}},
				}, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("works with service offering name", func() {
				dummies.CSMachine1.Spec.Offering.ID = ""
				dummies.CSMachine1.Spec.Offering.Name = offeringName

				sos.EXPECT().GetServiceOfferingByName(offeringName, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        offeringFakeID,
						Name:      offeringName,
						Cpunumber: 1,
						Memory:    1024,
					}, 1, nil)

				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: false,
					}, 1, nil)

				ts.EXPECT().GetTemplateID(dummies.CSMachine1.Spec.Template.Name, executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil)

				vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
					Return(&cloudstack.DeployVirtualMachineParams{})

				vms.EXPECT().DeployVirtualMachine(gomock.Any()).Return(&cloudstack.DeployVirtualMachineResponse{
					Id: *dummies.CSMachine1.Spec.InstanceID,
				}, nil)

				vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
				vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
					Count: 1,
					VirtualMachines: []*cloudstack.VirtualMachine{{
						Id: *dummies.CSMachine1.Spec.InstanceID,
					}},
				}, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns error when service offering name and ID mismatch", func() {
				dummies.CSMachine1.Spec.Offering.ID = offeringFakeID
				dummies.CSMachine1.Spec.Offering.Name = offeringName

				sos.EXPECT().GetServiceOfferingByID(offeringFakeID, gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:   offeringFakeID,
						Name: "different-name",
					}, 1, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(ContainSubstring("offering name")))
			})
		})

		Context("when using UUIDs and/or names to locate templates", func() {
			BeforeEach(func() {
				// Setup common service offering expectations
				sos.EXPECT().GetServiceOfferingByName(gomock.Any(), gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        offeringFakeID,
						Name:      offeringName,
						Cpunumber: 1,
						Memory:    1024,
					}, 1, nil).AnyTimes()
			})

			It("works with template ID", func() {
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Template.Name = ""

				ts.EXPECT().GetTemplateByID(templateFakeID, executableFilter, gomock.Any()).
					Return(&cloudstack.Template{
						Id:   templateFakeID,
						Name: templateName,
					}, 1, nil)

				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: false,
					}, 1, nil)

				vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
					Return(&cloudstack.DeployVirtualMachineParams{})

				vms.EXPECT().DeployVirtualMachine(gomock.Any()).Return(&cloudstack.DeployVirtualMachineResponse{
					Id: *dummies.CSMachine1.Spec.InstanceID,
				}, nil)

				vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
				vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
					Count: 1,
					VirtualMachines: []*cloudstack.VirtualMachine{{
						Id: *dummies.CSMachine1.Spec.InstanceID,
					}},
				}, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns error when template name and ID mismatch", func() {
				dummies.CSMachine1.Spec.Template.ID = templateFakeID
				dummies.CSMachine1.Spec.Template.Name = templateName

				ts.EXPECT().GetTemplateByID(templateFakeID, executableFilter, gomock.Any()).
					Return(&cloudstack.Template{
						Id:   templateFakeID,
						Name: "different-name",
					}, 1, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(ContainSubstring("template name")))
			})
		})

		Context("when using disk offerings", func() {
			BeforeEach(func() {
				// Setup common service offering and template expectations
				sos.EXPECT().GetServiceOfferingByName(gomock.Any(), gomock.Any()).
					Return(&cloudstack.ServiceOffering{
						Id:        offeringFakeID,
						Name:      offeringName,
						Cpunumber: 1,
						Memory:    1024,
					}, 1, nil).AnyTimes()

				ts.EXPECT().GetTemplateID(gomock.Any(), executableFilter, dummies.Zone1.ID, gomock.Any(), gomock.Any()).
					Return(templateFakeID, 1, nil).AnyTimes()
			})

			It("works with disk offering ID", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.DiskOffering.Name = ""

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: false,
					}, 1, nil)

				vms.EXPECT().NewDeployVirtualMachineParams(offeringFakeID, templateFakeID, dummies.Zone1.ID).
					Return(&cloudstack.DeployVirtualMachineParams{})

				vms.EXPECT().DeployVirtualMachine(gomock.Any()).Return(&cloudstack.DeployVirtualMachineResponse{
					Id: *dummies.CSMachine1.Spec.InstanceID,
				}, nil)

				vms.EXPECT().NewListVirtualMachinesParams().Return(&cloudstack.ListVirtualMachinesParams{})
				vms.EXPECT().ListVirtualMachines(gomock.Any()).Return(&cloudstack.ListVirtualMachinesResponse{
					Count: 1,
					VirtualMachines: []*cloudstack.VirtualMachine{{
						Id: *dummies.CSMachine1.Spec.InstanceID,
					}},
				}, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).ShouldNot(HaveOccurred())
			})

			It("returns error for customized disk offering without size", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 0

				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: true,
					}, 1, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(ContainSubstring("disk size can not be 0 GB")))
			})

			It("returns error for non-customized disk offering with size", func() {
				dummies.CSMachine1.Spec.DiskOffering.ID = diskOfferingFakeID
				dummies.CSMachine1.Spec.DiskOffering.CustomSize = 100

				dos.EXPECT().GetDiskOfferingID(dummies.CSMachine1.Spec.DiskOffering.Name, gomock.Any()).
					Return(diskOfferingFakeID, 1, nil)

				dos.EXPECT().GetDiskOfferingByID(diskOfferingFakeID, gomock.Any()).
					Return(&cloudstack.DiskOffering{
						Id:           diskOfferingFakeID,
						Iscustomized: false,
					}, 1, nil)

				_, err := client.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(ContainSubstring("disk size can not be specified")))
			})
		})

		Context("when account & domains have limits", func() {
			It("returns errors when there are not enough available CPU in account", func() {
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
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("CPU available .* in account can't fulfil the requirement:.*")))
			})

			It("returns errors when there are not enough available CPU in domain", func() {
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
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("CPU available .* in domain can't fulfil the requirement:.*")))
			})

			It("returns errors when there are not enough available CPU in project", func() {
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
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("CPU available .* in project can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in account", func() {
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
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("memory available .* in account can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in domain", func() {
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
						CPUAvailable:    "20",
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("memory available .* in domain can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available memory in project", func() {
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
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError(MatchRegexp("memory available .* in project can't fulfil the requirement:.*")))
			})

			It("returns errors when there is not enough available VM limit in account", func() {
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
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError("VM limit in account has reached its maximum value"))
			})

			It("returns errors when there is not enough available VM limit in domain", func() {
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
						MemoryAvailable: "2048",
						VMAvailable:     "20",
					},
				}
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError("VM limit in domain has reached its maximum value"))
			})

			It("returns errors when there is not enough available VM limit in project", func() {
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
				c := cloud.NewClientFromCSAPIClient(mockClient, user)
				_, err := c.CreateVMInstance(dummies.CSMachine1, dummies.CAPIMachine, dummies.CSFailureDomain1, dummies.CSAffinityGroup, "")
				Ω(err).Should(MatchError("VM Limit in project has reached it's maximum value"))
			})
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
