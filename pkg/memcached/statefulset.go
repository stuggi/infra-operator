package memcached

import (
	memcachedv1 "github.com/openstack-k8s-operators/infra-operator/apis/memcached/v1beta1"
	labels "github.com/openstack-k8s-operators/lib-common/modules/common/labels"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

// StatefulSet returns a Stateful resource for the Memcached CR
func StatefulSet(m *memcachedv1.Memcached) *appsv1.StatefulSet {
	matchls := map[string]string{
		"app":   m.Name,
		"cr":    m.Name,
		"owner": "infra-operator",
	}
	ls := labels.GetLabels(m, "memcached", matchls)
	runAsUser := int64(0)

	livenessProbe := &corev1.Probe{
		// TODO might need tuning
		TimeoutSeconds:      5,
		PeriodSeconds:       3,
		InitialDelaySeconds: 3,
	}
	readinessProbe := &corev1.Probe{
		// TODO might need tuning
		TimeoutSeconds:      5,
		PeriodSeconds:       5,
		InitialDelaySeconds: 5,
	}

	livenessProbe.TCPSocket = &corev1.TCPSocketAction{
		Port: intstr.IntOrString{Type: intstr.Int, IntVal: MemcachedPort},
	}
	readinessProbe.TCPSocket = &corev1.TCPSocketAction{
		Port: intstr.IntOrString{Type: intstr.Int, IntVal: MemcachedPort},
	}

	sfs := &appsv1.StatefulSet{
		ObjectMeta: metav1.ObjectMeta{
			Name:      m.Name,
			Namespace: m.Namespace,
		},
		Spec: appsv1.StatefulSetSpec{
			ServiceName: m.Name,
			Replicas:    m.Spec.Replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: matchls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: ls,
				},
				Spec: corev1.PodSpec{
					ServiceAccountName: m.RbacResourceName(),
					Containers: []corev1.Container{{
						Image:   m.Spec.ContainerImage,
						Name:    "memcached",
						Command: []string{"/usr/bin/dumb-init", "--", "/usr/local/bin/kolla_start"},
						SecurityContext: &corev1.SecurityContext{
							RunAsUser: &runAsUser,
						},
						Env: []corev1.EnvVar{{
							Name:  "KOLLA_CONFIG_STRATEGY",
							Value: "COPY_ALWAYS",
						}, {
							Name: "POD_IPS",
							ValueFrom: &corev1.EnvVarSource{
								FieldRef: &corev1.ObjectFieldSelector{
									FieldPath: "status.podIPs",
								},
							},
						},
						},
						VolumeMounts: getVolumeMounts(m),
						Ports: []corev1.ContainerPort{{
							ContainerPort: MemcachedPort,
							Name:          "memcached",
						}, {
							ContainerPort: MemcachedTLSPort,
							Name:          "memcached-tls",
						}},
						ReadinessProbe: readinessProbe,
						LivenessProbe:  livenessProbe,
					}},
					Volumes: getVolumes(m),
				},
			},
		},
	}

	return sfs
}
