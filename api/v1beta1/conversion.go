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
	"context"
	"errors"

	corev1 "k8s.io/api/core/v1"
	machineryconversion "k8s.io/apimachinery/pkg/conversion"
	"sigs.k8s.io/controller-runtime/pkg/client"

	infrav1 "sigs.k8s.io/cluster-api-provider-cloudstack/api/v1beta3"
	"sigs.k8s.io/cluster-api-provider-cloudstack/pkg/cloud"
)

const DefaultEndpointCredential = "global"

func Convert_v1beta1_CloudStackCluster_To_v1beta3_CloudStackCluster(in *CloudStackCluster, out *infrav1.CloudStackCluster, _ machineryconversion.Scope) error {
	out.ObjectMeta = in.ObjectMeta
	failureDomains, err := GetFailureDomains(in)
	if err != nil {
		return err
	}
	out.Spec = infrav1.CloudStackClusterSpec{
		ControlPlaneEndpoint: in.Spec.ControlPlaneEndpoint,
		FailureDomains:       failureDomains,
	}

	out.Status = infrav1.CloudStackClusterStatus{
		FailureDomains: in.Status.FailureDomains,
		Ready:          in.Status.Ready,
	}

	return nil
}

func Convert_v1beta3_CloudStackCluster_To_v1beta1_CloudStackCluster(in *infrav1.CloudStackCluster, out *CloudStackCluster, _ machineryconversion.Scope) error {
	if len(in.Spec.FailureDomains) < 1 {
		return errors.New("v1beta3 to v1beta1 conversion not supported when < 1 failure domain is provided")
	}
	out.ObjectMeta = in.ObjectMeta
	out.Spec = CloudStackClusterSpec{
		Account:              in.Spec.FailureDomains[0].Account,
		Domain:               in.Spec.FailureDomains[0].Domain,
		Zones:                getZones(in),
		ControlPlaneEndpoint: in.Spec.ControlPlaneEndpoint,
	}

	out.Status = CloudStackClusterStatus{
		FailureDomains: in.Status.FailureDomains,
		Ready:          in.Status.Ready,
	}

	return nil
}

func Convert_v1beta3_Network_To_v1beta1_Network(in *infrav1.Network, out *Network, s machineryconversion.Scope) error {
	return autoConvert_v1beta3_Network_To_v1beta1_Network(in, out, s)
}

// getZones maps failure domains to zones.
func getZones(csCluster *infrav1.CloudStackCluster) []Zone {
	zones := make([]Zone, 0, len(csCluster.Status.FailureDomains))
	for _, failureDomain := range csCluster.Spec.FailureDomains {
		zone := failureDomain.Zone
		zones = append(zones, Zone{
			Name: zone.Name,
			ID:   zone.ID,
			Network: Network{
				Name: zone.Network.Name,
				ID:   zone.Network.ID,
				Type: zone.Network.Type,
			},
		})
	}

	return zones
}

// GetFailureDomains maps v1beta1 zones to v1beta3 failure domains.
func GetFailureDomains(csCluster *CloudStackCluster) ([]infrav1.CloudStackFailureDomainSpec, error) {
	failureDomains := make([]infrav1.CloudStackFailureDomainSpec, 0, len(csCluster.Spec.Zones))
	namespace := csCluster.Namespace
	for _, zone := range csCluster.Spec.Zones {
		name, err := GetDefaultFailureDomainName(namespace, zone.ID, zone.Name)
		if err != nil {
			return nil, err
		}
		failureDomains = append(failureDomains, infrav1.CloudStackFailureDomainSpec{
			Name: name,
			Zone: infrav1.CloudStackZoneSpec{
				ID:   zone.ID,
				Name: zone.Name,
				Network: infrav1.Network{
					ID:   zone.Network.ID,
					Name: zone.Network.Name,
					Type: zone.Network.Type,
				},
			},
			Domain:  csCluster.Spec.Domain,
			Account: csCluster.Spec.Account,
			ACSEndpoint: corev1.SecretReference{
				Namespace: namespace,
				Name:      DefaultEndpointCredential,
			},
		})
	}

	return failureDomains, nil
}

// GetDefaultFailureDomainName return zoneID as failuredomain name.
// Default failure domain name is used when migrating an old cluster to a multiple-endpoints supported cluster, that
// requires to convert each zone to a failure domain.
// When upgrading cluster using eks-a, a secret named global will be created by eks-a, and it is used by following
// method to get zoneID by calling cloudstack API.
// When upgrading cluster using clusterctl directly, zoneID is fetched directly from kubernetes cluster in cloudstackzones.
func GetDefaultFailureDomainName(namespace string, zoneID string, zoneName string) (string, error) {
	if len(zoneID) > 0 {
		return zoneID, nil
	}

	secret, err := GetK8sSecret(DefaultEndpointCredential, namespace)
	if err != nil {
		return "", err
	}

	// try fetch zoneID using zoneName through cloudstack client.
	zoneID, err = fetchZoneIDUsingCloudStack(secret, zoneName)
	if err == nil {
		return zoneID, nil
	}

	zoneID, err = fetchZoneIDUsingK8s(namespace, zoneName)
	if err != nil {
		return "", err
	}

	return zoneID, nil
}

func fetchZoneIDUsingK8s(namespace string, zoneName string) (string, error) {
	zone := &CloudStackZone{}
	key := client.ObjectKey{Name: zoneName, Namespace: namespace}
	if err := infrav1.K8sClient.Get(context.TODO(), key, zone); err != nil {
		return "", err
	}

	return zone.Spec.ID, nil
}

func fetchZoneIDUsingCloudStack(secret *corev1.Secret, zoneName string) (string, error) {
	client, err := cloud.NewClientFromK8sSecret(secret, nil)
	if err != nil {
		return "", err
	}
	zone := &infrav1.CloudStackZoneSpec{Name: zoneName}
	err = client.ResolveZone(zone)

	return zone.ID, err
}

func GetK8sSecret(name, namespace string) (*corev1.Secret, error) {
	endpointCredentials := &corev1.Secret{}
	key := client.ObjectKey{Name: name, Namespace: namespace}
	if err := infrav1.K8sClient.Get(context.TODO(), key, endpointCredentials); err != nil {
		return nil, err
	}

	return endpointCredentials, nil
}
