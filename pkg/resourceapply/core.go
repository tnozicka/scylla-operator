package resourceapply

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appv1client "k8s.io/client-go/kubernetes/typed/core/v1"
	appv1listers "k8s.io/client-go/listers/core/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

func ApplyService(
	ctx context.Context,
	client appv1client.ServicesGetter,
	lister appv1listers.ServiceLister,
	recorder record.EventRecorder,
	required *corev1.Service,
) (*corev1.Service, bool, error) {
	requiredControllerRef := metav1.GetControllerOfNoCopy(required)
	if requiredControllerRef == nil {
		return nil, false, fmt.Errorf("service %q is missing controllerRef", objectReference(required))
	}

	requiredCopy := required.DeepCopy()
	err := SetHashAnnotation(requiredCopy)
	if err != nil {
		return nil, false, err
	}

	existing, err := lister.Services(requiredCopy.Namespace).Get(requiredCopy.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, false, err
		}

		actual, err := client.Services(requiredCopy.Namespace).Create(ctx, requiredCopy, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			klog.V(2).InfoS("Already exists (stale cache)", "Service", klog.KObj(requiredCopy))
		} else {
			reportCreateEvent(recorder, requiredCopy, err)
		}
		return actual, true, err
	}

	existingControllerRef := metav1.GetControllerOfNoCopy(existing)
	if !equality.Semantic.DeepEqual(existingControllerRef, requiredControllerRef) {
		// This is not the place to handle adoption.
		return nil, false, fmt.Errorf(
			"service %q isn't controlled by us: expected controller %#v but is owned by %#v",
			objectReference(requiredCopy),
			requiredControllerRef,
			existingControllerRef,
		)
	}

	// If they are the same do nothing.
	if existing.Annotations[naming.ManagedHash] == requiredCopy.Annotations[naming.ManagedHash] {
		return existing, false, nil
	}

	// Preserve allocated fields.
	requiredCopy.Spec.ClusterIP = existing.Spec.ClusterIP
	requiredCopy.Spec.ClusterIPs = existing.Spec.ClusterIPs

	requiredCopy.ResourceVersion = existing.ResourceVersion
	actual, err := client.Services(requiredCopy.Namespace).Update(ctx, requiredCopy, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		klog.V(2).InfoS("Hit update conflict, will retry.", "Service", klog.KObj(requiredCopy))
	} else {
		reportUpdateEvent(recorder, requiredCopy, err)
	}
	return actual, true, err
}
