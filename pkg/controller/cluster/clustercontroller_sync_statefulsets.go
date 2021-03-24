package cluster

import (
	"context"
	"fmt"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/controller/cluster/resource"
	"github.com/scylladb/scylla-operator/pkg/controller/helpers"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv1 "k8s.io/api/autoscaling/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

func (scc *ScyllaClusterController) makeRacks(sc *scyllav1.ScyllaCluster) []*appsv1.StatefulSet {
	sets := make([]*appsv1.StatefulSet, 0, len(sc.Spec.Datacenter.Racks))
	for _, rack := range sc.Spec.Datacenter.Racks {
		sts := resource.StatefulSetForRack(rack, sc, scc.operatorImage)
		sets = append(sets, sts)
	}
	return sets
}

func (scc *ScyllaClusterController) syncStatefulSets(
	ctx context.Context,
	sc *scyllav1.ScyllaCluster,
	status *scyllav1.ScyllaClusterStatus,
	statefulSets map[string]*appsv1.StatefulSet,
	services map[string]*corev1.Service,
) (*scyllav1.ScyllaClusterStatus, error) {
	var err error

	requiredStatefulSets := scc.makeRacks(sc)

	// Delete any excessive StatefulSets.
	// Delete has to be the fist action to avoid getting stuck on quota.
	var deletionErrors []error
	for _, sts := range statefulSets {
		if sts.DeletionTimestamp != nil {
			continue
		}

		isRequired := false
		for _, req := range requiredStatefulSets {
			if sts.Name == req.Name {
				isRequired = true
			}
		}
		if isRequired {
			continue
		}

		propagationPolicy := metav1.DeletePropagationBackground
		err = scc.kubeClient.AppsV1().StatefulSets(sts.Namespace).Delete(ctx, sts.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID: &sts.UID,
			},
			PropagationPolicy: &propagationPolicy,
		})
		deletionErrors = append(deletionErrors, err)
	}
	err = utilerrors.NewAggregate(deletionErrors)
	if err != nil {
		return status, fmt.Errorf("can't delete StatefulSet(s): %w", err)
	}

	// Before any update, make sure all StatefulSets are present.
	// Create any that are missing.
	var stsCreationErrors []error
	stsCreated := false
	for _, sts := range requiredStatefulSets {
		// Check the adopted set
		_, found := statefulSets[sts.Name]
		if !found {
			klog.V(2).InfoS("Creating missing StatefulSet", "StatefulSet", klog.KObj(sts))
			_, changed, err := resourceapply.ApplyStatefulSet(ctx, scc.kubeClient.AppsV1(), scc.statefulSetLister, scc.eventRecorder, sts)
			if err != nil {
				stsCreationErrors = append(stsCreationErrors, err)
				continue
			}
			if changed {
				stsCreated = true
			}
		}
	}
	err = utilerrors.NewAggregate(stsCreationErrors)
	if err != nil {
		return status, fmt.Errorf("can't create StatefulSet(s): %w", err)
	}
	if stsCreated {
		// Wait for the informers to catch up.
		// TODO: Protect premature requeues with expectations.
		return status, nil
	}

	// Wait for all racks to be up and ready.
	for _, req := range requiredStatefulSets {
		sts := statefulSets[req.Name]
		if !helpers.IsStatefulSetRolledOut(sts) {
			klog.V(4).InfoS("Waiting for StatefulSet rollout", "ScyllaCluster", klog.KObj(sc), "StatefulSet", klog.KObj(sts))
			return status, nil
		}
	}

	// Scale before the update.
	for _, req := range requiredStatefulSets {
		sts := statefulSets[req.Name]
		rackServices := map[string]*corev1.Service{}
		for _, svc := range services {
			svcRackName, ok := svc.Labels[naming.RackNameLabel]
			if !ok || svcRackName != sts.Labels[naming.RackNameLabel] {
				rackServices[svc.Name] = svc
			}
		}

		// Wait if any decommissioning is in progress.
		for _, svc := range rackServices {
			if svc.Labels[naming.DecommissionedLabel] == naming.LabelValueFalse {
				klog.V(4).InfoS("Waiting for service to be decommissioned")
				// FIXME: set decommissioning condition
				return status, nil
			}
		}

		scale := &autoscalingv1.Scale{
			ObjectMeta: metav1.ObjectMeta{
				Name:            sts.Name,
				Namespace:       sts.Namespace,
				ResourceVersion: sts.ResourceVersion,
			},
			Spec: autoscalingv1.ScaleSpec{
				Replicas: *req.Spec.Replicas,
			},
		}

		if scale.Spec.Replicas == *sts.Spec.Replicas {
			continue
		}

		if scale.Spec.Replicas < *sts.Spec.Replicas {
			// Make sure we always scale down by 1 member.
			scale.Spec.Replicas = *sts.Spec.Replicas - 1

			lastSvcName := fmt.Sprintf("%s-%d", sts.Name, *sts.Spec.Replicas-1)
			lastSvc, ok := rackServices[lastSvcName]
			if !ok {
				klog.V(4).InfoS("Missing service", "ScyllaCluster", klog.KObj(sc), "ServiceName", lastSvcName)
				// Services are managed in the other loop.
				// When informers see the new service, will get re-queued.
				return status, nil
			}

			if len(lastSvc.Labels[naming.DecommissionedLabel]) == 0 {
				lastSvcCopy := lastSvc.DeepCopy()
				// Record the intent to decommission the member.
				lastSvcCopy.Labels[naming.DecommissionedLabel] = naming.LabelValueFalse
				_, err := scc.kubeClient.CoreV1().Services(lastSvcCopy.Namespace).Update(ctx, lastSvcCopy, metav1.UpdateOptions{})
				if err != nil {
					return status, err
				}
				return status, nil
			}
		}

		klog.V(2).InfoS("Scaling StatefulSet", "ScyllaCluster", klog.KObj(sc), "StatefulSet", klog.KObj(sts), "CurrentReplicas", *sts.Spec.Replicas, "UpdatedReplicas", scale.Spec.Replicas)
		_, err = scc.kubeClient.AppsV1().StatefulSets(sts.Namespace).UpdateScale(ctx, sts.Name, scale, metav1.UpdateOptions{})
		if err != nil {
			return status, fmt.Errorf("can't update scale: %w", err)
		}
		return status, err
	}

	// Begin update.
	for _, req := range requiredStatefulSets {
		// TODO: Check transitions that need more then apply.

		sts, _, err := resourceapply.ApplyStatefulSet(ctx, scc.kubeClient.AppsV1(), scc.statefulSetLister, scc.eventRecorder, req)
		if err != nil {
			return status, err
		}

		// Wait for the StatefulSet to rollout.
		if !helpers.IsStatefulSetRolledOut(sts) {
			return status, nil
		}
	}

	return status, nil
}
