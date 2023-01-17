package controllerhelpers

import (
	"context"

	"github.com/scylladb/scylla-operator/pkg/controllertools"
	"github.com/scylladb/scylla-operator/pkg/kubeinterfaces"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
)

type ControlleeManagerGetObjectsInterface[CT, T kubeinterfaces.ObjectInterface] interface {
	GetControllerUncached(ctx context.Context, name string, opts metav1.GetOptions) (CT, error)
	ListObjects(selector labels.Selector) ([]T, error)
	PatchObject(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (T, error)
}

type ControlleeManagerGetObjectsFuncs[CT, T kubeinterfaces.ObjectInterface] struct {
	GetControllerUncachedFunc func(ctx context.Context, name string, opts metav1.GetOptions) (CT, error)
	ListObjectsFunc           func(selector labels.Selector) ([]T, error)
	PatchObjectFunc           func(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (T, error)
}

func (f ControlleeManagerGetObjectsFuncs[CT, T]) GetControllerUncached(ctx context.Context, name string, opts metav1.GetOptions) (CT, error) {
	return f.GetControllerUncachedFunc(ctx, name, opts)
}

func (f ControlleeManagerGetObjectsFuncs[CT, T]) ListObjects(selector labels.Selector) ([]T, error) {
	return f.ListObjectsFunc(selector)
}

func (f ControlleeManagerGetObjectsFuncs[CT, T]) PatchObject(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (T, error) {
	return f.PatchObjectFunc(ctx, name, pt, data, opts, subresources...)
}

var _ ControlleeManagerGetObjectsInterface[kubeinterfaces.ObjectInterface, kubeinterfaces.ObjectInterface] = ControlleeManagerGetObjectsFuncs[kubeinterfaces.ObjectInterface, kubeinterfaces.ObjectInterface]{}

func GetObjectsWithFilter[CT, T kubeinterfaces.ObjectInterface](
	ctx context.Context,
	controller metav1.Object,
	controllerGVK schema.GroupVersionKind,
	selector labels.Selector,
	filterFunc func(T) bool,
	control ControlleeManagerGetObjectsInterface[CT, T],
) (map[string]T, error) {
	// List all objects to find even those that no longer match our selector.
	// They will be orphaned in ClaimObjects().
	allObjects, err := control.ListObjects(labels.Everything())
	if err != nil {
		return nil, err
	}

	var objects []T
	for i := range allObjects {
		if filterFunc(allObjects[i]) {
			objects = append(objects, allObjects[i])
		}
	}

	return controllertools.NewControllerRefManager[T](
		ctx,
		controller,
		controllerGVK,
		selector,
		controllertools.ControllerRefManagerControlFuncsConverter[CT, T]{
			GetControllerUncachedFunc: func(ctx context.Context, name string, opts metav1.GetOptions) (CT, error) {
				return control.GetControllerUncached(ctx, name, opts)
			},
			PatchObjectFunc: func(ctx context.Context, name string, pt types.PatchType, data []byte, opts metav1.PatchOptions, subresources ...string) (T, error) {
				return control.PatchObject(ctx, name, pt, data, opts, subresources...)
			},
		}.Convert(),
	).ClaimObjects(objects)
}

func GetObjects[CT, T kubeinterfaces.ObjectInterface](
	ctx context.Context,
	controller metav1.Object,
	controllerGVK schema.GroupVersionKind,
	selector labels.Selector,
	control ControlleeManagerGetObjectsInterface[CT, T],
) (map[string]T, error) {
	return GetObjectsWithFilter(
		ctx,
		controller,
		controllerGVK,
		selector,
		func(T) bool {
			return true
		},
		control,
	)
}
