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
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// IPAddress -
type IPAddress struct {
	// Network name
	Network NetNameStr `json:"network"`

	// Subnet name
	Subnet NetNameStr `json:"subnet"`

	// +kubebuilder:validation:Maximum=128
	// Prefix is the mask of the network as integer (max 128)
	Prefix int `json:"prefix"`

	// Address contains the IP address
	Address string `json:"address"`

	// Gateway is the gateway ip address
	Gateway *string `json:"gateway,omitempty"`

	// Routes, list of networks that should be routed via network gateway.
	Routes []Route `json:"routes,omitempty"`
}

// ReservationSpec defines the desired state of Reservation
type ReservationSpec struct {
	// Hostname
	Hostname *HostNameStr `json:"hostname"`

	// IPSetRef points to the IPSet object the IPs were created for.
	IPSetRef corev1.ObjectReference `json:"ipSetRef"`

	// +kubebuilder:validation:Required
	// Reservations, map (index network name) with reservation
	Reservations map[string]IPAddress `json:"reservations"`
}

// ReservationStatus defines the observed state of Reservation
type ReservationStatus struct {
	// Conditions
	Conditions condition.Conditions `json:"conditions,omitempty" optional:"true"`
	// Reservations
	Reservations []string `json:"reservations,omitempty" optional:"true"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[0].status",description="Ready"
//+kubebuilder:printcolumn:name="Message",type="string",JSONPath=".status.conditions[0].message",description="Message"
//+kubebuilder:printcolumn:name="Reservation",type="string",JSONPath=".status.reservations",description="Reservation"

// Reservation is the Schema for the reservations API
type Reservation struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ReservationSpec   `json:"spec,omitempty"`
	Status ReservationStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// ReservationList contains a list of Reservation
type ReservationList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Reservation `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Reservation{}, &ReservationList{})
}
