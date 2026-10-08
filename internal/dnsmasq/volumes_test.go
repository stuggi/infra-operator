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

package dnsmasq

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func hasVolumeMount(mounts []corev1.VolumeMount, mountPath string) bool {
	for _, m := range mounts {
		if m.MountPath == mountPath {
			return true
		}
	}
	return false
}

func TestGetVolumeMountsAddsCNAMEsMountOnlyWhenKeyPresent(t *testing.T) {
	cms := &corev1.ConfigMapList{
		Items: []corev1.ConfigMap{
			{
				ObjectMeta: metav1.ObjectMeta{Name: "with-cnames"},
				Data: map[string]string{
					"with-cnames":        "1.2.3.4 host1\n",
					"with-cnames-cnames": "cname=alias,host1\n",
				},
			},
			{
				ObjectMeta: metav1.ObjectMeta{Name: "without-cnames"},
				Data: map[string]string{
					"without-cnames": "1.2.3.5 host2\n",
				},
			},
		},
	}

	mounts := getVolumeMounts("config", cms)

	if !hasVolumeMount(mounts, "/etc/dnsmasq.d/hosts/with-cnames") {
		t.Fatalf("expected hosts mount for with-cnames configmap")
	}
	if !hasVolumeMount(mounts, "/etc/dnsmasq.d/cnames/with-cnames") {
		t.Fatalf("expected cnames mount for configmap carrying the -cnames key")
	}
	if hasVolumeMount(mounts, "/etc/dnsmasq.d/cnames/without-cnames") {
		t.Fatalf("did not expect a cnames mount for configmap without the -cnames key")
	}
}
