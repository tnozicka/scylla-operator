// Copyright (C) 2021 ScyllaDB

package nodeconfig

import (
	"context"
	"fmt"

	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	"github.com/scylladb/scylla-operator/pkg/controllerhelpers"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/pointer"
	"github.com/scylladb/scylla-operator/pkg/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func (ncc *Controller) syncDaemonSet(
	ctx context.Context,
	nc *scyllav1alpha1.NodeConfig,
	soc *scyllav1alpha1.ScyllaOperatorConfig,
	daemonSets map[string]*appsv1.DaemonSet,
	status *scyllav1alpha1.NodeConfigStatus,
	statusConditions *[]metav1.Condition,
) ([]metav1.Condition, error) {
	var progressingConditions []metav1.Condition

	scyllaUtilsImage := soc.Spec.ScyllaUtilsImage
	// FIXME: check that its not empty, emit event
	// FIXME: add webhook validation for the format
	requiredDaemonSets := []*appsv1.DaemonSet{
		makeNodeSetupDaemonSet(nc, ncc.operatorImage, scyllaUtilsImage),
	}

	err := controllerhelpers.Prune(
		ctx,
		requiredDaemonSets,
		daemonSets,
		&controllerhelpers.PruneControlFuncs{
			DeleteFunc: ncc.kubeClient.AppsV1().DaemonSets(naming.ScyllaOperatorNodeTuningNamespace).Delete,
		},
		ncc.eventRecorder)
	if err != nil {
		return progressingConditions, fmt.Errorf("can't prune DaemonSet(s): %w", err)
	}

	desiredSum := int64(0)
	allReconciled := true
	for _, requiredDaemonSet := range requiredDaemonSets {
		if requiredDaemonSet == nil {
			continue
		}

		ds, _, err := resourceapply.ApplyDaemonSet(ctx, ncc.kubeClient.AppsV1(), ncc.daemonSetLister, ncc.eventRecorder, requiredDaemonSet, resourceapply.ApplyOptions{})
		if err != nil {
			return progressingConditions, fmt.Errorf("can't apply daemonset: %w", err)
		}

		desiredSum += int64(ds.Status.DesiredNumberScheduled)

		reconciled, err := controllerhelpers.IsDaemonSetRolledOut(ds)
		if err != nil {
			return nil, fmt.Errorf("can't determine is a daemonset %q is reconiled: %w", naming.ObjRef(ds), err)
		}
		if !reconciled {
			allReconciled = false
		}
	}

	status.DesiredNodeSetupCount = pointer.Ptr(desiredSum)

	reconciledCondition := metav1.Condition{
		Type:               string(scyllav1alpha1.NodeConfigReconciledConditionType),
		ObservedGeneration: nc.Generation,
		Status:             metav1.ConditionUnknown,
	}

	if allReconciled {
		reconciledCondition.Status = metav1.ConditionTrue
		reconciledCondition.Reason = "FullyReconciledAndUp"
		reconciledCondition.Message = "All operands are reconciled and available."
	} else {
		reconciledCondition.Status = metav1.ConditionFalse
		reconciledCondition.Reason = "DaemonSetNotRolledOut"
		reconciledCondition.Message = "DaemonSet isn't reconciled and fully rolled out yet."
	}
	_ = apimeta.SetStatusCondition(statusConditions, reconciledCondition)

	return progressingConditions, nil
}
