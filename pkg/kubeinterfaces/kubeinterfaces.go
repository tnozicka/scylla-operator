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
	List(selector labels.Selector) (ret []T, err error)
}

type GetList[T any] struct {
	GetFunc  func(namespace, name string) (T, error)
	ListFunc func(selector labels.Selector) (ret []T, err error)
}

func (gl GetList[T]) Get(namespace, name string) (T, error) {
	return gl.GetFunc(namespace, name)
}

func (gl GetList[T]) List(selector labels.Selector) (ret []T, err error) {
	return gl.ListFunc(selector)
}
