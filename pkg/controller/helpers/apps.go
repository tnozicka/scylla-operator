package helpers

import (
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/klog/v2"
)

func IsStatefulSetRolledOut(sts *appsv1.StatefulSet) bool {
	if sts.Spec.Replicas == nil {
		// This should never happen, but better safe then sorry.
		klog.ErrorS(nil, "StatefulSet.spec.replicas is nil!")
		return false
	}

	return sts.Status.ObservedGeneration >= sts.Generation &&
		sts.Status.CurrentRevision == sts.Status.UpdateRevision &&
		sts.Status.ReadyReplicas == *sts.Spec.Replicas
}
