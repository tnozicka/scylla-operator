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

// GrafanaDataSourceInformer provides access to a shared informer and lister for
// GrafanaDataSources.
type GrafanaDataSourceInformer interface {
	Informer() cache.SharedIndexInformer
	Lister() v1alpha1.GrafanaDataSourceLister
}

type grafanaDataSourceInformer struct {
	factory          internalinterfaces.SharedInformerFactory
	tweakListOptions internalinterfaces.TweakListOptionsFunc
	namespace        string
}

// NewGrafanaDataSourceInformer constructs a new informer for GrafanaDataSource type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewGrafanaDataSourceInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers) cache.SharedIndexInformer {
	return NewFilteredGrafanaDataSourceInformer(client, namespace, resyncPeriod, indexers, nil)
}

// NewFilteredGrafanaDataSourceInformer constructs a new informer for GrafanaDataSource type.
// Always prefer using an informer factory to get a shared informer instead of getting an independent
// one. This reduces memory footprint and number of connections to the server.
func NewFilteredGrafanaDataSourceInformer(client versioned.Interface, namespace string, resyncPeriod time.Duration, indexers cache.Indexers, tweakListOptions internalinterfaces.TweakListOptionsFunc) cache.SharedIndexInformer {
	return cache.NewSharedIndexInformer(
		&cache.ListWatch{
			ListFunc: func(options v1.ListOptions) (runtime.Object, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IntegreatlyV1alpha1().GrafanaDataSources(namespace).List(context.TODO(), options)
			},
			WatchFunc: func(options v1.ListOptions) (watch.Interface, error) {
				if tweakListOptions != nil {
					tweakListOptions(&options)
				}
				return client.IntegreatlyV1alpha1().GrafanaDataSources(namespace).Watch(context.TODO(), options)
			},
		},
		&integreatlyv1alpha1.GrafanaDataSource{},
		resyncPeriod,
		indexers,
	)
}

func (f *grafanaDataSourceInformer) defaultInformer(client versioned.Interface, resyncPeriod time.Duration) cache.SharedIndexInformer {
	return NewFilteredGrafanaDataSourceInformer(client, f.namespace, resyncPeriod, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc}, f.tweakListOptions)
}

func (f *grafanaDataSourceInformer) Informer() cache.SharedIndexInformer {
	return f.factory.InformerFor(&integreatlyv1alpha1.GrafanaDataSource{}, f.defaultInformer)
}

func (f *grafanaDataSourceInformer) Lister() v1alpha1.GrafanaDataSourceLister {
	return v1alpha1.NewGrafanaDataSourceLister(f.Informer().GetIndexer())
}
