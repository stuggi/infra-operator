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

	k8s_errors "k8s.io/apimachinery/pkg/api/errors"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"

	corev1 "k8s.io/api/core/v1"

	"github.com/go-logr/logr"
	networkv1 "github.com/openstack-k8s-operators/infra-operator/apis/network/v1beta1"
	ipam "github.com/openstack-k8s-operators/infra-operator/pkg/ipam"
	condition "github.com/openstack-k8s-operators/lib-common/modules/common/condition"
	helper "github.com/openstack-k8s-operators/lib-common/modules/common/helper"
	"github.com/openstack-k8s-operators/lib-common/modules/common/util"
)

// NetConfigReconciler reconciles a NetConfig object
type NetConfigReconciler struct {
	client.Client
	Kclient kubernetes.Interface
	Log     logr.Logger
	Scheme  *runtime.Scheme
}

//+kubebuilder:rbac:groups=network.openstack.org,resources=netconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=network.openstack.org,resources=netconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=network.openstack.org,resources=netconfigs/finalizers,verbs=update
//+kubebuilder:rbac:groups=network.openstack.org,resources=ipsets,verbs=get;list;watch;patch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.13.0/pkg/reconcile
func (r *NetConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, _err error) {
	_ = log.FromContext(ctx)

	// Fetch the NetConfig instance
	instance := &networkv1.NetConfig{}
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
		// update the Ready condition based on the sub conditions
		if instance.Status.Conditions.AllSubConditionIsTrue() {
			instance.Status.Conditions.MarkTrue(
				condition.ReadyCondition, condition.ReadyMessage)
		} else {
			// something is not ready so reset the Ready condition
			instance.Status.Conditions.MarkUnknown(
				condition.ReadyCondition, condition.InitReason, condition.ReadyInitMessage)
			// and recalculate it based on the state of the rest of the conditions
			instance.Status.Conditions.Set(
				instance.Status.Conditions.Mirror(condition.ReadyCondition))
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
func (r *NetConfigReconciler) SetupWithManager(mgr ctrl.Manager) error {
	// watch for objects in the same namespace as the NetConfig CR
	ipsetWatcher := handler.EnqueueRequestsFromMapFunc(func(obj client.Object) []reconcile.Request {
		result := []reconcile.Request{}

		// get all CRs from the same namespace
		crs := &networkv1.NetConfigList{}
		listOpts := []client.ListOption{
			client.InNamespace(obj.GetNamespace()),
		}
		if err := r.List(context.Background(), crs, listOpts...); err != nil {
			r.Log.Error(err, "Unable to retrieve CRs %v")
			return nil
		}

		for _, cr := range crs.Items {
			if obj.GetNamespace() == cr.Namespace {
				// return namespace and Name of CR
				name := client.ObjectKey{
					Namespace: cr.Namespace,
					Name:      cr.Name,
				}
				result = append(result, reconcile.Request{NamespacedName: name})
			}
		}

		return result
	})

	return ctrl.NewControllerManagedBy(mgr).
		For(&networkv1.NetConfig{}).
		Owns(&networkv1.Reservation{}).
		// watch IPSet in the same namespace
		Watches(&source.Kind{Type: &networkv1.IPSet{}}, ipsetWatcher).
		Complete(r)
}

func (r *NetConfigReconciler) reconcileDelete(ctx context.Context, instance *networkv1.NetConfig, helper *helper.Helper) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service delete")

	// Service is deleted so remove the finalizer.
	controllerutil.RemoveFinalizer(instance, helper.GetFinalizer())
	r.Log.Info("Reconciled Service delete successfully")

	return ctrl.Result{}, nil
}

// NetConfigReconciler reconciles a NetConfig object
func (r *NetConfigReconciler) reconcileNormal(
	ctx context.Context,
	instance *networkv1.NetConfig,
	helper *helper.Helper,
) (ctrl.Result, error) {
	r.Log.Info("Reconciling Service")

	r.Log.Info("get all Reservations")
	// get list of Reservation objects in the namespace
	reservations := &networkv1.ReservationList{}
	opts := &client.ListOptions{
		Namespace: instance.Namespace,
	}
	err := r.List(ctx, reservations, opts)
	if err != nil {
		return ctrl.Result{}, err
	}

	// reservations map map[string]Reservation with reservation name as index
	resMapNameIdx := make(map[string]networkv1.Reservation)
	// map with all ip -> hostname references
	resMapIPIdx := make(map[string]string)
	for _, res := range reservations.Items {
		resMapNameIdx[res.Name] = res
		for _, netRes := range res.Spec.Reservations {
			resMapIPIdx[netRes.Address] = string(*res.Spec.Hostname)
		}
	}

	r.Log.Info("get all IPSets")
	// get list of IPSets objects in the namespace
	ipSets := &networkv1.IPSetList{}
	err = r.List(ctx, ipSets, opts)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.Log.Info("check IPSets against Reservations")

	// Iterate over the IPSets to find all addresses and objects
	for _, ipset := range ipSets.Items {
		if instance.DeletionTimestamp.IsZero() {
			err := r.ensureReservation(ctx, instance, &ipset, helper, resMapIPIdx)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to create new reservation for %s - %w", ipset.Name, err)
			}

		} else {
			// TODO delete reservation
		}
	}

	instance.Status.Conditions.MarkTrue(condition.ServiceConfigReadyCondition, condition.InputReadyMessage)

	r.Log.Info("Reconciled Service successfully")
	return ctrl.Result{}, nil
}

