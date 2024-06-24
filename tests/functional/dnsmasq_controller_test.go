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
	. "github.com/onsi/ginkgo/v2" //revive:disable:dot-imports
	. "github.com/onsi/gomega"    //revive:disable:dot-imports

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"

	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	"github.com/openstack-k8s-operators/lib-common/modules/common/util"

	//revive:disable-next-line:dot-imports
	. "github.com/openstack-k8s-operators/lib-common/modules/common/test/helpers"
)

var _ = Describe("DNSMasq controller", func() {
	var dnsMasqName types.NamespacedName
	var dnsMasqServiceAccountName types.NamespacedName
	var dnsMasqRoleName types.NamespacedName
	var dnsMasqRoleBindingName types.NamespacedName
	var deploymentName types.NamespacedName
	var dnsDataCM types.NamespacedName

	When("A DNSMasq is created", func() {
		BeforeEach(func() {
			instance := CreateDNSMasq(namespace, GetDefaultDNSMasqSpec())
			dnsMasqName = types.NamespacedName{
				Name:      instance.GetName(),
				Namespace: namespace,
			}
			dnsMasqServiceAccountName = types.NamespacedName{
				Namespace: namespace,
				Name:      "dnsmasq-" + dnsMasqName.Name,
			}
			dnsMasqRoleName = types.NamespacedName{
				Namespace: namespace,
				Name:      dnsMasqServiceAccountName.Name + "-role",
			}
			dnsMasqRoleBindingName = types.NamespacedName{
				Namespace: namespace,
				Name:      dnsMasqServiceAccountName.Name + "-rolebinding",
			}
			deploymentName = types.NamespacedName{
				Namespace: namespace,
				Name:      "dnsmasq-" + dnsMasqName.Name,
			}

			dnsDataCM = types.NamespacedName{
				Namespace: namespace,
				Name:      "some-dnsdata",
			}

			th.CreateConfigMap(dnsDataCM, map[string]interface{}{
				dnsDataCM.Name: "172.20.0.80 keystone-internal.openstack.svc",
			})
			cm := th.GetConfigMap(dnsDataCM)
			cm.Labels = util.MergeStringMaps(cm.Labels, map[string]string{
				"dnsmasqhosts": "dnsdata",
			})
			Expect(th.K8sClient.Update(ctx, cm)).Should(Succeed())

			DeferCleanup(th.DeleteConfigMap, dnsDataCM)
			DeferCleanup(th.DeleteInstance, instance)
		})

		It("should have the Spec and Status fields initialized", func() {
			instance := GetDNSMasq(dnsMasqName)
			Expect(instance.Status.Hash).To(BeEmpty())
			Expect(instance.Status.ReadyCount).To(Equal(int32(0)))
			Expect(instance.Spec.ContainerImage).Should(Equal(containerImage))
		})

		It("creates service account, role and rolebindig", func() {
			th.ExpectCondition(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.ServiceAccountReadyCondition,
				corev1.ConditionTrue,
			)
			sa := th.GetServiceAccount(dnsMasqServiceAccountName)

			th.ExpectCondition(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.RoleReadyCondition,
				corev1.ConditionTrue,
			)
			role := th.GetRole(dnsMasqRoleName)
			Expect(role.Rules).To(HaveLen(2))
			Expect(role.Rules[0].Resources).To(Equal([]string{"securitycontextconstraints"}))
			Expect(role.Rules[1].Resources).To(Equal([]string{"pods"}))

			th.ExpectCondition(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.RoleBindingReadyCondition,
				corev1.ConditionTrue,
			)
			binding := th.GetRoleBinding(dnsMasqRoleBindingName)
			Expect(binding.RoleRef.Name).To(Equal(role.Name))
			Expect(binding.Subjects).To(HaveLen(1))
			Expect(binding.Subjects[0].Name).To(Equal(sa.Name))
		})

		It("generated a ConfigMap holding dnsmasq key=>value config options", func() {
			th.ExpectCondition(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.ServiceConfigReadyCondition,
				corev1.ConditionTrue,
			)

			configData := th.GetConfigMap(dnsMasqName)
			Expect(configData).ShouldNot(BeNil())
			Expect(configData.Data[dnsMasqName.Name]).Should(
				ContainSubstring("server=1.1.1.1"))
			Expect(configData.Data[dnsMasqName.Name]).Should(
				ContainSubstring("no-negcache\n"))
			Expect(configData.Labels["dnsmasq.openstack.org/name"]).To(Equal(dnsMasqName.Name))
		})

		It("stored the input hash in the Status", func() {
			Eventually(func(g Gomega) {
				instance := GetDNSMasq(dnsMasqName)
				g.Expect(instance.Status.Hash).Should(HaveKeyWithValue("input", Not(BeEmpty())))
			}, timeout, interval).Should(Succeed())
		})

		It("exposes the service", func() {
			svcNames := SimulateLBsReady(deploymentName)
			th.ExpectCondition(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.ExposeServiceReadyCondition,
				corev1.ConditionTrue,
			)

			for _, svcName := range svcNames {
				svc := th.GetService(svcName)
				Expect(svc.Labels["service"]).To(Equal("dnsmasq"))
			}
		})

		It("creates a Deployment for the service", func() {
			SimulateLBsReady(deploymentName)
			th.ExpectConditionWithDetails(
				dnsMasqName,
				ConditionGetterFunc(DNSMasqConditionGetter),
				condition.DeploymentReadyCondition,
				corev1.ConditionFalse,
				condition.RequestedReason,
				condition.DeploymentReadyRunningMessage,
			)

			Eventually(func(g Gomega) {
				depl := th.GetDeployment(deploymentName)

				g.Expect(int(*depl.Spec.Replicas)).To(Equal(1))
				g.Expect(depl.Spec.Template.Spec.Volumes).To(HaveLen(3))
				g.Expect(depl.Spec.Template.Spec.Containers).To(HaveLen(1))
				g.Expect(depl.Spec.Template.Spec.InitContainers).To(HaveLen(1))
				g.Expect(depl.Spec.Selector.MatchLabels).To(Equal(map[string]string{"service": "dnsmasq"}))

				container := depl.Spec.Template.Spec.Containers[0]
				g.Expect(container.VolumeMounts).To(HaveLen(3))
				g.Expect(container.Image).To(Equal(containerImage))

				g.Expect(container.LivenessProbe.TCPSocket.Port.IntVal).To(Equal(int32(53)))
				g.Expect(container.ReadinessProbe.TCPSocket.Port.IntVal).To(Equal(int32(53)))
			}, timeout, interval).Should(Succeed())
		})

		When("the DNSData CM gets updated", func() {
			It("the CONFIG_HASH on the deployment changes", func() {
				SimulateLBsReady(deploymentName)
				cm := th.GetConfigMap(dnsDataCM)
				configHash := ""
				Eventually(func(g Gomega) {
					depl := th.GetDeployment(deploymentName)
					g.Expect(depl.Spec.Template.Spec.Volumes).To(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))

					container := depl.Spec.Template.Spec.Containers[0]
					g.Expect(container.VolumeMounts).To(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))

					configHash = GetEnvVarValue(container.Env, "CONFIG_HASH", "")
					g.Expect(configHash).To(Not(Equal("")))
				}, timeout, interval).Should(Succeed())

				// Update the cm providing dnsdata
				cm.Data[dnsDataCM.Name] = "172.20.0.80 keystone-internal.openstack.svc some-other-node"
				Expect(th.K8sClient.Update(ctx, cm)).Should(Succeed())

				Eventually(func(g Gomega) {
					depl := th.GetDeployment(deploymentName)
					container := depl.Spec.Template.Spec.Containers[0]
					newConfigHash := GetEnvVarValue(container.Env, "CONFIG_HASH", "")
					g.Expect(newConfigHash).To(Not(Equal("")))
					g.Expect(newConfigHash).To(Not(Equal(configHash)))
				}, timeout, interval).Should(Succeed())
			})
		})

		When("the DNSData CM gets deleted", func() {
			It("the ConfigMap gets removed from the deployment", func() {
				SimulateLBsReady(deploymentName)
				th.GetConfigMap(dnsDataCM)
				Eventually(func(g Gomega) {
					depl := th.GetDeployment(deploymentName)
					g.Expect(depl.Spec.Template.Spec.Volumes).To(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))

					container := depl.Spec.Template.Spec.Containers[0]
					g.Expect(container.VolumeMounts).To(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))
				}, timeout, interval).Should(Succeed())

				// Delete the cm providing dnsdata
				th.DeleteConfigMap(dnsDataCM)
				Eventually(func(g Gomega) {
					depl := th.GetDeployment(deploymentName)
					g.Expect(depl.Spec.Template.Spec.Volumes).NotTo(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))

					container := depl.Spec.Template.Spec.Containers[0]
					g.Expect(container.VolumeMounts).NotTo(
						ContainElement(HaveField("Name", Equal(dnsDataCM.Name))))
				}, timeout, interval).Should(Succeed())
			})
		})

		When("the CR is deleted", func() {
			It("deletes the generated ConfigMaps", func() {
				th.ExpectCondition(
					dnsMasqName,
					ConditionGetterFunc(DNSMasqConditionGetter),
					condition.ServiceConfigReadyCondition,
					corev1.ConditionTrue,
				)

				th.DeleteInstance(GetDNSMasq(dnsMasqName))

				Eventually(func() []corev1.ConfigMap {
					return th.ListConfigMaps(dnsMasqName.Name).Items
				}, timeout, interval).Should(BeEmpty())
			})
		})
	})
})
