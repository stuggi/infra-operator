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

package network

import (
	"context"
	"fmt"
	"net"
	"strings"
	"time"

	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"github.com/go-logr/logr"
	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"

	dnsmasq "github.com/openstack-k8s-operators/infra-operator/pkg/dnsmasq"
	common "github.com/openstack-k8s-operators/lib-common/modules/common"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	configmap "github.com/openstack-k8s-operators/lib-common/modules/common/configmap"
	deployment "github.com/openstack-k8s-operators/lib-common/modules/common/deployment"
	endpoint "github.com/openstack-k8s-operators/lib-common/modules/common/endpoint"
	env "github.com/openstack-k8s-operators/lib-common/modules/common/env"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	labels "github.com/openstack-k8s-operators/lib-common/modules/common/labels"
	nad "github.com/openstack-k8s-operators/lib-common/modules/common/networkattachment"
	util "github.com/openstack-k8s-operators/lib-common/modules/common/util"
)

// DNSMasqReconciler reconciles a DNSMasq object
type DNSMasqReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Log     logr.Logger
	Scheme  *runtime.Scheme
}

//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsmasqs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsmasqs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsmasqs/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete;
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete;
//+kubebuilder:rbac:groups=k8s.cni.cncf.io,resources=network-attachment-definitions,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *DNSMasqReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, _err error) {
	_ = log.FromContext(ctx)

	// Fetch the DNSMasq instance
	instance := &networkv1.DNSMasq{}
	err := r.Client.Get(ctx, req.NamespacedName, instance)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected.
			// For additional cleanup logic use finalizers. Return and don't requeue.
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, err
	}

	helper, err := helper.NewHelper(
		instance,
		r.Client,
		r.Kclient,
		r.Scheme,
		r.Log,
	)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Always patch the instance status when exiting this function so we can persist any changes.
	defer func() {
		// update the overall status condition if service is ready
		if instance.IsReady() {
			instance.Status.Conditions.MarkTrue(condition.ReadyCondition, condition.ReadyMessage)
		}

		err := helper.PatchInstance(ctx, instance)
		if err != nil {
			_err = err
			return
		}
	}()

	// If we're not deleting this and the service object doesn't have our finalizer, add it.
	if instance.DeletionTimestamp.IsZero() && controllerutil.AddFinalizer(instance, helper.GetFinalizer()) {
		return ctrl.Result{}, err
	}

	// initialize status
	if instance.Status.Conditions == nil {
		instance.Status.Conditions = condition.Conditions{}

		cl := condition.CreateList(
			condition.UnknownCondition(condition.InputReadyCondition, condition.InitReason, condition.InputReadyInitMessage),
			condition.UnknownCondition(condition.ServiceConfigReadyCondition, condition.InitReason, condition.ServiceConfigReadyInitMessage),
			condition.UnknownCondition(condition.DeploymentReadyCondition, condition.InitReason, condition.DeploymentReadyInitMessage),
		)

		instance.Status.Conditions.Init(&cl)

		// Register overall status immediately to have an early feedback e.g. in the cli
		return ctrl.Result{}, nil
	}

	// Handle service delete
	if !instance.DeletionTimestamp.IsZero() {
		return r.reconcileDelete(ctx, instance, helper)
	}

	// Handle non-deleted clusters
	return r.reconcileNormal(ctx, instance, helper)
}

