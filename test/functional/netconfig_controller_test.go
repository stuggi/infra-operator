/*
Copyright 2022.

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

package functional_test

import (
	"github.com/google/uuid"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/types"

	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
)

var _ = Describe("NetConfig controller", func() {

	var netCfgName types.NamespacedName
	var ipSetName types.NamespacedName
	var namespace string

	BeforeEach(func() {
		// NOTE(gibi): We need to create a unique namespace for each test run
		// as namespaces cannot be deleted in a locally running envtest. See
		// https://book.kubebuilder.io/reference/envtest.html#namespace-usage-limitation
		namespace = uuid.New().String()
		th.CreateNamespace(namespace)
		// We still request the delete of the Namespace to properly cleanup if
		// we run the test in an existing cluster.
		DeferCleanup(th.DeleteNamespace, namespace)

	})

	When("a default NetConfig is created", func() {
		BeforeEach(func() {
			netCfg := CreateNetConfig(namespace, GetDefaultNetConfigSpec())
			netCfgName = types.NamespacedName{
				Name:      netCfg.GetName(),
				Namespace: namespace,
			}

			DeferCleanup(th.DeleteInstance, netCfg)
		})

		It("should have created a NetConfig with a network and subnet definied", func() {
			Eventually(func(g Gomega) {
				netcfg := GetNetConfig(netCfgName)
				g.Expect(netcfg).To(Not(BeNil()))
				g.Expect(netcfg.Spec.Networks).To(HaveLen(1))
				net := netcfg.Spec.Networks[0]
				g.Expect(net.MTU).To(Equal(1400))
				g.Expect(net.Name).To(Equal(networkv1.NetNameStr("net-1")))
				subnet := net.Subnets[0]
				g.Expect(subnet.Name).To(Equal(networkv1.NetNameStr("subnet1")))
				g.Expect(subnet.AllocationRanges).To(HaveLen(1))
				g.Expect(subnet.Cidr).To(Equal("172.17.0.0/24"))
				g.Expect(subnet.Gateway).To(Not(BeNil()))
				g.Expect(*subnet.Gateway).To(Equal("172.17.0.1"))
				g.Expect(subnet.Routes).To(BeEmpty())
				g.Expect(subnet.Vlan).To(Equal(20))

			}, timeout, interval).Should(Succeed())
		})

		When("an IPSet gets created", func() {
			BeforeEach(func() {
				ipset := CreateIPSet(namespace, GetDefaultIPSetSpec())
				ipSetName = types.NamespacedName{
					Name:      ipset.GetName(),
					Namespace: namespace,
				}

				DeferCleanup(th.DeleteInstance, ipset)
			})
			It("should reconcile and create reservation", func() {
				res := &networkv1.Reservation{}
				Eventually(func(g Gomega) {
					ipSet := GetIPSet(ipSetName)
					g.Expect(ipSet).To(Not(BeNil()))
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					netcfg := GetNetConfig(netCfgName)
					g.Expect(netcfg).To(Not(BeNil()))
					g.Expect(netcfg.Spec.Networks).To(HaveLen(1))
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					res = GetReservation(ipSetName)
					g.Expect(res).To(Not(BeNil()))
					g.Expect(res.Spec.Reservations).To(HaveLen(1))
					g.Expect(res.Spec.Reservations["net-1"]).To(Not(BeNil()))
					net1Res := res.Spec.Reservations["net-1"]
					g.Expect(net1Res.Address).To(Equal("172.17.0.100"))
				}, timeout, interval).Should(Succeed())

				Eventually(func(g Gomega) {
					ipSet := GetIPSet(ipSetName)
					g.Expect(ipSet).To(Not(BeNil()))
					for _, ipSetRes := range ipSet.Status.Reservations {
						netRes, ok := res.Spec.Reservations[string(ipSetRes.Network)]
						g.Expect(ok).To(BeTrue())
						g.Expect(netRes.Address).To(Equal("172.17.0.100"))
					}
				}, timeout, interval).Should(Succeed())
			})

			/*
				It("The IPshould reconcile and create reservation", func() {
					Eventually(func(g Gomega) {
						ipSet := GetIPSet(ipSetName)
						g.Expect(ipSet).To(Not(BeNil()))
						netcfg := GetNetConfig(netCfgName)
						g.Expect(netcfg).To(Not(BeNil()))
						g.Expect(netcfg.Spec.Networks).To(HaveLen(1))
						res := GetReservation(ipSetName)
						g.Expect(res).To(Not(BeNil()))
						g.Expect(res.Spec.Reservations).To(HaveLen(1))
						g.Expect(res.Spec.Reservations["net-1"]).To(Not(BeNil()))
						net1Res := res.Spec.Reservations["net-1"]
						g.Expect(net1Res.Address).To(Equal(networkv1.IPAddressStr("172.17.0.100")))
					}, timeout, interval).Should(Succeed())
				})
			*/
		})
	})
})
