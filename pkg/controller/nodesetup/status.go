// Copyright (c) 2023 ScyllaDB.

package nodesetup

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	scyllav1alpha1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1alpha1"
	apiequality "k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/klog/v2"
)

func (nsc *Controller) updateNodeStatus(ctx context.Context, currentNC *v1alpha1.NodeConfig, conditions []metav1.Condition) error {
	nc := currentNC.DeepCopy()

	statusConditions := nc.Status.Conditions.ToMetaV1Conditions()
	for _, cond := range conditions {
		_ = apimeta.SetStatusCondition(&statusConditions, cond)
	}
	nc.Status.Conditions = scyllav1alpha1.NewNodeConfigConditions(statusConditions)

	if apiequality.Semantic.DeepEqual(nc.Status, currentNC.Status) {
		return nil
	}

	klog.V(2).InfoS("Updating status", "NodeConfig", klog.KObj(currentNC), "Node", nsc.nodeName)

	_, err := nsc.scyllaClient.NodeConfigs().UpdateStatus(ctx, nc, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("can't update node config status %q: %w", nsc.nodeConfigName, err)
	}

	klog.V(2).InfoS("Status updated", "NodeConfig", klog.KObj(currentNC), "Node", nsc.nodeName)

	return nil
}
