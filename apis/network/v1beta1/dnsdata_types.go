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

// DNSHost holds the mapping between IP and hostnames that will be added to dnsmasq hosts file.
type DNSHost struct {
	// +kubebuilder:validation:Required
	// IP address of the host file entry.
	IP string `json:"ip"`

	// +kubebuilder:validation:Required
	// Hostnames for the IP address.
	Hostnames []string `json:"hostnames"`
}

// DNSDataSpec defines the desired state of DNSData
type DNSDataSpec struct {
	// +kubebuilder:validation:Optional
	Hosts []DNSHost `json:"hosts,omitempty"`

	// +kubebuilder:validation:Optional
	// +kubebuilder:default="dnsdata"
	// Value of the DNSDataLabelSelector to set on the created configmaps containing hosts information
	DNSDataLabelSelectorValue string `json:"dnsDataLabelSelectorValue"`
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
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[0].status",description="Ready"
//+kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[0].message",description="Message"

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

// RbacConditionsSet - set the conditions for the rbac object
func (instance DNSData) RbacConditionsSet(c *condition.Condition) {
	instance.Status.Conditions.Set(c)
}

// RbacNamespace - return the namespace
func (instance DNSData) RbacNamespace() string {
	return instance.Namespace
}

// RbacResourceName - return the name to be used for rbac objects (serviceaccount, role, rolebinding)
func (instance DNSData) RbacResourceName() string {
	return "dnsdata-" + instance.Name
}
