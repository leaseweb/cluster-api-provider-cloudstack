/*
Copyright 2025 The Kubernetes Authors.

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

package controllers

import (
	"context"

	"github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	clusterv1 "sigs.k8s.io/cluster-api/api/core/v1beta2"
	"sigs.k8s.io/cluster-api/util"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
)

// GetOwnerClusterName returns the name of the owning Cluster by finding a clusterv1.Cluster in the ownership references.
func GetOwnerClusterName(obj metav1.ObjectMeta) (string, bool) {
	for _, ref := range obj.OwnerReferences {
		if ref.Kind != "Cluster" {
			continue
		}
		gv, err := schema.ParseGroupVersion(ref.APIVersion)
		if err != nil {
			return "", false
		}
		if gv.Group == clusterv1.GroupVersion.Group {
			return ref.Name, true
		}
	}
	return "", false
}

// fetchOwnerRef searches a list of OwnerReference objects for a given kind and returns it if found.
func fetchOwnerRef(refList []metav1.OwnerReference, kind string) *metav1.OwnerReference {
	for _, ref := range refList {
		if ref.Kind == kind {
			return &ref
		}
	}

	return nil
}

// GetManagementOwnerRef returns the owner reference pointing to the CAPI Machine's manager.
func GetManagementOwnerRef(capiMachine *clusterv1.Machine) *metav1.OwnerReference {
	if util.IsControlPlaneMachine(capiMachine) {
		return fetchOwnerRef(capiMachine.OwnerReferences, "KubeadmControlPlane")
	} else if ref := fetchOwnerRef(capiMachine.OwnerReferences, "EtcdadmCluster"); ref != nil {
		return ref
	}

	return fetchOwnerRef(capiMachine.OwnerReferences, "MachineSet")
}

// GetFailureDomainByName gets the CloudStack Failure Domain by name.
func GetFailureDomainByName(ctx context.Context, k8sClient client.Client, name, namespace, clusterName string) (*infrav1.CloudStackFailureDomain, error) {
	fd := &infrav1.CloudStackFailureDomain{}
	metaHashName := infrav1.FailureDomainHashedMetaName(name, clusterName)
	if err := k8sClient.Get(ctx, client.ObjectKey{Name: metaHashName, Namespace: namespace}, fd); err != nil {
		return nil, errors.Wrapf(err, "failed to get failure domain with name %s", name)
	}

	return fd, nil
}
