/*
Copyright 2023.

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
	"github.com/openstack-k8s-operators/lib-common/modules/common/condition"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DNSDataSpec defines the desired state of DNSData
type DNSDataSpec struct {
	// +kubebuilder:validation:Required
	// +kubebuilder:validation:Pattern=`[a-z0-9]([-a-z0-9]*[a-z0-9])?(\\.[a-z0-9]([-a-z0-9]*[a-z0-9])?)*`
	// ConfigMap containing data for dns resolution mounted to /etc/dnsmasq.d inside the deployment.
	// TODO: expected data
	DNSData string `json:"dnsData"`
}

// DNSDataStatus defines the observed state of DNSData
type DNSDataStatus struct {
	// Conditions
	Conditions condition.Conditions `json:"conditions,omitempty" optional:"true"`

	// Map of the dns data configmap
	Hash string `json:"hash,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// DNSData is the Schema for the dnsdata API
type DNSData struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSDataSpec   `json:"spec,omitempty"`
	Status DNSDataStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DNSDataList contains a list of DNSData
type DNSDataList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSData `json:"items"`
}

func init() {
	SchemeBuilder.Register(&DNSData{}, &DNSDataList{})
}

// IsReady - returns true if service is ready to server requests
func (instance DNSData) IsReady() bool {
	ready := instance.Status.Hash != ""

	return ready
}