// SetupWithManager sets up the controller with the Manager.
func (r *DNSMasqReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1.DNSMasq{}).
		Owns(&appsv1.Deployment{}).
		Owns(&corev1.Service{}).
		Watches(&source.Kind{Type: &corev1.Service{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Complete(r)
}

func (r *DNSMasqReconciler) reconcileDelete(ctx context.Context, instance *networkv1.DNSMasq, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service delete")

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	r.Log.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

func (r *DNSMasqReconciler) reconcileNormal(ctx context.Context, instance *networkv1.DNSMasq, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service")

	serviceLabels := map[string]string{
		common.AppSelector: dnsmasq.ServiceName,
	}

	// networks to attach to
	for _, netAtt := range instance.Spec.NetworkAttachments {
		_, err := nad.GetNADWithName(ctx, helper, netAtt, instance.Namespace)
		if err != nil {
			if k8s_errors.IsNotFound(err) {
				instance.Status.Conditions.Set(condition.FalseCondition(
					condition.NetworkAttachmentsReadyCondition,
					condition.RequestedReason,
					condition.SeverityInfo,
					condition.NetworkAttachmentsReadyWaitingMessage,
					netAtt))
				return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("network-attachment-definition %s not found", netAtt)
			}
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.NetworkAttachmentsReadyCondition,
				condition.ErrorReason,
				condition.SeverityWarning,
				condition.NetworkAttachmentsReadyErrorMessage,
				err.Error()))
			return ctrl.Result{}, err
		}
	}

	serviceAnnotations, err := nad.CreateNetworksAnnotation(instance.Namespace, instance.Spec.NetworkAttachments)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("failed create network annotation from %s: %w",
			instance.Spec.NetworkAttachments, err)
	}

	//
	// check for required configmap instance.Spec.DNSData holding data for the dns service
	//
	configMapVars := make(map[string]env.Setter)
	data, hash, err := configmap.GetConfigMapAndHashWithName(ctx, helper, "test", instance.Namespace)
	if err != nil {
		if k8s_errors.IsNotFound(err) {
			instance.Status.Conditions.Set(condition.FalseCondition(
				condition.InputReadyCondition,
				condition.RequestedReason,
				condition.SeverityInfo,
				condition.InputReadyWaitingMessage))
			return ctrl.Result{RequeueAfter: time.Second * 10}, fmt.Errorf("dnsmasq data config map %s not found", "Test")
		}
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.InputReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.InputReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}
	configMapVars[data.Name] = env.SetValue(hash)

	instance.Status.Conditions.MarkTrue(condition.InputReadyCondition, condition.InputReadyMessage)

	//
	// create Configmap for dnsmasq input
	//
	err = r.generateServiceConfigMaps(ctx, helper, instance, &configMapVars)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ServiceConfigReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.ServiceConfigReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	//
	// create hash over all the different input resources to identify if any those changed
	// and a restart/recreate is required.
	//
	_, hashChanged, err := r.createHashOfInputHashes(ctx, instance, configMapVars)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ServiceConfigReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.ServiceConfigReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	} else if hashChanged {
		// Hash changed and instance status should be updated (which will be done by main defer func),
		// so we need to return and reconcile again
		return ctrl.Result{}, nil
	}
	// run check OpenStack secret - end

	//
	// expose the service (create service, route and return the created endpoint URLs)
	//
	var dnsEndpointCfg = map[endpoint.Endpoint]endpoint.Data{
		endpoint.Endpoint(instance.Name): endpoint.Data{
			Port: dnsmasq.DNSPort,
		},
	}

	for _, metallbcfg := range instance.Spec.ExternalEndpoints {
		portCfg := endpoint.Data{
			MetalLB: &endpoint.MetalLBData{
				IPAddressPool:   metallbcfg.IPAddressPool,
				SharedIP:        metallbcfg.SharedIP,
				SharedIPKey:     metallbcfg.SharedIPKey,
				LoadBalancerIPs: metallbcfg.LoadBalancerIPs,
			},
		}

		dnsEndpointCfg[endpoint.Endpoint(metallbcfg.IPAddressPool)] = portCfg
	}

	_, ctrlResult, err := endpoint.ExposeEndpoints(
		ctx,
		helper,
		dnsmasq.ServiceName,
		serviceLabels,
		dnsEndpointCfg,
		time.Duration(5)*time.Second,
	)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ExposeServiceReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.ExposeServiceReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.ExposeServiceReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.ExposeServiceReadyRunningMessage))
		return ctrlResult, nil
	}
	instance.Status.Conditions.MarkTrue(condition.ExposeServiceReadyCondition, condition.ExposeServiceReadyMessage)

	// Define a new Deployment object
	deplDef := dnsmasq.Deployment(instance, instance.Status.Hash[common.InputHashName], serviceLabels, serviceAnnotations)
	depl := deployment.NewDeployment(
		deplDef,
		time.Duration(5)*time.Second,
	)

	ctrlResult, err = depl.CreateOrPatch(ctx, helper)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.DeploymentReadyErrorMessage,
			err.Error()))
		return ctrlResult, err
	} else if (ctrlResult != ctrl.Result{}) {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.DeploymentReadyCondition,
			condition.RequestedReason,
			condition.SeverityInfo,
			condition.DeploymentReadyRunningMessage))
		return ctrlResult, nil
	}
	instance.Status.ReadyCount = depl.GetDeployment().Status.ReadyReplicas

	// verify if network attachment matches expectations
	networkReady, networkAttachmentStatus, err := nad.VerifyNetworkStatusFromAnnotation(ctx, helper, instance.Spec.NetworkAttachments, serviceLabels, instance.Status.ReadyCount)
	if err != nil {
		return ctrl.Result{}, err
	}

	instance.Status.NetworkAttachments = networkAttachmentStatus
	if networkReady {
		instance.Status.Conditions.MarkTrue(condition.NetworkAttachmentsReadyCondition, condition.NetworkAttachmentsReadyMessage)
	} else {
		err := fmt.Errorf("not all pods have interfaces with ips as configured in NetworkAttachments: %s", instance.Spec.NetworkAttachments)
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.NetworkAttachmentsReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.NetworkAttachmentsReadyErrorMessage,
			err.Error()))

		return ctrl.Result{}, err
	}

	if instance.Status.ReadyCount > 0 {
		instance.Status.Conditions.MarkTrue(condition.DeploymentReadyCondition, condition.DeploymentReadyMessage)
	}
	// create Deployment - end

	r.Log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

