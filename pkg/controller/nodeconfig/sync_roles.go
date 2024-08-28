// Copyright (C) 2024 ScyllaDB

package nodeconfig

import (
	"context"
	"fmt"

	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/controllerhelpers"
	"github.com/scylladb/scylla-operator/pkg/resourceapply"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
)

func (ncc *Controller) syncRoles(ctx context.Context, nc *scyllav1alpha1.NodeConfig, roles map[string]*rbacv1.Role) ([]metav1.Condition, error) {
	var progressingConditions []metav1.Condition

	requiredRoles := []*rbacv1.Role{
		makePerftuneRole(),
	}

	// Delete any excessive Roles.
	// Delete has to be the first action to avoid getting stuck on quota.
	err := controllerhelpers.Prune(
		ctx,
		requiredRoles,
		roles,
		&controllerhelpers.PruneControlFuncs{
			DeleteFunc: ncc.kubeClient.RbacV1().Roles(nc.Namespace).Delete,
		},
		ncc.eventRecorder)
	if err != nil {
		return progressingConditions, fmt.Errorf("can't prune Role(s): %w", err)
	}

	var errs []error
	for _, cr := range requiredRoles {
		_, _, err := resourceapply.ApplyRole(ctx, ncc.kubeClient.RbacV1(), ncc.roleLister, ncc.eventRecorder, cr, resourceapply.ApplyOptions{
			AllowMissingControllerRef: true,
		})
		if err != nil {
			errs = append(errs, fmt.Errorf("can't create missing Role: %w", err))
			continue
		}
	}

	return progressingConditions, utilerrors.NewAggregate(errs)
}
