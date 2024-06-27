/*
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

package instanceha

import (
	"context"

	instancehav1 "github.com/openstack-k8s-operators/infra-operator/apis/instanceha/v1beta1"
	env "github.com/openstack-k8s-operators/lib-common/modules/common/env"
	"github.com/openstack-k8s-operators/lib-common/modules/common/helper"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

func Deployment(
	ctx context.Context,
	instance *instancehav1.InstanceHA,
	helper *helper.Helper,
	labels map[string]string,
	annotations map[string]string,
	openstackcloud string,
	configHash string,
) *appsv1.Deployment {

	replicas := int32(1)

	envVars := map[string]env.Setter{}
	envVars["OS_CLOUD"] = env.SetValue(openstackcloud)
	envVars["CONFIG_HASH"] = env.SetValue(configHash)

	// create Volume and VolumeMounts
	volumes := instancehaPodVolumes(instance)
	volumeMounts := instancehaPodVolumeMounts()

	// add CA cert if defined
	if instance.Spec.CaBundleSecretName != "" {
		volumes = append(volumes, instance.Spec.CreateVolume())
		volumeMounts = append(volumeMounts, instance.Spec.CreateVolumeMounts(nil)...)
	}

	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      instance.Name,
			Namespace: instance.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Strategy: appsv1.DeploymentStrategy{
				Type: "Recreate",
			},
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels:      labels,
					Annotations: annotations,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName:            instance.RbacResourceName(),
					Volumes:                       volumes,
					TerminationGracePeriodSeconds: ptr.To[int64](0),
					NodeSelector:                  instance.Spec.NodeSelector,
					Containers: []corev1.Container{{
						Name:    "instanceha",
						Image:   instance.Spec.ContainerImage,
						Command: []string{"/usr/bin/python3", "-u", "/var/lib/instanceha/instanceha.py"},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser:                ptr.To[int64](42401),
							RunAsGroup:               ptr.To[int64](42401),
							RunAsNonRoot:             ptr.To(true),
							AllowPrivilegeEscalation: ptr.To(false),
							Capabilities: &corev1.Capabilities{
								Drop: []corev1.Capability{
									"ALL",
								},
							},
						},
						Env: env.MergeEnvs([]corev1.EnvVar{}, envVars),
						Ports: []corev1.ContainerPort{{
							ContainerPort: instance.Spec.InstanceHAKdumpPort,
							Protocol:      "UDP",
							Name:          "instanceha",
						}},
						VolumeMounts: volumeMounts,
					}},
				},
			},
		},
	}

	return dep
}

func instancehaPodVolumeMounts() []corev1.VolumeMount {
	return []corev1.VolumeMount{
		{
			Name:      "openstack-config",
			MountPath: "/home/cloud-admin/.config/openstack/clouds.yaml",
			SubPath:   "clouds.yaml",
		},
		{
			Name:      "openstack-config-secret",
			MountPath: "/home/cloud-admin/.config/openstack/secure.yaml",
			SubPath:   "secure.yaml",
		},
		{
			Name:      "fencing-secret",
			MountPath: "/secrets/fencing.yaml",
			SubPath:   "fencing.yaml",
		},
		{
			Name:      "instanceha-script",
			MountPath: "/var/lib/instanceha/instanceha.py",
			SubPath:   "instanceha.py",
			ReadOnly:  true,
		},
		{
			Name:      "instanceha-config",
			MountPath: "/var/lib/instanceha/config.yaml",
			SubPath:   "config.yaml",
			ReadOnly:  true,
		},
	}
}

func instancehaPodVolumes(
	instance *instancehav1.InstanceHA,
) []corev1.Volume {

	var config0644AccessMode int32 = 0644
	return []corev1.Volume{
		{
			Name: "openstack-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *instance.Spec.OpenStackConfigMap,
					},
				},
			},
		},
		{
			Name: "openstack-config-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: *instance.Spec.OpenStackConfigSecret,
				},
			},
		},
		{
			Name: "fencing-secret",
			VolumeSource: corev1.VolumeSource{
				Secret: &corev1.SecretVolumeSource{
					SecretName: *instance.Spec.FencingSecret,
				},
			},
		},
		{
			Name: "instanceha-script",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					DefaultMode: &config0644AccessMode,
					LocalObjectReference: corev1.LocalObjectReference{
						Name: instance.Name + "-sh",
					},
				},
			},
		},
		{
			Name: "instanceha-config",
			VolumeSource: corev1.VolumeSource{
				ConfigMap: &corev1.ConfigMapVolumeSource{
					LocalObjectReference: corev1.LocalObjectReference{
						Name: *instance.Spec.InstanceHAConfigMap,
					},
				},
			},
		},
	}
}
