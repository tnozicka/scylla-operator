package cluster

import (
	"context"
	"fmt"
	"regexp"
	"strconv"

	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	"github.com/scylladb/scylla-operator/pkg/controller/cluster/resource"
	"github.com/scylladb/scylla-operator/pkg/controller/helpers"
	"github.com/scylladb/scylla-operator/pkg/naming"
	"github.com/scylladb/scylla-operator/pkg/resourceapply"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1beta1 "k8s.io/api/policy/v1beta1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	utilerrors "k8s.io/apimachinery/pkg/util/errors"
	"k8s.io/klog/v2"
)

var serviceOrdinalRegex = regexp.MustCompile("^.*-([0-9]+)$")

func (scc *ScyllaClusterController) makeServices(sc *scyllav1.ScyllaCluster, oldServices map[string]*corev1.Service) []*corev1.Service {
	services := []*corev1.Service{
		resource.HeadlessServiceForCluster(sc),
	}

	for _, rack := range sc.Spec.Datacenter.Racks {
		stsName := naming.StatefulSetNameForRack(rack, sc)

		for ord := int32(0); ord < rack.Members; ord++ {
			svcName := fmt.Sprintf("%s-%d", stsName, ord)
			oldSvc := oldServices[svcName]
			services = append(services, resource.MemberService(sc, rack.Name, svcName, oldSvc))
		}
	}

	return services
}

