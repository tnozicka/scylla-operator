package resourceapply

import (
	"context"
	"fmt"

	"github.com/scylladb/scylla-operator/pkg/naming"
	appsv1 "k8s.io/api/apps/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	appv1client "k8s.io/client-go/kubernetes/typed/apps/v1"
	appv1listers "k8s.io/client-go/listers/apps/v1"
	"k8s.io/client-go/tools/record"
	"k8s.io/klog/v2"
)

func ApplyStatefulSet(
	ctx context.Context,
	client appv1client.StatefulSetsGetter,
	lister appv1listers.StatefulSetLister,
	recorder record.EventRecorder,
	required *appsv1.StatefulSet,
) (*appsv1.StatefulSet, bool, error) {
	requiredControllerRef := metav1.GetControllerOfNoCopy(required)
	if requiredControllerRef == nil {
		return nil, false, fmt.Errorf("StatefulSet %q is missing controllerRef", objectReference(required))
	}

	requiredCopy := required.DeepCopy()
	err := SetHashAnnotation(requiredCopy)
	if err != nil {
		return nil, false, err
	}

	existing, err := lister.StatefulSets(requiredCopy.Namespace).Get(requiredCopy.Name)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, false, err
		}

		actual, err := client.StatefulSets(requiredCopy.Namespace).Create(ctx, requiredCopy, metav1.CreateOptions{})
		if apierrors.IsAlreadyExists(err) {
			klog.V(2).InfoS("Already exists (stale cache)", "StatefulSet", klog.KObj(requiredCopy))
		} else {
			ReportCreateEvent(recorder, requiredCopy, err)
		}
		return actual, true, err
	}

	existingControllerRef := metav1.GetControllerOfNoCopy(existing)
	if !equality.Semantic.DeepEqual(existingControllerRef, requiredControllerRef) {
		// This is not the place to handle adoption.
		return nil, false, fmt.Errorf(
			"statefulset %q isn't controlled by us: expected controller %#v but is owned by %#v",
			objectReference(requiredCopy),
			requiredControllerRef,
			existingControllerRef,
		)
	}

	// If they are the same do nothing.
	if existing.Annotations[naming.ManagedHash] == requiredCopy.Annotations[naming.ManagedHash] {
		return existing, false, nil
	}

	requiredCopy.ResourceVersion = existing.ResourceVersion
	actual, err := client.StatefulSets(requiredCopy.Namespace).Update(ctx, requiredCopy, metav1.UpdateOptions{})
	if apierrors.IsConflict(err) {
		klog.V(2).InfoS("Hit update conflict, will retry.", "StatefulSet", klog.KObj(requiredCopy))
	} else {
		ReportUpdateEvent(recorder, requiredCopy, err)
	}
	return actual, true, err
}