// createHashOfInputHashes - creates a hash of hashes which gets added to the resources which requires a restart
// if any of the input resources change, like configs, passwords, ...
//
// returns the hash, whether the hash changed (as a bool) and any error
func (r *DNSMasqReconciler) createHashOfInputHashes(
	ctx context.Context,
	instance *networkv1.DNSMasq,
	envVars map[string]env.Setter,
) (string, bool, error) {
	var hashMap map[string]string
	changed := false
	mergedMapVars := env.MergeEnvs([]corev1.EnvVar{}, envVars)
	hash, err := util.ObjectHash(mergedMapVars)
	if err != nil {
		return hash, changed, err
	}
	if hashMap, changed = util.SetHash(instance.Status.Hash, common.InputHashName, hash); changed {
		instance.Status.Hash = hashMap
		r.Log.Info(fmt.Sprintf("Input maps hash %s - %s", common.InputHashName, hash))
	}
	return hash, changed, nil
}

// generateServiceConfigMaps - create create configmaps which hold service configuration
func (r *DNSMasqReconciler) generateServiceConfigMaps(
	ctx context.Context,
	h *helper.Helper,
	instance *networkv1.DNSMasq,
	envVars *map[string]env.Setter,
) error {
	//
	// create Configmap/Secret required for placement input
	// - %-scripts configmap holding scripts to e.g. bootstrap the service
	// - %-config configmap holding minimal placement config required to get the service up, user can add additional files to be added to the service
	// - parameters which has passwords gets added from the ospSecret via the init container
	//

	cmLabels := labels.GetLabels(instance, labels.GetGroupLabel(dnsmasq.ServiceName), map[string]string{})

	// DHCPRange - enable the integrated DHCP server on the range of addresses available for lease and optionally
	// a lease time. For each network which DHCP needs to be provided a range config is required.
	// service.

	configMapData := map[string]string{}
	if instance.Spec.DHCPRanges != nil {

		for _, dhcpRange := range instance.Spec.DHCPRanges {
			start := net.ParseIP(dhcpRange.Start)
			if start == nil {
				return fmt.Errorf(fmt.Sprintf("unrecognized address %s", start))
			}
			end := net.ParseIP(dhcpRange.End)
			if end == nil {
				return fmt.Errorf(fmt.Sprintf("unrecognized address %s", end))
			}
			configMapData[dnsmasq.CustomServiceConfigFileName] = configMapData[dnsmasq.CustomServiceConfigFileName] + "\n" +
				dnsmasqDHCPRange(start, end) + "\n" +
				dnsmasqDomain(dhcpRange.Domain, start, end)
		}
	}
	if instance.Spec.CustomServiceConfig != "" {
		configMapData[dnsmasq.CustomServiceConfigFileName] = configMapData[dnsmasq.CustomServiceConfigFileName] + "\n" + instance.Spec.CustomServiceConfig
	}

	cms := []util.Template{
		{
			Name:         strings.ToLower(instance.Name),
			Namespace:    instance.Namespace,
			Type:         util.TemplateTypeNone,
			InstanceType: instance.Kind,
			CustomData:   configMapData,
			Labels:       cmLabels,
		},
	}

	return configmap.EnsureConfigMaps(ctx, h, instance, cms, envVars)
}

// dnsmasqDHCPRange -
func dnsmasqDHCPRange(
	start net.IP,
	end net.IP,
) string {
	return fmt.Sprintf("dhcp-range=%s,%s,infinite", start.String(), end.String())
}

// dnsmasqDomain -
func dnsmasqDomain(
	domain string,
	start net.IP,
	end net.IP,
) string {
	return fmt.Sprintf("domain=%s,%s,%s", domain, start.String(), end.String())
}
