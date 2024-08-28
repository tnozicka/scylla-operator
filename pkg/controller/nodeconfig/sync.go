// Copyright (C) 2021 ScyllaDB

package nodeconfig

import (
	"context"
	"fmt"
	"time"

	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/controllerhelpers"
	"github.com/scylladb/scylla-operator/pkg/naming"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/client-go/tools/cache"
	"k8s.io/klog/v2"
)

func (ncc *Controller) sync(ctx context.Context, key string) error {
	namespace, name, err := cache.SplitMetaNamespaceKey(key)
	if err != nil {
		return fmt.Errorf("can't split meta namespace cache key: %w", err)
	}

	startTime := time.Now()
	klog.V(4).InfoS("Started syncing NodeConfig", "NodeConfig", klog.KRef(namespace, name), "startTime", startTime)
	defer func() {
		klog.V(4).InfoS("Finished syncing NodeConfig", "NodeConfig", klog.KRef(namespace, name), "duration", time.Since(startTime))
	}()

	nc, err := ncc.nodeConfigLister.Get(name)
	if apierrors.IsNotFound(err) {
		klog.V(2).InfoS("NodeConfig has been deleted", "NodeConfig", klog.KObj(nc))
		return nil
	}
	if err != nil {
		return fmt.Errorf("can't get NodeConfig %q: %w", key, err)
	}

	soc, err := ncc.scyllaOperatorConfigLister.Get(naming.SingletonName)
	if err != nil {
		return fmt.Errorf("can't get ScyllaOperatorConfig: %w", err)
	}

	ncSelector := labels.SelectorFromSet(labels.Set{
		naming.NodeConfigNameLabel: nc.Name,
	})

	type CT = *scyllav1alpha1.NodeConfig
	var objectErrs []error

	namespaces, err := ncc.getNamespaces()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	clusterRoles, err := ncc.getClusterRoles()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	roles, err := ncc.getRoles()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	serviceAccounts, err := ncc.getServiceAccounts()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	clusterRoleBindings, err := ncc.getClusterRoleBindings()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	roleBindings, err := ncc.getRoleBindings()
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	daemonSets, err := controllerhelpers.GetObjects[CT, *appsv1.DaemonSet](
		ctx,
		nc,
		nodeConfigControllerGVK,
		ncSelector,
		controllerhelpers.ControlleeManagerGetObjectsFuncs[CT, *appsv1.DaemonSet]{
			GetControllerUncachedFunc: ncc.scyllaClient.NodeConfigs().Get,
			ListObjectsFunc:           ncc.daemonSetLister.DaemonSets(naming.ScyllaOperatorNodeTuningNamespace).List,
			PatchObjectFunc:           ncc.kubeClient.AppsV1().DaemonSets(naming.ScyllaOperatorNodeTuningNamespace).Patch,
		},
	)
	if err != nil {
		objectErrs = append(objectErrs, err)
	}

	objectErr := utilerrors.NewAggregate(objectErrs)
	if objectErr != nil {
		return objectErr
	}

	status, err := ncc.calculateStatus(nc)
	if err != nil {
		return fmt.Errorf("can't calculate status: %w", err)
	}
	statusConditions := status.Conditions.ToMetaV1Conditions()

	if nc.DeletionTimestamp != nil {
		return ncc.updateStatus(ctx, nc, status, statusConditions)
	}

	var errs []error

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncNamespaces(
				ctx,
				namespaces,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync Namespace(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncClusterRoles(
				ctx,
				clusterRoles,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync ClusterRole(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncRoles(
				ctx,
				nc,
				roles,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync Role(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncServiceAccounts(
				ctx,
				serviceAccounts,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync ServiceAccount(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncClusterRoleBindings(
				ctx,
				clusterRoleBindings,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync ClusterRoleBinding(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncRoleBindings(
				ctx,
				nc,
				roleBindings,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync RoleBinding(s): %w", err))
	}

	err = controllerhelpers.RunSync(
		&statusConditions,
		nodeConfigControllerProgressingCondition,
		nodeConfigControllerDegradedCondition,
		nc.Generation,
		func() ([]metav1.Condition, error) {
			return ncc.syncDaemonSet(
				ctx,
				nc,
				soc,
				daemonSets,
				status,
				&statusConditions,
			)
		},
	)
	if err != nil {
		errs = append(errs, fmt.Errorf("sync DaemonSet(s): %w", err))
	}

	// Aggregate conditions.
	err = controllerhelpers.SetAggregatedWorkloadConditions(&statusConditions, nc.Generation)
	if err != nil {
		errs = append(errs, fmt.Errorf("can't aggregate workload conditions: %w", err))
	} else {
		// Remove unused Available condition, for now.
		apimeta.RemoveStatusCondition(&statusConditions, scyllav1alpha1.AvailableCondition)

		err = ncc.updateStatus(ctx, nc, status, statusConditions)
		errs = append(errs, err)
	}

	return utilerrors.NewAggregate(errs)
}

func (ncc *Controller) getNamespaces() (map[string]*corev1.Namespace, error) {
	nss, err := ncc.namespaceLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list namespaces: %w", err)
	}

	nsMap := map[string]*corev1.Namespace{}
	for i := range nss {
		nsMap[nss[i].Name] = nss[i]
	}

	return nsMap, nil
}

func (ncc *Controller) getClusterRoles() (map[string]*rbacv1.ClusterRole, error) {
	crs, err := ncc.clusterRoleLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list clusterroles: %w", err)
	}

	crMap := map[string]*rbacv1.ClusterRole{}
	for i := range crs {
		crMap[crs[i].Name] = crs[i]
	}

	return crMap, nil
}

func (ncc *Controller) getClusterRoleBindings() (map[string]*rbacv1.ClusterRoleBinding, error) {
	crbs, err := ncc.clusterRoleBindingLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list clusterrolebindings: %w", err)
	}

	crbMap := map[string]*rbacv1.ClusterRoleBinding{}
	for i := range crbs {
		crbMap[crbs[i].Name] = crbs[i]
	}

	return crbMap, nil
}

func (ncc *Controller) getRoles() (map[string]*rbacv1.Role, error) {
	roles, err := ncc.roleLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list roles: %w", err)
	}

	res := map[string]*rbacv1.Role{}
	for i := range roles {
		res[roles[i].Name] = roles[i]
	}

	return res, nil
}

func (ncc *Controller) getRoleBindings() (map[string]*rbacv1.RoleBinding, error) {
	roleBindings, err := ncc.roleBindingLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list rolebindings: %w", err)
	}

	res := map[string]*rbacv1.RoleBinding{}
	for i := range roleBindings {
		res[roleBindings[i].Name] = roleBindings[i]
	}

	return res, nil
}

func (ncc *Controller) getServiceAccounts() (map[string]*corev1.ServiceAccount, error) {
	sas, err := ncc.serviceAccountLister.List(labels.SelectorFromSet(map[string]string{
		naming.NodeConfigNameLabel: naming.NodeConfigAppName,
	}))
	if err != nil {
		return nil, fmt.Errorf("list serviceaccounts: %w", err)
	}

	sasMap := map[string]*corev1.ServiceAccount{}
	for i := range sas {
		sasMap[sas[i].Name] = sas[i]
	}

	return sasMap, nil
}
