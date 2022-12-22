package kubeinterfaces

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
)

type ObjectInterface interface {
	runtime.Object
	metav1.Object
}

type GetterLister[T any] interface {
	Get(namespace, name string) (T, error)
	List(namespace string, selector labels.Selector) ([]T, error)
}

type GlobalGetList[T any] struct {
	GetFunc  func(name string) (T, error)
	ListFunc func(selector labels.Selector) ([]T, error)
}

func (gl GlobalGetList[T]) Get(namespace, name string) (T, error) {
	if len(namespace) != 0 {
		panic("trying to get non-namespaced object with a namespace")
	}

	return gl.GetFunc(name)
}

func (gl GlobalGetList[T]) List(namespace string, selector labels.Selector) ([]T, error) {
	if len(namespace) != 0 {
		panic("trying to get non-namespaced object with a namespace")
	}

	return gl.ListFunc(selector)
}

type NamespacedGetList[T any] struct {
	GetFunc  func(namespace, name string) (T, error)
	ListFunc func(namespace string, selector labels.Selector) ([]T, error)
}

func (gl NamespacedGetList[T]) Get(namespace, name string) (T, error) {
	return gl.GetFunc(namespace, name)
}

func (gl NamespacedGetList[T]) List(namespace string, selector labels.Selector) ([]T, error) {
	return gl.ListFunc(namespace, selector)
}
