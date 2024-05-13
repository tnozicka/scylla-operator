// Copyright (C) 2021 ScyllaDB

package framework

import (
	"context"
	"fmt"
	"time"

	g "github.com/onsi/ginkgo/v2"
	o "github.com/onsi/gomega"
	scyllav1 "github.com/scylladb/scylla-operator/pkg/api/scylla/v1"
	scyllaclientset "github.com/scylladb/scylla-operator/pkg/client/scylla/clientset/versioned"
	scyllafixture "github.com/scylladb/scylla-operator/test/e2e/fixture/scylla"
	"k8s.io/apiserver/pkg/storage/names"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	restclient "k8s.io/client-go/rest"
)

const (
	ServiceAccountName                   = "e2e-user"
	ServiceAccountTokenSecretName        = "e2e-user-token"
	serviceAccountWaitTimeout            = 1 * time.Minute
	serviceAccountTokenSecretWaitTimeout = 1 * time.Minute
)

type Framework struct {
	name string

	clusters []Cluster
}

func NewFramework(name string) *Framework {
	frameworkName := names.SimpleNameGenerator.GenerateName(fmt.Sprintf("%s-", name))

	clusters := make([]Cluster, 0, len(TestContext.RestConfigs))
	for i, restConfig := range TestContext.RestConfigs {
		clusterName := fmt.Sprintf("%s-%d", frameworkName, i)
		c := NewCluster(clusterName, frameworkName, restConfig)
		clusters = append(clusters, c)
	}

	f := &Framework{
		name:     frameworkName,
		clusters: clusters,
	}

	g.BeforeEach(f.SetUp)
	g.AfterEach(f.DumpArtifactsAndDestroy)

	return f
}

func (f *Framework) DefaultCluster() Cluster {
	return f.Cluster(0)
}

func (f *Framework) Cluster(idx int) Cluster {
	return f.clusters[idx]
}

func (f *Framework) Clusters() []Cluster {
	return f.clusters
}

var _ Cluster = &Framework{}

func (f *Framework) Namespace() string {
	return f.DefaultCluster().Namespace()
}

func (f *Framework) Username() string {
	return f.DefaultCluster().Username()
}

func (f *Framework) GetIngressAddress(hostname string) string {
	if TestContext.IngressController == nil || len(TestContext.IngressController.Address) == 0 {
		return hostname
	}

	return TestContext.IngressController.Address
}

func (f *Framework) FieldManager() string {
	return f.DefaultCluster().FieldManager()
}

func (f *Framework) ClientConfig() *restclient.Config {
	return f.DefaultCluster().ClientConfig()
}

func (f *Framework) AdminClientConfig() *restclient.Config {
	return f.DefaultCluster().AdminClientConfig()
}

func (f *Framework) DiscoveryClient() *discovery.DiscoveryClient {
	return f.DefaultCluster().DiscoveryClient()
}

func (f *Framework) DynamicClient() dynamic.Interface {
	return f.DefaultCluster().DynamicClient()
}

func (f *Framework) DynamicAdminClient() dynamic.Interface {
	return f.DefaultCluster().DynamicAdminClient()
}

func (f *Framework) KubeClient() *kubernetes.Clientset {
	return f.DefaultCluster().KubeClient()
}

func (f *Framework) KubeAdminClient() *kubernetes.Clientset {
	return f.DefaultCluster().KubeAdminClient()
}

func (f *Framework) ScyllaClient() *scyllaclientset.Clientset {
	return f.DefaultCluster().ScyllaClient()
}

func (f *Framework) ScyllaAdminClient() *scyllaclientset.Clientset {
	return f.DefaultCluster().ScyllaAdminClient()
}

func (f *Framework) CommonLabels() map[string]string {
	return f.DefaultCluster().CommonLabels()
}

func (f *Framework) GetDefaultScyllaCluster() *scyllav1.ScyllaCluster {
	renderArgs := map[string]any{
		"nodeServiceType":             TestContext.ScyllaClusterOptions.ExposeOptions.NodeServiceType,
		"nodesBroadcastAddressType":   TestContext.ScyllaClusterOptions.ExposeOptions.NodesBroadcastAddressType,
		"clientsBroadcastAddressType": TestContext.ScyllaClusterOptions.ExposeOptions.ClientsBroadcastAddressType,
	}

	sc, _, err := scyllafixture.ScyllaClusterTemplate.RenderObject(renderArgs)
	o.Expect(err).NotTo(o.HaveOccurred())

	return sc
}

func (f *Framework) SetUp(ctx context.Context) {
	f.DefaultCluster().SetUp(ctx)
}

func (f *Framework) DumpArtifactsAndDestroy(ctx context.Context) {
	f.DefaultCluster().DumpArtifactsAndDestroy(ctx)
}
