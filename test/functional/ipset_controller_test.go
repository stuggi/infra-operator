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

var _ = Describe("IPSet controller", func() {

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

	When("a IPSet is created", func() {
		BeforeEach(func() {
			instance := CreateIPSet(namespace, GetDefaultIPSetSpec())
			ipSetName = types.NamespacedName{
				Name:      instance.GetName(),
				Namespace: namespace,
			}

			DeferCleanup(th.DeleteInstance, instance)
		})

		It("should have created a IPSet", func() {
			Eventually(func(g Gomega) {
				ipset := GetIPSet(ipSetName)
				g.Expect(ipset).To(Not(BeNil()))
				g.Expect(ipset.Spec.Hostname).To(Not(BeNil()))
				g.Expect(*ipset.Spec.Hostname).To(Equal(networkv1.HostNameStr("host1")))
				g.Expect(ipset.Spec.Networks).To(HaveLen(1))
				net := ipset.Spec.Networks[0]
				g.Expect(net.Name).To(Equal(networkv1.NetNameStr("net-1")))
				g.Expect(net.SubnetName).To(Equal(networkv1.NetNameStr("subnet1")))
			}, timeout, interval).Should(Not(Succeed()))
		})
	})
})