func (r *NetConfigReconciler) ensureReservation(
	ctx context.Context,
	instance *networkv1.NetConfig,
	ipset *networkv1.IPSet,
	helper *helper.Helper,
	reservations map[string]string,
) (_err error) {

	reservationName := types.NamespacedName{
		Namespace: ipset.Namespace,
		Name:      ipset.Name,
	}
	reservationSpec := networkv1.ReservationSpec{
		Hostname:     ipset.Spec.Hostname,
		Reservations: map[string]networkv1.IPAddress{},
	}
	reservationLabels := map[string]string{}

	// always patch the Reservation
	defer func() {
		err := r.patchReservation(
			ctx,
			helper,
			ipset,
			reservationName,
			reservationLabels,
			reservationSpec,
		)
		if err != nil {
			_err = fmt.Errorf("failed to patch reservation %w", err)
			return
		}
	}()

	// add IPset owner reference to the reservation spec
	reservationSpec.IPSetRef = corev1.ObjectReference{
		Namespace: ipset.Namespace,
		Name:      ipset.Name,
	}

	// create IPs per requested Network and Subnet
	for _, ipsetNet := range ipset.Spec.Networks {
		netDef, subnetDef, err := instance.GetNetAndSubnet(ipsetNet.Name, ipsetNet.SubnetName)
		if err != nil {
			return err
		}

		// set net: subnet label
		reservationLabels = map[string]string{
			fmt.Sprintf("%s/%s", ipam.IPAMLabelKey, string(netDef.Name)): string(subnetDef.Name),
		}

		_, subnet, err := net.ParseCIDR(string(subnetDef.Cidr))
		if err != nil {
			return fmt.Errorf("failed to parse subnet cidr %w", err)
		}

		ipDetails := ipam.AssignIPDetails{
			Hostname:    string(*ipset.Spec.Hostname),
			IPNet:       *subnet,
			Ranges:      subnetDef.AllocationRanges,
			Reservelist: reservations,
		}
		if ipsetNet.FixedIP != nil {
			ipDetails.FixedIP = net.ParseIP(string(*ipsetNet.FixedIP))
		}

		ip, err := ipDetails.AssignIP(helper)
		if err != nil {
			return fmt.Errorf("Failed to do ip reservation: %w", err)
		}

		ip.Subnet = subnetDef.Name
		ip.Network = netDef.Name
		ip.Gateway = subnetDef.Gateway
		ip.Prefix, _ = subnet.Mask.Size()
		ip.Routes = subnetDef.Routes

		// add IP to the reservation
		r.Log.Info(fmt.Sprintf("Reservation for %s %s - %+v", string(*ipset.Spec.Hostname), string(ipsetNet.Name), ip))

		reservationSpec.Reservations[string(ipsetNet.Name)] = ip
		reservations[ip.Address] = string(*ipset.Spec.Hostname)
	}

	return nil
}

func (r *NetConfigReconciler) patchReservation(
	ctx context.Context,
	helper *helper.Helper,
	ipset *networkv1.IPSet,
	name types.NamespacedName,
	labels map[string]string,
	spec networkv1.ReservationSpec,
) error {
	res := &networkv1.Reservation{
		ObjectMeta: v1.ObjectMeta{
			Name:      name.Name,
			Namespace: name.Namespace,
		},
	}

	// create or update the Reservation
	op, err := controllerutil.CreateOrPatch(ctx, r.Client, res, func() error {
		res.Labels = util.MergeStringMaps(res.Labels, labels)
		res.Spec = spec

		// Set controller reference to the NetConfig object
		err := controllerutil.SetControllerReference(helper.GetBeforeObject(), res, r.Scheme)
		if err != nil {
			return err
		}

		// Add owner reference for the IPSet object
		err = controllerutil.SetOwnerReference(ipset, res, r.Scheme)
		if err != nil {
			return err
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("error create/updating Reservation: %w", err)
	}

	if op != controllerutil.OperationResultNone {
		r.Log.Info(fmt.Sprintf("%s reservation - operation: %s", res.Name, string(op)))
	}

	return nil
}
