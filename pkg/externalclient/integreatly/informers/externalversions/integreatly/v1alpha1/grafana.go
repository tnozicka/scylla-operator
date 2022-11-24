// Code generated by informer-gen. DO NOT EDIT.

package v1alpha1

import (
	"context"
	time "time"

	integreatlyv1alpha1 "github.com/scylladb/scylla-operator/pkg/externalapi/integreatly/v1alpha1"
	versioned "github.com/scylladb/scylla-operator/pkg/externalclient/integreatly/clientset/versioned"
	internalinterfaces "github.com/scylladb/scylla-operator/pkg/externalclient/integreatly/informers/externalversions/internalinterfaces"
	v1alpha1 "github.com/scylladb/scylla-operator/pkg/externalclient/integreatly/listers/integreatly/v1alpha1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	runtime "k8s.io/apimachinery/pkg/runtime"
	watch "k8s.io/apimachinery/pkg/watch"
	cache "k8s.io/client-go/tools/cache"
)

// GrafanaInformer provides access to a shared informer and lister for
// Grafanas.
type GrafanaInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.GrafanaLister
}

type grafanaInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewGrafanaInformer constructs a new informer for Grafana type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewGrafanaInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredGrafanaInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredGrafanaInformer constructs a new informer for Grafana type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredGrafanaInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IntegreatlyV1alpha1().Grafanas(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IntegreatlyV1alpha1().Grafanas(namespace).Watch(context.TODO(), options)
			},
		},
		&integreatlyv1alpha1.Grafana{},
		resyncPeriod,
		indexers,
	)
}

func (f *grafanaInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredGrafanaInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *grafanaInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&integreatlyv1alpha1.Grafana{}, f.defaultInformer)
}

func (f *grafanaInformer) Lister() v1alpha1.GrafanaLister {
	return v1alpha1.NewGrafanaLister(f.Informer().GetIndexer())
}