func (scc *ScyllaClusterController) syncServices(
	ctx context.Context,
	sc *scyllav1.ScyllaCluster,
	status *scyllav1.ScyllaClusterStatus,
	services map[string]*corev1.Service,
	statefulSets map[string]*appsv1.StatefulSet,
) (*scyllav1.ScyllaClusterStatus, error) {
	var err error

	requiredServices := scc.makeServices(sc, services)

	// Delete any excessive Services.
	// Delete has to be the fist action to avoid getting stuck on quota.
	var deletionErrors []error
	for _, svc := range services {
		if svc.DeletionTimestamp != nil {
			continue
		}

		isRequired := false
		for _, req := range requiredServices {
			if svc.Name == req.Name {
				isRequired = true
			}
		}
		if isRequired {
			continue
		}

		// Do not delete services for scale down.
		rackName, ok := svc.Labels[naming.RackNameLabel]
		if !ok {
			return status, fmt.Errorf("service %s/%s is missing %q label", svc.Namespace, svc.Name, naming.RackNameLabel)
		}
		stsName := fmt.Sprintf("%s-%s-%s", sc.Name, sc.Spec.Datacenter.Name, rackName)
		sts, ok := statefulSets[stsName]
		if !ok {
			return status, fmt.Errorf("statefulset %s/%s is missing", sts.Namespace, sts.Name)
		}
		// TODO: Label services with the ordinal instead of parsing.
		// FIXME: Move it to a function and unit test it.
		svcOrdinalStrings := serviceOrdinalRegex.FindStringSubmatch(svc.Name)
		if len(svcOrdinalStrings) != 2 {
			return status, fmt.Errorf("can't parse ordinal from service %s/%s", svc.Namespace, svc.Name)
		}
		svcOrdinal, err := strconv.Atoi(svcOrdinalStrings[1])
		if err != nil {
			return status, err
		}
		if int32(svcOrdinal) < *sts.Spec.Replicas {
			continue
		}

		// Do not delete services that weren't properly decommissioned.
		if svc.Labels[naming.DecommissionedLabel] != naming.LabelValueTrue {
			klog.Warningf("Refusing to cleanup service %s/%s whose member wasn't decommissioned.", svc.Namespace, svc.Name)
			continue
		}

		propagationPolicy := metav1.DeletePropagationBackground
		err = scc.kubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, svc.Name, metav1.DeleteOptions{
			Preconditions: &metav1.Preconditions{
				UID: &svc.UID,
			},
			PropagationPolicy: &propagationPolicy,
		})
		deletionErrors = append(deletionErrors, err)
	}
	err = utilerrors.NewAggregate(deletionErrors)
	if err != nil {
		return status, fmt.Errorf("can't delete Service(s): %w", err)
	}

	// We need to first propagate ReplaceAddressFirstBoot from status for a new service.
	for _, svc := range requiredServices {
		_, _, err = resourceapply.ApplyService(ctx, scc.kubeClient.CoreV1(), scc.serviceLister, scc.eventRecorder, svc)
		if err != nil {
			return status, err
		}
	}

	// Replace members.
	for _, svc := range services {
		replaceAddr, ok := svc.Labels[naming.ReplaceLabel]
		if !ok {
			continue
		}

		_, isSeed := svc.Labels[naming.SeedLabel]
		if isSeed {
			klog.ErrorS(nil, "Seed node replace is not supported",
				"ScyllaCluster", klog.KObj(sc),
				"Service", klog.KObj(svc),
			)
			continue
		}

		rackName, ok := svc.Labels[naming.RackNameLabel]
		if !ok {
			return status, fmt.Errorf("service %s/%s is missing %q label", svc.Namespace, svc.Name, naming.RackNameLabel)
		}

		if len(replaceAddr) == 0 {
			// Member needs to be replaced.

			// TODO: Can't we just use the same service and keep it?
			//       The pod has identity so there will never be 2 at the same time.

			// Save replace address in RackStatus
			if len(status.Racks[rackName].ReplaceAddressFirstBoot[svc.Name]) == 0 {
				status.Racks[rackName].ReplaceAddressFirstBoot[svc.Name] = svc.Spec.ClusterIP
				klog.V(2).InfoS("Adding member address to replace address list", "Member", svc.Name, "IP", svc.Spec.ClusterIP, "ReplaceAddresses", status.Racks[rackName].ReplaceAddressFirstBoot)
				rackStatus := status.Racks[rackName]
				scyllav1.SetRackCondition(&rackStatus, scyllav1.RackConditionTypeMemberReplacing)
				status.Racks[rackName] = rackStatus

				// Make sure the address is stored before proceeding further.
				return status, nil
			}

			backgroundPropagationPolicy := metav1.DeletePropagationBackground

			// First, Delete the PVC if it exists.
			// The PVC has finalizer protection so it will wait for the pod to be deleted.
			// We can't do it the other way around or the pod could be recreated before we delete the PVC.
			pvcName := naming.PVCNameForPod(svc.Name)
			klog.V(2).InfoS("Deleting the PVC to replace member",
				"ScyllaCluster", klog.KObj(sc),
				"Service", klog.KObj(svc),
				"PVCName", pvcName,
			)
			err = scc.kubeClient.CoreV1().PersistentVolumeClaims(svc.Namespace).Delete(ctx, pvcName, metav1.DeleteOptions{
				PropagationPolicy: &backgroundPropagationPolicy,
			})
			if err != nil && !apierrors.IsNotFound(err) {
				return status, err
			}

			// Evict the Pod if it exists.
			klog.V(2).InfoS("Evicting the Pod to replace member",
				"ScyllaCluster", klog.KObj(sc),
				"Service", klog.KObj(svc),
				"PodName", svc.Name,
			)
			err = scc.kubeClient.CoreV1().Pods(svc.Namespace).Evict(ctx, &policyv1beta1.Eviction{
				ObjectMeta: metav1.ObjectMeta{
					Name: svc.Name,
				},
				DeleteOptions: &metav1.DeleteOptions{
					PropagationPolicy: &backgroundPropagationPolicy,
				},
			})
			if err != nil && !apierrors.IsNotFound(err) {
				return status, err
			}

			// Delete the member Service.
			klog.V(2).InfoS("Deleting the member service to replace member",
				"ScyllaCluster", klog.KObj(sc),
				"Service", klog.KObj(svc),
			)
			err = scc.kubeClient.CoreV1().Services(svc.Namespace).Delete(ctx, pvcName, metav1.DeleteOptions{
				PropagationPolicy: &backgroundPropagationPolicy,
				Preconditions: &metav1.Preconditions{
					UID: &svc.UID,
					// Delete only if its the version we see in cache. (Has the same replace label state.)
					ResourceVersion: &svc.ResourceVersion,
				},
			})
			if err != nil && !apierrors.IsNotFound(err) {
				return status, err
			} else {
				// FIXME: The pod could have been already up and read the old ClusterIP - make sure it will restart.
				//        We can't delete the pod here as it wouldn't retry failures.

				// We have deleted the service. Wait for re-queue.
				return status, nil
			}
		} else {
			// Member is being replaced. Wait for readiness and clear the replace label.

			pod, err := scc.podLister.Pods(svc.Namespace).Get(svc.Name)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					return status, err
				}

				klog.V(2).InfoS("Pod has not been recreated by the StatefulSet controller yet",
					"ScyllaCluster", klog.KObj(sc),
					"Pod", klog.KObj(pod),
				)
			} else {
				sc := scc.resolveScyllaClusterControllerThroughStatefulSet(pod)
				if sc == nil {
					klog.ErrorS(nil, "Pod matching our selector is not owned by us.",
						"ScyllaCluster", klog.KObj(sc),
						"Pod", klog.KObj(pod),
					)
					// User needs to fix the collision manually so there
					// is no point in returning an error and retrying.
					return status, nil
				}

				// We could still see an old pod in the caches - verify with a live call.
				podReady, pod, err := helpers.IsPodReadyWithPositiveLiveCheck(ctx, scc.kubeClient.CoreV1(), pod)
				if err != nil {
					return status, err
				}
				if podReady {
					svcCopy := svc.DeepCopy()
					delete(svcCopy.Labels, naming.ReplaceLabel)
					_, err := scc.kubeClient.CoreV1().Services(svcCopy.Namespace).Update(ctx, svcCopy, metav1.UpdateOptions{})
					if err != nil {
						return status, err
					}

					delete(status.Racks[rackName].ReplaceAddressFirstBoot, svc.Name)
				} else {
					klog.V(2).InfoS("Pod isn't ready yet",
						"ScyllaCluster", klog.KObj(sc),
						"Pod", klog.KObj(pod),
					)
				}
			}
		}
	}

	return status, nil
}
