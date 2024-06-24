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
	"fmt"

	"github.com/openstack-k8s-operators/lib-common/modules/common/service"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

// DNSMasqDefaults -
type DNSMasqDefaults struct {
	ContainerImageURL string
}

var dnsMasqDefaults DNSMasqDefaults

// log is for logging in this package.
var dnsmasqlog = logf.Log.WithName("dnsmasq-resource")

// SetupDNSMasqDefaults - initialize DNSMasq spec defaults for use with either internal or external webhooks
func SetupDNSMasqDefaults(defaults DNSMasqDefaults) {
	dnsMasqDefaults = defaults
	dnsmasqlog.Info("DNSMasq defaults initialized", "defaults", defaults)
}

// SetupWebhookWithManager sets up the webhook with the Manager
func (r *DNSMasq) SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr).
		For(r).
		Complete()
}

//+kubebuilder:webhook:path=/mutate-network-openstack-org-v1beta1-dnsmasq,mutating=true,failurePolicy=fail,sideEffects=None,groups=network.openstack.org,resources=dnsmasqs,verbs=create;update,versions=v1beta1,name=mdnsmasq.kb.io,admissionReviewVersions=v1

var _ webhook.Defaulter = &DNSMasq{}

// Default implements webhook.Defaulter so a webhook will be registered for the type
func (r *DNSMasq) Default() {
	dnsmasqlog.Info("default", "name", r.Name)

	r.Spec.Default()
}

// Default - set defaults for this DNSMasq spec
func (spec *DNSMasqSpec) Default() {
	if spec.ContainerImage == "" {
		spec.ContainerImage = dnsMasqDefaults.ContainerImageURL
	}
	spec.DNSMasqSpecCore.Default()
}

// Default - common validations go here (for the OpenStackControlplane which uses this one)
func (spec *DNSMasqSpecCore) Default() {
	// nothing here

	// spec.Override.Service is deprecated, append it to spec.Override.Services
	// spec.Override.Service will be removed in a new api version
	if spec.Override.Service.Spec != nil {
		spec.Override.Services = append(spec.Override.Services, spec.Override.Service)
		spec.Override.Service = service.OverrideSpec{}
	}
}

// TODO(user): change verbs to "verbs=create;update;delete" if you want to enable deletion validation.
//+kubebuilder:webhook:path=/validate-network-openstack-org-v1beta1-dnsmasq,mutating=false,failurePolicy=fail,sideEffects=None,groups=network.openstack.org,resources=dnsmasqs,verbs=create;update,versions=v1beta1,name=vdnsmasq.kb.io,admissionReviewVersions=v1

var _ webhook.Validator = &DNSMasq{}

// ValidateCreate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSMasq) ValidateCreate() (admission.Warnings, error) {
	dnsmasqlog.Info("validate create", "name", r.Name)

	allErrs := field.ErrorList{}
	basePath := field.NewPath("spec")

	if err := r.Spec.ValidateCreate(basePath); err != nil {
		allErrs = append(allErrs, err...)
	}

	if len(allErrs) != 0 {
		return nil, apierrors.NewInvalid(GroupVersion.WithKind("DNSMasq").GroupKind(), r.Name, allErrs)
	}

	return nil, nil
}

// ValidateCreate - Exported function wrapping non-exported validate functions,
// this function can be called externally to validate an DNSMasq spec.
func (spec *DNSMasqSpec) ValidateCreate(basePath *field.Path) field.ErrorList {
	return spec.DNSMasqSpecCore.ValidateCreate(basePath)
}

func (spec *DNSMasqSpecCore) ValidateCreate(basePath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// nothing in here yet

	return allErrs
}

// ValidateUpdate implements webhook.Validator so a webhook will be registered for the type
func (r *DNSMasq) ValidateUpdate(old runtime.Object) (admission.Warnings, error) {
	dnsmasqlog.Info("validate update", "name", r.Name)

	oldDNSMasq, ok := old.(*DNSMasq)
	if !ok || oldDNSMasq == nil {
		return nil, apierrors.NewInternalError(fmt.Errorf("unable to convert existing object"))
	}

	allErrs := field.ErrorList{}
	basePath := field.NewPath("spec")

	if err := r.Spec.ValidateUpdate(oldDNSMasq.Spec, basePath); err != nil {
		allErrs = append(allErrs, err...)
	}

	if len(allErrs) != 0 {
		return nil, apierrors.NewInvalid(GroupVersion.WithKind("DNSMasq").GroupKind(), r.Name, allErrs)
	}

	return nil, nil
}

// ValidateUpdate - Exported function wrapping non-exported validate functions,
// this function can be called externally to validate an DNSMasq spec.
func (spec *DNSMasqSpec) ValidateUpdate(old DNSMasqSpec, basePath *field.Path) field.ErrorList {
	return spec.DNSMasqSpecCore.ValidateUpdate(old.DNSMasqSpecCore, basePath)
}

func (spec *DNSMasqSpecCore) ValidateUpdate(_ DNSMasqSpecCore, basePath *field.Path) field.ErrorList {
	var allErrs field.ErrorList

	// nothing in here yet

	return allErrs
}

// ValidateDelete implements webhook.Validator so a webhook will be registered for the type
func (r *DNSMasq) ValidateDelete() (admission.Warnings, error) {
	dnsmasqlog.Info("validate delete", "name", r.Name)

	// TODO(user): fill in your validation logic upon object deletion.
	return nil, nil
}
