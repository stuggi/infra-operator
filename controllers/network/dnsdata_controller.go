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
	"sort"
	"strconv"
	"strings"

	"github.com/go-logr/logr"
	"golang.org/x/exp/maps"
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

	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
	common "github.com/openstack-k8s-operators/lib-common/modules/common"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	configmap "github.com/openstack-k8s-operators/lib-common/modules/common/configmap"
	env "github.com/openstack-k8s-operators/lib-common/modules/common/env"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	util "github.com/openstack-k8s-operators/lib-common/modules/common/util"

	dnsmasq "github.com/openstack-k8s-operators/infra-operator/pkg/dnsmasq"
	corev1 "k8s.io/api/core/v1"
)

// DNSDataReconciler reconciles a DNSData object
type DNSDataReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Log     logr.Logger
	Scheme  *runtime.Scheme
}

type hostData struct {
	name        string
	hostrecords []string
	addresses   []string
	ptrRecords  []string
	dhcpRecords []string
}

type hostRecord struct {
	hostname  string
	addresses []networkData
}

type networkData struct {
	name      string
	addresses []net.IP
}

const hexDigit = "0123456789abcdef"

//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsdata,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsdata/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.openstack.org,resources=dnsdata/finalizers,verbs=update
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *DNSDataReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, _err error) {
	_ = log.FromContext(ctx)

	// Fetch the DNSData instance
	instance := &networkv1.DNSData{}
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
func (r *DNSDataReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1.DNSData{}).
		Watches(&source.Kind{Type: &corev1.Service{}},
			&handler.EnqueueRequestForObject{},
			builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		//For(&corev1.Service{}, builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Owns(&corev1.ConfigMap{}).
		//Watches(&source.Kind{Type: &corev1.Service{}},
		//	&handler.EnqueueRequestForObject{},
		//	builder.
		//	builder.WithPredicates(predicate.AnnotationChangedPredicate{})).
		Complete(r)
}

func (r *DNSDataReconciler) reconcileDelete(ctx context.Context, instance *networkv1.DNSData, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service delete")

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	r.Log.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

func (r *DNSDataReconciler) reconcileNormal(ctx context.Context, instance *networkv1.DNSData, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service")

	hosts := map[string]hostRecord{}
	hostAddrMap := map[string][]net.IP{}
	dnsData := map[string]hostData{}
	//dnsData := make(map[string]string)

	cmLabels := map[string]string{
		common.AppSelector: dnsmasq.ServiceName,
	}

	// services
	serviceAddrMap, err := r.getServiceDNSData(ctx, instance)
	if err != nil {
		return ctrl.Result{}, err
	}
	hostAddrMap = MergeIPMaps(hostAddrMap, serviceAddrMap)

	//dnsData = MergeMaps(dnsData, serviceDNSData)

	//util.MergeStringMaps(dnsData, serviceDNSData)

	// pods

	// ...

	hosts["test"] = hostRecord{
		hostname: "test",
		addresses: []networkData{
			{
				name: "internalapi",
				addresses: []net.IP{
					net.ParseIP("172.17.0.88"),
				},
			},
			{
				name: "storage",
				addresses: []net.IP{
					net.ParseIP("172.18.0.88"),
				},
			},
		},
	}

	hosts["test-statefulset-0"] = hostRecord{
		hostname: "test-statefulset-0",
		addresses: []networkData{
			{
				name: "internalapi",
				addresses: []net.IP{
					net.ParseIP("172.17.0.100"),
				},
			},
			{
				name: "storage",
				addresses: []net.IP{
					net.ParseIP("172.18.0.100"),
				},
			},
		},
	}

	// create dnsData configmap
	for _, hostRec := range hosts {
		dhcpRecords := []string{}
		hostRecords := []string{}
		for _, network := range hostRec.addresses {
			hostname := hostRec.hostname + "." + network.name
			for _, ip := range network.addresses {
				hostRecords = append(hostRecords, dnsmasqHostRecord(hostname, ip))
				dhcpRecords = append(dhcpRecords, dnsmasqDHCPHost(hostRec.hostname, ip))
			}
		}

		dnsData[hostRec.hostname] = hostData{
			name:        hostRec.hostname,
			hostrecords: hostRecords,
			dhcpRecords: dhcpRecords,
		}
	}
	/*
		hostAddrMap["test-statefulset-0"] = []net.IP{
			net.ParseIP("172.17.0.100"),
			net.ParseIP("172.18.0.100"),
		}
		hostAddrMap["test-statefulset-1"] = []net.IP{
			net.ParseIP("172.17.0.101"),
			net.ParseIP("172.18.0.101"),
		}
		hostAddrMap["test-statefulset-2"] = []net.IP{
			net.ParseIP("172.17.0.102"),
			net.ParseIP("172.18.0.102"),
		}

		// create dnsData configmap
		for hostname, addrs := range hostAddrMap {
			addresses := []string{}
			ptrRecords := []string{}
			dhcpRecords := []string{}
			hostRecords := []string{}
			for _, ip := range addrs {
				hostRecords = append(hostRecords, dnsmasqHostRecord(hostname, ip))
				addresses = append(addresses, dnsmasqAddressEntry(hostname, ip))
				ptrRecords = append(ptrRecords, dnsmasqPTRRecord(hostname, ip))
				dhcpRecords = append(dhcpRecords, dnsmasqDHCPHost(hostname, ip))
			}

			dnsData[hostname] = hostData{
				name:        hostname,
				hostrecords: hostRecords,
				addresses:   addresses,
				ptrRecords:  ptrRecords,
				dhcpRecords: dhcpRecords,
			}
		}
	*/
	r.Log.Info(fmt.Sprintf("booo dnsData %+v", dnsData))

	configMapData := map[string]string{}
	if len(dnsData) > 0 {
		// Create configmap with ordered entries by hostname to make sure the content won't change as
		// maps are not sorted.
		hostNames := maps.Keys(dnsData)
		sort.Strings(hostNames)

		for _, hostname := range hostNames {
			hostdata := strings.Join(dnsData[hostname].hostrecords, "\n") + "\n" +
				strings.Join(dnsData[hostname].dhcpRecords, "\n")

			//hostdata := strings.Join(dnsData[hostname].addresses, "\n") + "\n" +
			//	strings.Join(dnsData[hostname].ptrRecords, "\n") + "\n" +
			//	strings.Join(dnsData[hostname].dhcpRecords, "\n")
			configMapData[hostname] = hostdata + "\n"
		}
	}

	// create DNSData ConfigMap
	configMapVars := make(map[string]env.Setter)
	cms := []util.Template{
		{
			Name:         strings.ToLower(instance.Spec.DNSData),
			Namespace:    instance.Namespace,
			Type:         util.TemplateTypeNone,
			InstanceType: instance.Kind,
			CustomData:   configMapData,
			Labels:       cmLabels,
		},
	}

	// create configmap holding dns host files
	err = configmap.EnsureConfigMaps(ctx, helper, instance, cms, &configMapVars)
	if err != nil {
		instance.Status.Conditions.Set(condition.FalseCondition(
			condition.InputReadyCondition,
			condition.ErrorReason,
			condition.SeverityWarning,
			condition.InputReadyErrorMessage,
			err.Error()))
		return ctrl.Result{}, err
	}

	hash, err := util.ObjectHash(env.MergeEnvs([]corev1.EnvVar{}, configMapVars))
	if err != nil {
		return ctrl.Result{}, err
	}
	if hash != instance.Status.Hash {
		instance.Status.Hash = hash
		r.Log.Info(fmt.Sprintf("Input maps hash %s - %s", common.InputHashName, hash))
	}

	instance.Status.Conditions.MarkTrue(condition.InputReadyCondition, condition.InputReadyMessage)

	r.Log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

// getServiceDNSData -
func (r *DNSDataReconciler) getServiceDNSData(
	ctx context.Context,
	instance *networkv1.DNSData,
) (map[string][]net.IP, error) {
	hostList := map[string][]net.IP{}

	// get all services from the namespace triggered the reconcile
	svcList := &corev1.ServiceList{}

	/*
	   // but only ObjectMetadata as we don't care about spec and status
	   svcList := &metav1.PartialObjectMetadataList{}
	   svcList.SetGroupVersionKind(corev1.SchemeGroupVersion.WithKind("ServiceList"))
	*/
	if err := r.List(ctx, svcList, client.InNamespace(instance.Namespace)); err != nil {
		return hostList, err
	}

	for _, svc := range svcList.Items {
		if svc.Annotations != nil {
			// if the service has our networkv1.AnnotationHostnameKey get
			// the ips from its status if it is a LoadBalancer type
			if hostname, ok := svc.Annotations[networkv1.AnnotationHostnameKey]; ok && svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				if len(svc.Status.LoadBalancer.Ingress) > 0 {
					addresses := []net.IP{}
					//ptrRecords := []string{}
					for _, ingr := range svc.Status.LoadBalancer.Ingress {
						ip := net.ParseIP(ingr.IP)
						if ip == nil {
							return hostList, fmt.Errorf(fmt.Sprintf("unrecognized address %s", ingr.IP))
						}

						addresses = append(addresses, ip)
					}
					if len(addresses) > 0 {
						hostList[hostname] = addresses
					}
				}
			}
		}
		/*
			if svc.Name == req.Name {
				r.Log.Info(fmt.Sprintf("YEAH %s: %+v", req.Name, svc.ObjectMeta))
			} else {
				r.Log.Info(fmt.Sprintf("NOOO %s", req.Name))
			}
		*/
	}

	return hostList, nil
}

func reverseaddr(ip net.IP) string {
	if ip.To4() != nil {
		return strconv.FormatUint(uint64(ip[15]), 10) + "." +
			strconv.FormatUint(uint64(ip[14]), 10) + "." +
			strconv.FormatUint(uint64(ip[13]), 10) + "." +
			strconv.FormatUint(uint64(ip[12]), 10)
	}
	// Must be IPv6
	buf := make([]byte, 0, len(ip)*4)
	// Add it, in reverse, to the buffer
	for i := len(ip) - 1; i >= 0; i-- {
		v := ip[i]
		buf = append(buf, hexDigit[v&0xF],
			'.',
			hexDigit[v>>4],
			'.')
	}

	return string(buf)
}

// dnsmasqHostRecord -
func dnsmasqHostRecord(
	hostname string,
	ip net.IP,
) string {
	return fmt.Sprintf("host-record=%s,%s", hostname, ip.String())
}

// dnsmasqAddressEntry -
func dnsmasqAddressEntry(
	hostname string,
	ip net.IP,
) string {
	return fmt.Sprintf("address=/%s/%s", hostname, ip.String())
}

// dnsmasqPTRRecord -
func dnsmasqPTRRecord(
	hostname string,
	ip net.IP,
) string {
	return fmt.Sprintf("ptr-record=/%s.in-addr.arpa./%s", reverseaddr(ip), hostname)
}

// dnsmasqDHCPHost -
func dnsmasqDHCPHost(
	hostname string,
	ip net.IP,
) string {
	return fmt.Sprintf("dhcp-host=%s,%s,infinite", hostname, ip.String())
}

// MergeIPMaps - merge two or more maps
// NOTE: In case a key exists, the value in the first map is preserved.
func MergeIPMaps(baseMap map[string][]net.IP, extraMaps ...map[string][]net.IP) map[string][]net.IP {
	mergedMap := make(map[string][]net.IP)

	// Copy from the original map to the target map
	for key, value := range baseMap {
		mergedMap[key] = value
	}

	for _, extraMap := range extraMaps {
		for key, value := range extraMap {
			if _, ok := mergedMap[key]; !ok {
				mergedMap[key] = value
			}
		}
	}

	// Nil the result if the map is empty, thus avoiding triggering infinite reconcile
	// given that at json level label: {} or annotation: {} is different from no field, which is the
	// corresponding value stored in etcd given that those fields are defined as omitempty.
	if len(mergedMap) == 0 {
		return nil
	}
	return mergedMap
}
